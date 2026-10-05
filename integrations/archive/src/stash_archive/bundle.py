"""Portable SQLite snapshots, original blobs and explicitly declared components.

This layer preserves and verifies components. A caller coordinating live writers
must additionally establish their common receipt/media boundary before publishing
a successful backup. Merely supplying several database paths is not that proof.
"""

from contextlib import closing
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import re
import shutil
import sqlite3
import tempfile
import time
import uuid
from urllib.parse import urlsplit

from .storage import (CHUNK_SIZE, HEX, MAX_MANIFEST, RESERVE_BYTES, InvalidArchive,
                      json_bytes, load_manifest, open_regular, publish_bytes,
                      regular, require_space, store_file, sync_directory,
                      check_descriptor, decode_json, write_artifact)

FORMAT = "org.notsafeforgit.stash.native-archive"
VERSION = 1
NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z")
MD5 = re.compile(r"[0-9a-f]{32}\Z")
ROLES = {"config", "import_rules", "file_journal", "producer_outbox",
         "download_archive", "media_manifest", "worker_profile", "operating_state"}
SQLITE_ROLES = {"library", "producer_outbox", "download_archive"}


def connect_readonly(path):
    regular(path)
    connection = sqlite3.connect(Path(path).absolute().as_uri() + "?mode=ro", uri=True)
    connection.execute("PRAGMA query_only=ON")
    return connection


def database_metadata(connection, role):
    if connection.execute("PRAGMA integrity_check").fetchone() != ("ok",):
        raise InvalidArchive("SQLite integrity check failed")
    if connection.execute("PRAGMA foreign_key_check").fetchone() is not None:
        raise InvalidArchive("SQLite foreign key check failed")
    metadata = {"application_id": connection.execute("PRAGMA application_id").fetchone()[0],
                "user_version": connection.execute("PRAGMA user_version").fetchone()[0]}
    if role == "library":
        rows = connection.execute("SELECT version,dirty FROM schema_migrations LIMIT 2").fetchall()
        if len(rows) != 1 or type(rows[0][0]) is not int or rows[0][0] < 1000000 or rows[0][1] != 0:
            raise InvalidArchive("A clean native migration ledger is required")
        lineage = connection.execute("SELECT lineage FROM native_schema WHERE singleton=1").fetchall()
        if lineage != [(FORMAT,)]:
            raise InvalidArchive("Foreign database lineage")
        metadata.update(lineage=FORMAT, schema=rows[0][0])
    if role == "producer_outbox":
        if metadata["application_id"] != 0x5354494F or metadata["user_version"] < 1:
            raise InvalidArchive("Foreign producer outbox")
        binding = connection.execute("SELECT endpoint,producer FROM binding WHERE id=1").fetchall()
        if len(binding) != 1:
            raise InvalidArchive("Producer outbox binding is missing")
        try:
            endpoint = urlsplit(binding[0][0])
            if (endpoint.scheme not in ("http", "https") or not endpoint.hostname
                    or endpoint.username or endpoint.password or endpoint.query or endpoint.fragment
                    or str(uuid.UUID(binding[0][1])) != binding[0][1]):
                raise ValueError()
        except (ValueError, TypeError, AttributeError) as error:
            raise InvalidArchive("Invalid producer origin or UUID") from error
        metadata.update(endpoint=binding[0][0], producer_uuid=binding[0][1])
    return metadata


def snapshot_database(source, target, role, reserve):
    info = regular(source)
    # The destination and compressed objects may coexist while packing.
    wal = Path(str(source) + "-wal")
    estimated = info.st_size + (regular(wal).st_size if wal.exists() else 0)
    require_space(Path(target).parent, estimated * 2, reserve)
    deadline = time.monotonic() + 3600
    def progress(status, remaining, total):
        if time.monotonic() > deadline:
            raise InvalidArchive("SQLite snapshot deadline exceeded")
        require_space(Path(target).parent, 0, reserve)
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.close(fd)
    with closing(connect_readonly(source)) as incoming, closing(sqlite3.connect(target)) as output:
        incoming.backup(output, pages=256, progress=progress, sleep=0.05)
        metadata = database_metadata(output, role)
    with open_regular(target) as output:
        os.fsync(output.fileno())
    return metadata


