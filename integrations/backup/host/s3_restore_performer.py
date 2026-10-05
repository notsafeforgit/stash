#!/usr/bin/env python3
"""Plan or reconstruct base-plus-delta backups.

Default invocation only reads metadata in the Standard bucket and prints a plan.
Glacier thaw requests require --request-thaw. --objects-dir is entirely offline
and requires an explicit local manifest. Payload downloads require --download.
"""
from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import subprocess
import tarfile
import tempfile
from native_store import selection_digest, validate_reference
import media_objects

STANDARD_REMOTE_PATH = "s3-standard:metadata-backup-andrew/"
REMOTE_PATH = "glacier:video-backup-andrew/"


def relative_path(value):
    if not isinstance(value, str) or not value or value.startswith("/"):
        raise ValueError(f"Expected a relative path: {value!r}")
    if any(part in ("", ".", "..") for part in value.split("/")) or "\0" in value:
        raise ValueError(f"Unsafe relative path: {value!r}")
    return PurePosixPath(value)


def beneath(root, relative):
    path = root.joinpath(*relative_path(relative).parts)
    if not path.resolve().is_relative_to(root.resolve()):
        raise ValueError(f"Path escapes its restore directory: {relative!r}")
    if path.is_symlink():
        raise ValueError(f"Refusing symlink destination: {path}")
    return path


def video_path(manifest, video):
    return video["path"] if manifest["version"] == 4 else video["key"]


def validate_manifest(manifest):
    if (not isinstance(manifest, dict) or manifest.get("format") != "s3-log-backup"
            or type(manifest.get("version")) is not int or manifest["version"] not in (2, 3, 4)):
        raise ValueError("Unsupported restore manifest format")
    if not isinstance(manifest.get("units"), list) or not isinstance(manifest.get("videos"), list):
        raise ValueError("Manifest must contain units and videos lists")
    seen_units, seen_videos, seen_archives = set(), set(), set()
    directories = {}
    for unit in manifest["units"]:
        if (not isinstance(unit, dict)
                or not {'rel_dir', 'kind', 'unit_id', 'base_key', 'deltas'} <= unit.keys()):
            raise ValueError("Invalid archive unit")
        rel = str(relative_path(unit["rel_dir"]))
        kind = unit["kind"]
        if not isinstance(kind, str):
            raise ValueError("Invalid archive kind")
        prefix = {"recursive": "R:", "rootfiles": "F:"}.get(kind)
        if prefix is None or unit["unit_id"] != prefix + rel or unit["unit_id"] in seen_units:
            raise ValueError("Invalid or duplicate archive unit")
        seen_units.add(unit["unit_id"])
        if rel in directories:
            raise ValueError("Multiple archive units target the same directory")
        directories[rel] = kind
        if not isinstance(unit["deltas"], list):
            raise ValueError("Archive deltas must be an ordered list")
        for key in [unit["base_key"], *unit["deltas"]]:
            relative_path(key)
            if not key.startswith("tarballs/") or not key.endswith(".tar") or key in seen_archives:
                raise ValueError(f"Invalid or duplicate archive key: {key!r}")
            seen_archives.add(key)
    for rel in directories:
        for parent in PurePosixPath(rel).parents:
            if directories.get(str(parent)) == "recursive":
                raise ValueError("Recursive archive units overlap")
    for video in manifest["videos"]:
        if not isinstance(video, dict) or "key" not in video:
            raise ValueError("Invalid video entry")
        key = video["key"]
        relative_path(key)
        path = video.get("path") if manifest["version"] == 4 else key
        relative_path(path)
        if path in seen_videos or key in seen_archives:
            raise ValueError(f"Duplicate restore path or conflicting key: {path!r}")
        if video.get("size") is not None and (type(video["size"]) is not int or video["size"] < 0):
            raise ValueError(f"Invalid video size: {key!r}")
        seen_videos.add(path)
    for path in seen_videos:
        if any(str(parent) in seen_videos for parent in PurePosixPath(path).parents):
            raise ValueError("Video restore paths overlap")
    for directory in directories:
        if any(str(part) in seen_videos for part in (PurePosixPath(directory), *PurePosixPath(directory).parents)):
            raise ValueError("Video restore path overlaps an archive directory")
    if manifest["version"] == 4:
        media_objects.validate_inventory(manifest)
        if manifest.get("scope") not in ("native", "media"):
            raise ValueError("Immutable manifests require an explicit native or media scope")
    if manifest["version"] == 3 or (manifest["version"] == 4 and manifest["scope"] == "native"):
        reference = validate_reference(manifest.get("native_archive"))
        if selection_digest(manifest) != reference["selection_sha256"]:
            raise ValueError("Media selection differs from its native archive publication")
    elif "native_archive" in manifest:
        raise ValueError("Media-only manifests cannot claim a native archive binding")
    return manifest


