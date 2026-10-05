"""Retain native artwork inodes while the server checkpoint guard is held.

Only the native atomic artwork writer and journaled cleanup are supported.
External tools must not mutate retained artwork in place. The host owns this
private cache and its lifecycle; Stash neither chooses paths nor creates links.
"""

import base64
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import re
import shutil
import stat
import time

from .filesystem_boundary import canonical_uuid, timestamp
from .storage import (HEX, InvalidArchive, decode_json, json_bytes, open_regular,
                      publish_bytes, require_space, sync_directory)

FORMAT = "org.notsafeforgit.stash.artwork-pins"
MD5 = re.compile(r"[0-9a-f]{32}\Z")
PREFIX = re.compile(r"[0-9a-f]{2}\Z")
MAX_RECORD = 48 << 10


def directory(path, private=False):
    info = Path(path).lstat()
    if not stat.S_ISDIR(info.st_mode) or (private and info.st_mode & 0o077):
        raise InvalidArchive("Artwork pin directories must be real, private directories")
    return info.st_dev, info.st_ino


def identity(info):
    return {"device": info.st_dev, "inode": info.st_ino,
            "size": info.st_size, "mtime_ns": info.st_mtime_ns}


def pin_entries(source):
    """Open prefix directories without following links; ignore non-blob debris."""
    root = os.open(source, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for prefix in sorted(os.listdir(root)):
            if not PREFIX.fullmatch(prefix):
                continue
            first = os.open(prefix, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root)
            try:
                for suffix in sorted(os.listdir(first)):
                    if not PREFIX.fullmatch(suffix):
                        continue
                    second = os.open(suffix, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=first)
                    try:
                        for checksum in sorted(os.listdir(second)):
                            if not MD5.fullmatch(checksum):
                                continue
                            if checksum[:4] != prefix + suffix:
                                raise InvalidArchive("Artwork file has the wrong checksum directory")
                            yield second, checksum
                    finally:
                        os.close(second)
            finally:
                os.close(first)
    finally:
        os.close(root)


class ArtworkPins:
    def __init__(self, cache, sources, *, reserve=50 << 30):
        self.cache = Path(cache).resolve(strict=True)
        self.cache_identity = directory(self.cache, private=True)
        self.sources = [Path(p).absolute() for p in sources]
        if not self.sources or len(self.sources) > 64 or len(set(self.sources)) != len(self.sources):
            raise InvalidArchive("Artwork pins require one to 64 distinct source roots")
        self.identities = [directory(p) if p.exists() else None for p in self.sources]
        if any(info is not None and info[0] != self.cache_identity[0] for info in self.identities):
            raise InvalidArchive("The artwork pin cache must share the source filesystem")
        self.reserve = reserve

    def verify_cache(self):
        if directory(self.cache, private=True) != self.cache_identity:
            raise InvalidArchive("Artwork pin cache was replaced")

    def path(self, checkpoint, token):
        self.verify_cache()
        if not canonical_uuid(checkpoint) or not canonical_uuid(token):
            raise InvalidArchive("Invalid artwork checkpoint identity")
        return self.cache / (checkpoint + "-" + token)

    def capture(self, ready):
        """Call only from the authenticated boundary callback, before its ACK."""
        target = self.path(ready["uuid"], ready["token"])
        request_hash = ready["request_sha256"]
        if not isinstance(request_hash, str) or not HEX.fullmatch(request_hash):
            raise InvalidArchive("Invalid artwork checkpoint request digest")
        remaining = (timestamp(ready["expires_at"]) - datetime.now(timezone.utc)).total_seconds()
        deadline = time.monotonic() + remaining

        def check():
            if time.monotonic() >= deadline:
                raise InvalidArchive("Artwork pin capture exceeded the checkpoint deadline")
            self.verify_cache()
            require_space(self.cache, 0, self.reserve)

        check()
        # Each one-use server challenge gets fresh output. Never replace or
        # recapture an existing directory after an interrupted invocation.
        target.mkdir(mode=0o700)
        try:
            roots = []
            for index, (source, expected) in enumerate(zip(self.sources, self.identities)):
                if directory(source) != expected:
                    raise InvalidArchive("Artwork source root changed")
                tree = target / str(index)
                tree.mkdir(mode=0o700)
                count, total, digest = 0, 0, hashlib.sha256()
                with open(tree / "inventory.jsonl", "xb") as inventory:
                    os.chmod(inventory.name, 0o600)
                    # Flat pins avoid duplicating tens of thousands of hash
                    # directories. Portable restore still uses Stash's layout.
                    for source_fd, checksum in pin_entries(source):
                        check()
                        before = os.stat(checksum, dir_fd=source_fd, follow_symlinks=False)
                        if not stat.S_ISREG(before.st_mode):
                            raise InvalidArchive("Artwork pin source must be a regular file")
                        os.link(checksum, tree / checksum, src_dir_fd=source_fd, follow_symlinks=False)
                        linked = (tree / checksum).lstat()
                        current = os.stat(checksum, dir_fd=source_fd, follow_symlinks=False)
                        if identity(before) != identity(linked) or identity(before) != identity(current):
                            raise InvalidArchive("Artwork changed while retaining its inode")
                        with open_regular(tree / checksum) as retained:
                            if identity(os.fstat(retained.fileno())) != identity(linked):
                                raise InvalidArchive("Artwork pin changed before flushing")
                            os.fsync(retained.fileno())
                        body = json_bytes({"checksum": checksum, **identity(linked)})
                        inventory.write(body)
                        digest.update(body)
                        count += 1
                        total += linked.st_size
                    inventory.flush()
                    os.fsync(inventory.fileno())
                sync_directory(tree)
                roots.append({"source_path": base64.b64encode(os.fsencode(source)).decode("ascii"),
                              "source_device": expected[0], "source_inode": expected[1],
                              "count": count, "bytes": total, "inventory_sha256": digest.hexdigest()})
                if directory(source) != expected:
                    raise InvalidArchive("Artwork source root changed during capture")
            record = {"format": FORMAT, "version": 1, "uuid": ready["uuid"], "token": ready["token"],
                      "request_sha256": request_hash, "roots": roots}
            body = json_bytes(record)
            if len(body) > MAX_RECORD:
                raise InvalidArchive("Artwork pin record exceeds its size limit")
            check()
            publish_bytes(target / "manifest.json", body)
            sync_directory(self.cache)
            check()
            return record
        except BaseException:
            shutil.rmtree(target)
            sync_directory(self.cache)
            raise

    def open_bound(self, boundary, *, _released=False):
        """Use only the original pin set named by a sealed server receipt."""
        record = boundary.get("details", {}).get("artwork") if isinstance(boundary, dict) else None
        if (not isinstance(record, dict) or record.get("format") != FORMAT
                or type(record.get("version")) is not int or record["version"] != 1
                or any(record.get(k) != boundary.get(k) for k in ("uuid", "token", "request_sha256"))):
            raise InvalidArchive("Artwork pins do not match the server checkpoint")
        path = self.path(record["uuid"], record["token"])
        directory(path, private=True)
        if ((path / "release.json").exists() or (path / "release.json").is_symlink()) and not _released:
            raise InvalidArchive("Artwork pins have already been released")
        with open_regular(path / "manifest.json") as incoming:
            body = incoming.read(MAX_RECORD + 1)
        if len(body) > MAX_RECORD or body != json_bytes(record):
            raise InvalidArchive("Artwork pin manifest differs from the server receipt")
        if (set(record) != {"format", "version", "uuid", "token", "request_sha256", "roots"}
                or not isinstance(record["roots"], list) or len(record["roots"]) != len(self.sources)):
            raise InvalidArchive("Artwork pin source configuration changed")
        for source, original in zip(self.sources, record["roots"]):
            if (not isinstance(original, dict)
                    or set(original) != {"source_path", "source_device", "source_inode", "count", "bytes", "inventory_sha256"}
                    or original["source_path"] != base64.b64encode(os.fsencode(source)).decode("ascii")
                    or any(type(original[k]) is not int or original[k] < 0 for k in ("source_device", "source_inode", "count", "bytes"))
                    or not isinstance(original["inventory_sha256"], str) or not HEX.fullmatch(original["inventory_sha256"])):
                raise InvalidArchive("Artwork pin source configuration changed")
        return PinnedArtwork(path, record)


class PinnedArtwork:
    def __init__(self, path, record):
        self.path, self.record = path, record

    def entries(self, index, *, missing_ok=False):
        root = self.record["roots"][index]
        tree = self.path / str(index)
        directory(tree, private=True)
        count, total, previous, digest = 0, 0, "", hashlib.sha256()
        with open_regular(tree / "inventory.jsonl") as inventory:
            while body := inventory.readline(1025):
                if len(body) > 1024 or not body.endswith(b"\n"):
                    raise InvalidArchive("Invalid artwork pin inventory row")
                entry = decode_json(body)
                if (not isinstance(entry, dict) or set(entry) != {"checksum", "device", "inode", "size", "mtime_ns"}
                        or not isinstance(entry["checksum"], str) or not MD5.fullmatch(entry["checksum"])
                        or entry["checksum"] <= previous
                        or any(type(entry[k]) is not int for k in ("device", "inode", "size", "mtime_ns"))
                        or min(entry["device"], entry["inode"], entry["size"]) < 0):
                    raise InvalidArchive("Invalid artwork pin inventory row")
                previous = entry["checksum"]
                digest.update(body)
                count += 1
                total += entry["size"]
                path = tree / previous
                try:
                    info = path.lstat()
                except FileNotFoundError:
                    if missing_ok:
                        continue
                    raise
                if not stat.S_ISREG(info.st_mode) or identity(info) != {k: entry[k] for k in identity(info)}:
                    raise InvalidArchive("Retained artwork inode was replaced or modified")
                yield path
        if (count, total, digest.hexdigest()) != (root["count"], root["bytes"], root["inventory_sha256"]):
            raise InvalidArchive("Artwork pin inventory differs from the sealed record")

    def verify(self, *, missing_ok=False):
        for index in range(len(self.record["roots"])):
            for _ in self.entries(index, missing_ok=missing_ok):
                pass
        return self

    def resolve(self, checksum):
        if not MD5.fullmatch(checksum):
            raise InvalidArchive("Invalid artwork checksum")
        for index in range(len(self.record["roots"])):
            path = self.path / str(index) / checksum
            if path.exists() or path.is_symlink():
                return path
        raise InvalidArchive("Original artwork is absent from the retained checkpoint")


def release_published_artwork(source, client, pins):
    """Retire pins only after the caller verifies durable archive publication.

    Derive the pin identity from the verified archived server component and bind
    cleanup to the server's permanent release receipt. No S3 access happens here.
    Keep small receipts; remove inventories only after durable cleanup completion.
    """
    from .checkpoint_release import archived_boundary, release_published_checkpoint
    boundary = archived_boundary(source, client)
    view = pins.open_bound(boundary, _released=True)
    # Server release is retryable and verifies this exact enclosing archive.
    release = release_published_checkpoint(source, client)
    body = json_bytes({"format": FORMAT + "-release", "version": 1,
                       "pins_sha256": hashlib.sha256(json_bytes(view.record)).hexdigest(), "server_release": release})
    intent, complete = view.path / "release.json", view.path / "released.json"

    def matching(path):
        try:
            with open_regular(path) as incoming:
                stored = incoming.read(len(body) + 1)
        except FileNotFoundError:
            return False
        if stored != body:
            raise InvalidArchive("Artwork pin release binding changed")
        return True

    if not matching(complete):
        started = matching(intent)
        # Validate every inventory before any removal, including after a crash.
        view.verify(missing_ok=started)
        if not started:
            publish_bytes(intent, body)
        for index in range(len(view.record["roots"])):
            for path in view.entries(index, missing_ok=True):
                path.unlink()
            sync_directory(view.path / str(index))
        publish_bytes(complete, body)
    # Interrupted cleanup of the now-unneeded inventories is also retryable.
    # Unknown files are never removed, and the identity/receipt files remain.
    for index, root in enumerate(view.record["roots"]):
        inventory = view.path / str(index) / "inventory.jsonl"
        try:
            with open_regular(inventory) as incoming:
                digest = hashlib.file_digest(incoming, "sha256").hexdigest()
        except FileNotFoundError:
            continue
        if digest != root["inventory_sha256"]:
            raise InvalidArchive("Released artwork inventory was replaced")
        inventory.unlink()
        sync_directory(inventory.parent)
    return release