def _blob_rows(connection):
    # Do not reconstruct or normalize retained documents/payloads. Every SQLite
    # page survives byte-for-byte; this query only resolves external artwork.
    for checksum, data in connection.execute("SELECT checksum,blob FROM blobs ORDER BY checksum"):
        if not isinstance(checksum, str) or not MD5.fullmatch(checksum):
            raise InvalidArchive("Invalid artwork checksum in the native database")
        if data is not None and hashlib.md5(data, usedforsecurity=False).hexdigest() != checksum:
            raise InvalidArchive("Embedded artwork does not match its checksum")
        if data is None:
            yield checksum


def export_archive(database, destination, *, blob_paths=(), components=(), reserve=RESERVE_BYTES, progress=None):
    destination = Path(destination)
    require_space(destination.parent, 0, reserve)
    destination.mkdir(mode=0o700)
    try:
        (destination / "objects").mkdir(mode=0o700)
        names = set()
        inventory_digest = hashlib.sha256()
        inventory_bytes, count, total = 0, 0, 0
        inventory_path = destination / "artifacts.jsonl"
        inventory_fd = os.open(inventory_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        def add(path, role, name, sqlite_metadata=None):
            nonlocal inventory_bytes, count, total
            if not NAME.fullmatch(name) or (role, name) in names:
                raise InvalidArchive("Invalid or duplicate component name")
            names.add((role, name))
            entry = store_file(destination, path, reserve=reserve)
            entry.update(role=role, name=name)
            if sqlite_metadata is not None:
                entry["sqlite"] = sqlite_metadata
            body = json_bytes(entry)
            if len(body) > MAX_MANIFEST:
                raise InvalidArchive("Artifact descriptor exceeds the supported size")
            inventory.write(body)
            inventory_digest.update(body)
            inventory_bytes += len(body)
            count += 1
            total += entry["size"]
            if progress is not None and (count == 1 or count % 1000 == 0):
                progress({"stage": "export", "artifacts": count, "bytes": total})
            return entry
        with os.fdopen(inventory_fd, "wb") as inventory, tempfile.TemporaryDirectory(prefix=".snapshot-", dir=destination) as temp:
            native = Path(temp) / "library.sqlite"
            metadata = snapshot_database(database, native, "library", reserve)
            add(native, "library", "library", metadata)
            with closing(connect_readonly(native)) as connection:
                for checksum in _blob_rows(connection):
                    found = None
                    for root in blob_paths:
                        candidate = Path(root) / checksum[:2] / checksum[2:4] / checksum
                        if candidate.exists() or candidate.is_symlink():
                            found = candidate
                            break
                    if found is None:
                        raise InvalidArchive(f"Required original artwork is unavailable: {checksum}")
                    entry = add(found, "blob", checksum)
                    # Check the actual bytes packed, not a second live-file read.
                    digest = hashlib.md5(usedforsecurity=False)
                    from .storage import read_chunk
                    for chunk in entry["chunks"]:
                        digest.update(read_chunk(destination, chunk))
                    if digest.hexdigest() != checksum:
                        raise InvalidArchive("Original artwork checksum mismatch")
            for index, component in enumerate(components):
                if not isinstance(component, dict) or set(component) != {"role", "name", "path"}:
                    raise InvalidArchive("Components require exactly role, name and path")
                role, name = component["role"], component["name"]
                if not isinstance(role, str) or role not in ROLES or not isinstance(name, str):
                    raise InvalidArchive("Unsupported component role or name")
                path, meta = component["path"], None
                if role in SQLITE_ROLES:
                    path = Path(temp) / f"component-{index}.sqlite"
                    meta = snapshot_database(component["path"], path, role, reserve)
                add(path, role, name, meta)
            inventory.flush()
            os.fsync(inventory.fileno())
        manifest = {"format": FORMAT, "version": VERSION, "uuid": str(uuid.uuid4()),
                    "created_at": datetime.now(timezone.utc).isoformat(),
                    "coverage": "declared-components", "chunk_size": CHUNK_SIZE,
                    "inventory": {"sha256": inventory_digest.hexdigest(), "size": inventory_bytes,
                                  "count": count, "total_bytes": total}}
        body = json_bytes(manifest)
        if len(body) > MAX_MANIFEST:
            raise InvalidArchive("Archive manifest exceeds the supported size")
        validate_manifest(manifest)
        publish_bytes(destination / "manifest.json", body)
        sync_directory(destination.parent)
        return manifest
    except BaseException:
        # Only a directory created by this call is removed. Existing outputs are
        # refused above, and interruptions cannot publish a success manifest.
        shutil.rmtree(destination)
        raise


def validate_manifest(manifest):
    if not isinstance(manifest, dict) or set(manifest) != {
            "format", "version", "uuid", "created_at", "coverage", "chunk_size", "inventory"}:
        raise InvalidArchive("Invalid archive manifest fields")
    if (manifest["format"] != FORMAT or type(manifest["version"]) is not int
            or manifest["version"] != VERSION or type(manifest["chunk_size"]) is not int or manifest["chunk_size"] != CHUNK_SIZE
            or manifest["coverage"] != "declared-components"):
        raise InvalidArchive("Unsupported archive format, version or coverage")
    try:
        if str(uuid.UUID(manifest["uuid"])) != manifest["uuid"]:
            raise ValueError()
        if datetime.fromisoformat(manifest["created_at"]).tzinfo is None:
            raise ValueError()
    except (ValueError, TypeError, AttributeError) as error:
        raise InvalidArchive("Invalid archive identity or timestamp") from error
    inventory = manifest["inventory"]
    if not isinstance(inventory, dict) or set(inventory) != {"sha256", "size", "count", "total_bytes"}:
        raise InvalidArchive("Invalid inventory descriptor")
    if not isinstance(inventory["sha256"], str) or not HEX.fullmatch(inventory["sha256"]):
        raise InvalidArchive("Invalid inventory digest")
    if any(type(inventory[k]) is not int or inventory[k] < 1 for k in ("size", "count", "total_bytes")):
        raise InvalidArchive("Invalid inventory dimensions")
    return manifest


def iter_artifacts(source, manifest):
    validate_manifest(manifest)
    digest, size, total, count = hashlib.sha256(), 0, 0, 0
    names, libraries = set(), 0
    with open_regular(Path(source) / "artifacts.jsonl") as incoming:
        while body := incoming.readline(MAX_MANIFEST + 1):
            if len(body) > MAX_MANIFEST or not body.endswith(b"\n"):
                raise InvalidArchive("Invalid or oversized inventory record")
            size += len(body)
            if size > manifest["inventory"]["size"]:
                raise InvalidArchive("Inventory exceeds its declared size")
            digest.update(body)
            entry = decode_json(body)
            if not isinstance(entry, dict) or not {"role", "name", "sha256", "size", "chunks"} <= entry.keys() or entry.keys() - {"role", "name", "sha256", "size", "chunks", "sqlite"}:
                raise InvalidArchive("Invalid artifact fields")
            role, name = entry["role"], entry["name"]
            if not isinstance(role, str) or role not in ROLES | {"library", "blob"} or not isinstance(name, str) or not NAME.fullmatch(name):
                raise InvalidArchive("Invalid artifact role or name")
            if (role, name) in names:
                raise InvalidArchive("Duplicate artifact")
            names.add((role, name))
            if role == "library":
                libraries += 1
                if name != "library":
                    raise InvalidArchive("Invalid library component name")
            if role == "blob" and not MD5.fullmatch(name):
                raise InvalidArchive("Invalid original artwork name")
            if (role in SQLITE_ROLES) != ("sqlite" in entry):
                raise InvalidArchive("SQLite component metadata is required only for database components")
            if type(entry["size"]) is not int or entry["size"] < 0 or not isinstance(entry["sha256"], str) or not HEX.fullmatch(entry["sha256"]) or not isinstance(entry["chunks"], list):
                raise InvalidArchive("Invalid artifact dimensions or digest")
            for chunk in entry["chunks"]:
                check_descriptor(chunk)
            if sum(chunk["size"] for chunk in entry["chunks"]) != entry["size"]:
                raise InvalidArchive("Artifact size does not match its chunks")
            count += 1
            total += entry["size"]
            yield entry
    if libraries != 1:
        raise InvalidArchive("Exactly one native library is required")
    if {"sha256": digest.hexdigest(), "size": size, "count": count, "total_bytes": total} != manifest["inventory"]:
        raise InvalidArchive("Inventory does not match its manifest")


def _target(destination, entry):
    if entry["role"] == "library":
        return destination / "library.sqlite"
    if entry["role"] == "blob":
        name = entry["name"]
        return destination / "blobs" / name[:2] / name[2:4] / name
    return destination / "components" / entry["role"] / entry["name"]


def import_archive(source, destination, *, reserve=RESERVE_BYTES, progress=None):
    """Restore into a new directory. Never start a server or resume workers."""
    manifest = validate_manifest(load_manifest(source))
    destination = Path(destination)
    # Authenticate and validate the complete inventory before trusting its space
    # estimate or creating any destination. The second pass streams the data.
    for _ in iter_artifacts(source, manifest):
        pass
    require_space(destination.parent, manifest["inventory"]["total_bytes"], reserve)
    destination.mkdir(mode=0o700)
    try:
        blobs = set()
        for index, entry in enumerate(iter_artifacts(source, manifest), 1):
            if entry["role"] == "blob":
                blobs.add(entry["name"])
            path = _target(destination, entry)
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "wb") as output:
                write_artifact(source, entry, output,
                               check_space=lambda size: require_space(destination, size, reserve))
                output.flush()
                os.fsync(output.fileno())
            sync_directory(path.parent)
            if entry["role"] in SQLITE_ROLES:
                with closing(connect_readonly(path)) as connection:
                    if database_metadata(connection, entry["role"]) != entry["sqlite"]:
                        raise InvalidArchive("Restored database identity differs from the manifest")
            if progress is not None and (index == 1 or index % 1000 == 0):
                progress({"stage": "import", "artifacts": index,
                          "total": manifest["inventory"]["count"]})
        with closing(connect_readonly(destination / "library.sqlite")) as connection:
            required = set(_blob_rows(connection))
        if required != blobs:
            raise InvalidArchive("Original artwork inventory does not match the library snapshot")
        for name in blobs:
            path = destination / "blobs" / name[:2] / name[2:4] / name
            with open_regular(path) as incoming:
                if hashlib.file_digest(incoming, lambda: hashlib.md5(usedforsecurity=False)).hexdigest() != name:
                    raise InvalidArchive("Restored original artwork checksum mismatch")
        # This receipt is last: a partial directory cannot claim a verified
        # restore, including after a process interruption or disk write failure.
        receipt = {"format": FORMAT + ".restore", "version": VERSION,
                   "archive_uuid": manifest["uuid"],
                   "manifest_sha256": hashlib.sha256(json_bytes(manifest)).hexdigest()}
        # Sync every newly created ancestor as well as each leaf directory;
        # syncing only the leaf does not persist its entry in a new parent.
        for directory, _, _ in os.walk(destination, topdown=False):
            sync_directory(directory)
        publish_bytes(destination / "restore.json", json_bytes(receipt))
        sync_directory(destination.parent)
        return manifest
    except BaseException:
        shutil.rmtree(destination)
        raise


def verify_archive(source, *, temp_parent=None, reserve=RESERVE_BYTES):
    """Exercise the complete empty-install restore, then discard only that copy."""
    with tempfile.TemporaryDirectory(prefix="stash-archive-verify-", dir=temp_parent) as temp:
        os.chmod(temp, 0o700)
        return import_archive(source, Path(temp) / "restored", reserve=reserve)


def summary(source):
    manifest = validate_manifest(load_manifest(source))
    objects = {c["sha256"]: c["encoded_size"] for e in iter_artifacts(source, manifest) for c in e["chunks"]}
    return {"uuid": manifest["uuid"], "created_at": manifest["created_at"],
            "coverage": manifest["coverage"], "artifacts": manifest["inventory"]["count"],
            "uncompressed_bytes": manifest["inventory"]["total_bytes"],
            "unique_objects": len(objects), "compressed_bytes": sum(objects.values()),
            "contents_verified": False}