def load_legacy_manifest(manifest_path, ledger_db, footprints):
    """Adapt the currently deployed text allowlist and backed-up SQLite ledger.

    Only keys in the allowlist may be restored. Failure to account for any listed
    archive is fatal; silently dropping an unknown delta would corrupt a restore.
    """
    keys = set(Path(manifest_path).read_text(encoding="utf-8").splitlines()) - {""}
    if any(key.startswith(media_objects.PREFIX) for key in keys):
        raise ValueError("Content-addressed media requires its JSON manifest with restore paths")
    for key in keys:
        relative_path(key)
    tar_keys = {key for key in keys if key.startswith("tarballs/")}
    fp_units = set()
    for line in Path(footprints).read_text(encoding="utf-8").splitlines():
        if line:
            row = json.loads(line)
            fp_units.add(row.get("unit") or "R:" + row["rel_dir"])
    conn = sqlite3.connect(Path(ledger_db).resolve().as_uri() + "?mode=ro", uri=True)
    try:
        states = dict(conn.execute("SELECT unit_id, base_key FROM unit_state"))
        deltas = {}
        for uid, key in conn.execute("SELECT unit_id,key FROM delta_keys ORDER BY created_epoch,rowid"):
            if key in tar_keys:
                deltas.setdefault(uid, []).append(key)
    finally:
        conn.close()
    units, accounted = [], set()
    for uid in sorted(fp_units | set(states) | set(deltas)):
        if not uid.startswith(("R:", "F:")):
            raise ValueError(f"Unknown legacy unit ID: {uid}")
        rel = uid[2:]
        relative_path(rel)
        safe = rel.replace("/", "_").replace("\\", "_").replace(",", "_")
        rootfiles = uid.startswith("F:")
        stable = f"tarballs/{safe}{'.__root__' if rootfiles else ''}.tar"
        base = states.get(uid)
        if base not in tar_keys:
            base = stable if stable in tar_keys else None
        if not base and not rootfiles:
            candidates = []
            for key in tar_keys:
                prefix = f"tarballs/{safe}_"
                stamp = key[len(prefix):-4] if key.startswith(prefix) and key.endswith(".tar") else ""
                if len(stamp) == 8 and stamp.isdigit():
                    candidates.append(key)
            if len(candidates) == 1:
                base = candidates[0]
        chain = deltas.get(uid, [])
        if not base:
            if chain:
                raise ValueError(f"No allowlisted baseline for delta chain {uid}")
            continue
        units.append({"unit_id": uid, "rel_dir": rel, "kind": "rootfiles" if rootfiles else "recursive", "base_key": base, "deltas": chain})
        accounted.update([base, *chain])
    if accounted != tar_keys:
        raise ValueError(f"Legacy ledger cannot resolve {len(tar_keys - accounted)} allowlisted archives")
    return validate_manifest({
        "format": "s3-log-backup", "version": 2, "run_id": "legacy",
        "units": units, "videos": [{"key": key, "size": None} for key in sorted(keys - tar_keys)],
    })


def load_manifest(path, ledger_db=None, footprints=None):
    path = Path(path)
    if path.suffix.lower() == ".json":
        return validate_manifest(json.loads(path.read_text(encoding="utf-8")))
    if not ledger_db or not footprints:
        raise ValueError("A legacy text manifest requires --ledger-db and --footprints")
    return load_legacy_manifest(path, ledger_db, footprints)


def fetch_manifest(directory):
    """Fetch only Standard-storage metadata; never contact the archive bucket."""
    path = directory / "current_manifest.json"
    result = subprocess.run(["rclone", "copyto", STANDARD_REMOTE_PATH + path.name, str(path)], capture_output=True, text=True)
    if result.returncode == 0:
        return load_manifest(path)
    if result.returncode not in (3, 4):
        raise RuntimeError(f"Cannot read the Standard-storage manifest: {result.stderr.strip()}")
    # Before the first run of the updated backup, adapt the existing published
    # metadata. Authentication/network errors must not masquerade as absence.
    local_paths = []
    for key in ("current_manifest.txt", "ledgers/latest/tar_delta_state.sqlite3", "ledgers/latest/tarball_footprints.jsonl"):
        local = directory / Path(key).name
        subprocess.run(["rclone", "copyto", STANDARD_REMOTE_PATH + key, str(local)], check=True)
        local_paths.append(local)
    return load_legacy_manifest(*local_paths)


def select_plan(manifest, search_term, include_videos=False):
    validate_manifest(manifest)
    term = search_term.casefold()
    units = [unit for unit in manifest["units"] if term in unit["rel_dir"].casefold()]
    videos = [video for video in manifest["videos"]
              if include_videos and term in video_path(manifest, video).casefold()]
    if not units and not videos:
        raise ValueError(f"No backup entries match {search_term!r}")
    # A subset restores media only. It cannot claim the whole-library native
    # binding after removing files from the original selection.
    selected = {"format": "s3-log-backup", "version": 2,
                "run_id": manifest.get("run_id"), "units": units, "videos": videos}
    if manifest["version"] == 4:
        selected.update(version=4, scope="media", media_store=manifest["media_store"],
                        objects={key: manifest["objects"][key] for key in required_keys(selected)})
    return validate_manifest(selected)


def required_keys(plan):
    return list(dict.fromkeys([key for unit in plan["units"] for key in [unit["base_key"], *unit["deltas"]]]
                              + [v["key"] for v in plan["videos"]]))


def show_plan(plan):
    keys = required_keys(plan)
    print(f"Plan: {len(plan['units'])} metadata units, {len(plan['videos'])} videos, {len(keys)} objects.")
    for unit in plan["units"]:
        print(f"  {unit['rel_dir']} ({unit['kind']}): base + {len(unit['deltas'])} deltas")
    for video in plan["videos"]:
        print(f"  video: {video_path(plan, video)}")
    return keys


def apply_archive(path, unit, destination, is_delta, previous_epoch=None):
    root = beneath(destination, unit["rel_dir"])
    root.mkdir(parents=True, exist_ok=True)
    with tarfile.open(path, "r:") as archive:
        members = archive.getmembers()
        names = [member.name for member in members]
        if len(names) != len(set(names)):
            raise ValueError(f"Duplicate members in {path}")
        epoch = previous_epoch
        if is_delta:
            if "__delta_meta__.json" not in names:
                raise ValueError(f"Delta has no metadata: {path}")
            stream = archive.extractfile("__delta_meta__.json")
            if stream is None:
                raise ValueError("Delta metadata is not a regular file")
            with stream:
                meta = json.load(stream)
            for field in ("unit_id", "rel_dir", "kind", "base_key"):
                if meta.get(field) != unit[field]:
                    raise ValueError(f"Delta {path} has mismatched {field}")
            epoch = meta["created_epoch"]
            if not isinstance(epoch, int) or (previous_epoch is not None and epoch < previous_epoch):
                raise ValueError("Delta chain is not in chronological order")
            if "__tombstones__.txt" in names:
                stream = archive.extractfile("__tombstones__.txt")
                if stream is None:
                    raise ValueError("Tombstones are not a regular file")
                with stream:
                    tombstones = stream.read().decode("utf-8").splitlines()
                for relative in tombstones:
                    if not relative:
                        continue
                    target = beneath(root, relative)
                    if target.is_dir():
                        raise ValueError(f"A file tombstone targets a directory: {relative!r}")
                    target.unlink(missing_ok=True)
        elif "__delta_meta__.json" in names:
            raise ValueError("A delta archive was supplied as a baseline")
        for member in members:
            if member.name in ("__delta_meta__.json", "__tombstones__.txt"):
                continue
            name = member.name.removesuffix("/") if member.isdir() else member.name
            relative_path(name)
            if unit["kind"] == "rootfiles" and "/" in name:
                raise ValueError("A rootfiles unit contains a nested file")
            if member.isdir():
                beneath(root, name).mkdir(parents=True, exist_ok=True)
                continue
            # Tar may encode deduplicated source images as hardlinks. Reading
            # their bytes through tarfile is safe; no filesystem links are made.
            if not (member.isfile() or member.islnk()):
                raise ValueError(f"Unsupported tar entry: {member.name!r}")
            if member.islnk():
                relative_path(member.linkname)
            compressed_nfo = name.lower().endswith(".nfo.gz")
            target = beneath(root, name[:-3] if compressed_nfo else name)
            target.parent.mkdir(parents=True, exist_ok=True)
            stream = archive.extractfile(member)
            if stream is None:
                raise ValueError(f"Cannot read archive member: {member.name!r}")
            with stream, open(target, "wb") as output:
                if compressed_nfo:
                    with gzip.GzipFile(fileobj=stream) as uncompressed:
                        shutil.copyfileobj(uncompressed, output)
                else:
                    shutil.copyfileobj(stream, output)
            os.utime(target, (member.mtime, member.mtime))
    return epoch


def restore_local(plan, objects_dir, destination):
    """Reconstruct a selected snapshot solely from already-local objects."""
    validate_manifest(plan)
    objects_dir, destination = Path(objects_dir), Path(destination)
    if destination.is_symlink() or (destination.exists() and (not destination.is_dir() or any(destination.iterdir()))):
        raise ValueError("Restore destination must be a new or empty directory")
    paths = {key: beneath(objects_dir, key) for key in required_keys(plan)}
    signatures = {}
    for key, path in paths.items():
        if not path.is_file():
            raise FileNotFoundError(f"Required backup object is missing: {key}")
        if plan["version"] == 4:
            signatures[key] = media_objects.signature(path.lstat())
            media_objects.verify_local(plan["objects"][key], path)
            if signatures[key] != media_objects.signature(path.lstat()):
                raise ValueError("Media object changed during restore verification")
    for video in plan["videos"]:
        if video.get("size") is not None and paths[video["key"]].stat().st_size != video["size"]:
            raise ValueError(f"Video size does not match manifest: {video['key']}")

    def checked(key):
        path = paths[key]
        if key in signatures and signatures[key] != media_objects.signature(path.lstat()):
            raise ValueError("Media object changed after restore verification")
        return path

    destination.mkdir(parents=True, exist_ok=True)
    for unit in plan["units"]:
        apply_archive(checked(unit["base_key"]), unit, destination, False)
        epoch = None
        for key in unit["deltas"]:
            epoch = apply_archive(checked(key), unit, destination, True, epoch)
    for video in plan["videos"]:
        target = beneath(destination, video_path(plan, video))
        if target.exists():
            raise ValueError("Video restore path collides with an archive member")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(checked(video["key"]), target)
    for key in signatures:
        checked(key)
    return destination


def verify_tree(actual, expected):
    def hashes(root):
        result = {}
        for path in Path(root).rglob("*"):
            if path.is_symlink():
                raise ValueError(f"Unexpected symlink in comparison tree: {path}")
            if path.is_file():
                with path.open("rb") as stream:
                    result[path.relative_to(root).as_posix()] = hashlib.file_digest(stream, "sha256").hexdigest()
        return result
    actual_files, expected_files = hashes(actual), hashes(expected)
    if actual_files != expected_files:
        missing = set(expected_files) - set(actual_files)
        extra = set(actual_files) - set(expected_files)
        changed = {key for key in actual_files.keys() & expected_files.keys() if actual_files[key] != expected_files[key]}
        raise ValueError(f"Restore verification failed: missing={len(missing)} extra={len(extra)} changed={len(changed)}")
    return len(actual_files)


def media_client():
    import boto3
    from botocore.config import Config
    return boto3.client("s3", config=Config(connect_timeout=10, read_timeout=60, retries={"max_attempts": 3}))


def request_thaw(plan, directory, lifetime):
    validate_manifest(plan)
    if type(lifetime) is not int or lifetime < 1:
        raise ValueError("Thaw lifetime must be positive")
    keys = required_keys(plan)
    if plan["version"] == 4:
        client = media_client()
        for key in keys:
            media_objects.request_restore(client, plan["media_store"], key, plan["objects"][key], lifetime)
        print(f"Requested Bulk thaw for {len(keys)} objects. No payloads were downloaded.")
        return
    if not keys or any("\n" in key or "\r" in key for key in keys):
        raise ValueError("Cannot safely represent this selection as a thaw file list")
    listing = directory / "thaw-objects.txt"
    listing.write_text("".join(key + "\n" for key in keys), encoding="utf-8")
    result = subprocess.run([
        "rclone", "backend", "restore", REMOTE_PATH, "--files-from-raw", str(listing),
        "-o", "priority=Bulk", "-o", f"lifetime={lifetime}",
    ], check=True, capture_output=True, text=True)
    statuses = json.loads(result.stdout)
    if not isinstance(statuses, list) or len(statuses) != len(keys) or any(row.get("Status") != "OK" for row in statuses):
        raise RuntimeError(f"Not all requested thaws succeeded: {result.stdout}")
    print(f"Requested Bulk thaw for {len(keys)} objects. No payloads were downloaded.")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("name", help="Substring of a backed-up directory; default action prints a metadata restore plan.")
    parser.add_argument("--manifest", type=Path, help="Local JSON manifest, or legacy text manifest.")
    parser.add_argument("--ledger-db", type=Path, help="Required with a legacy text manifest.")
    parser.add_argument("--footprints", type=Path, help="Required with a legacy text manifest.")
    parser.add_argument("--include-videos", action="store_true")
    parser.add_argument("--destination", type=Path)
    parser.add_argument("--save-plan", type=Path)
    parser.add_argument("--lifetime", type=int, default=7)
    action = parser.add_mutually_exclusive_group()
    action.add_argument("--objects-dir", type=Path, help="Reconstruct from local object files without network access.")
    action.add_argument("--request-thaw", action="store_true", help="Explicitly request potentially billable Glacier Bulk retrievals.")
    action.add_argument("--download", action="store_true", help="Download already-thawed objects and reconstruct them.")
    action.add_argument("--check-status", action="store_true", help="Read object restore status; never requests thawing.")
    args = parser.parse_args(argv)
    if args.objects_dir and not args.manifest:
        parser.error("--objects-dir requires a local --manifest")
    if (args.objects_dir or args.download) and not args.destination:
        parser.error("Reconstruction requires --destination pointing to a new or empty directory")
    if args.destination and (args.destination.is_symlink() or (args.destination.exists() and (not args.destination.is_dir() or any(args.destination.iterdir())))):
        parser.error("--destination must be a new or empty directory")
    with tempfile.TemporaryDirectory(prefix="s3-restore-") as temp:
        directory = Path(temp)
        manifest = load_manifest(args.manifest, args.ledger_db, args.footprints) if args.manifest else fetch_manifest(directory)
        plan = select_plan(manifest, args.name, args.include_videos)
        keys = show_plan(plan)
        if args.save_plan:
            args.save_plan.write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")
        if args.request_thaw:
            request_thaw(plan, directory, args.lifetime)
        elif args.check_status:
            if plan["version"] == 4:
                client = media_client()
                for key in keys:
                    print(json.dumps(media_objects.restore_status(client, plan["media_store"], key, plan["objects"][key])))
            else:
                # restore-status ignores rclone filters: scope each call to one key.
                for key in keys:
                    subprocess.run(["rclone", "backend", "restore-status", REMOTE_PATH + key, "-o", "all"], check=True)
        elif args.download:
            objects = directory / "objects"
            client = media_client() if plan["version"] == 4 else None
            for key in keys:
                target = beneath(objects, key)
                target.parent.mkdir(parents=True, exist_ok=True)
                if plan["version"] == 4:
                    media_objects.download(client, plan["media_store"], key, plan["objects"][key], target)
                else:
                    subprocess.run(["rclone", "copyto", REMOTE_PATH + key, str(target)], check=True)
            restore_local(plan, objects, args.destination)
            print(f"Restore complete: {args.destination}")
        elif args.objects_dir:
            restore_local(plan, args.objects_dir, args.destination)
            print(f"Local reconstruction complete: {args.destination}")
        else:
            print("Plan only. No Glacier retrieval or payload download was requested.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
