"""Prepare and verify immutable, bounded catalog snapshots for native import."""

import argparse
import base64
import hashlib
import os
from pathlib import Path
import re
import shutil
import sqlite3
import sys
import tempfile

from .catalog_source import APPLICATION_ID, CatalogSource, KEYS, MAX_ROW_BYTES, READER_VERSION, REFERENCES, REQUIRED, SHA256, TABLES, source_time
from .encoding import InvalidData, decode, digest, encode, identifier


FORMAT = "stash-catalog-snapshot-v1"
MAX_CHUNK_BYTES = 16 << 20
MAX_CHUNK_ROWS = 1000
MAX_MANIFEST_BYTES = 8 << 20


def record(table, row):
    values = {key: {"sqlite_blob_base64": base64.b64encode(value).decode("ascii")} if isinstance(value, bytes) else value
              for key, value in row.items()}
    result = {"table": table, "key": [row[key] for key in KEYS[table]], "values": values}
    return encode(result, MAX_ROW_BYTES - 1) + b"\n"


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


class ChunkWriter:
    def __init__(self, directory):
        self.directory = directory
        self.chunks = []
        self.file = None
        self.rows = self.size = 0

    def append(self, body):
        if self.file is not None and (self.size + len(body) > MAX_CHUNK_BYTES or self.rows >= MAX_CHUNK_ROWS):
            self.finish()
        if self.file is None:
            self.name = f"records-{len(self.chunks):06d}.jsonl"
            self.file = (self.directory / self.name).open("xb")
            os.fchmod(self.file.fileno(), 0o600)
            self.hashed = hashlib.sha256()
        self.file.write(body)
        self.hashed.update(body)
        self.rows += 1
        self.size += len(body)

    def finish(self):
        if self.file is None:
            return
        try:
            self.file.flush()
            os.fsync(self.file.fileno())
        finally:
            self.file.close()
            self.file = None
        self.chunks.append({"file": self.name, "rows": self.rows, "bytes": self.size, "sha256": self.hashed.hexdigest()})
        self.rows = self.size = 0

    def close(self):
        if self.file is not None:
            self.file.close()
            self.file = None


def prepare(catalog, output, snapshot_uuid, registry_source_uuid, captured_at):
    identifier(snapshot_uuid)
    identifier(registry_source_uuid)
    source_time(captured_at)
    destination = Path(output).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Snapshot destination already exists; verify it or choose a new destination")
    parent = destination.parent.resolve(strict=True)
    destination = parent / destination.name
    stage = Path(tempfile.mkdtemp(prefix=".stash-catalog-snapshot-", dir=parent))
    writer = ChunkWriter(stage)
    published = False
    try:
        with CatalogSource(catalog) as source:
            source.check_integrity()
            inventory = {}
            for table in sorted(source.tables):
                hashed, count, size = hashlib.sha256(), 0, 0
                for row in source.rows(table):
                    source.validate_row(table, row)
                    body = record(table, row)
                    writer.append(body)
                    hashed.update(body)
                    count += 1
                    size += len(body)
                inventory[table] = {"rows": count, "bytes": size, "sha256": hashed.hexdigest(),
                                    "columns": source.tables[table], "key": list(KEYS[table])}
            writer.finish()
            captures = source.capture_inventory()
            manifest = {"format": FORMAT, "reader": READER_VERSION, "snapshot_uuid": snapshot_uuid,
                        "registry_source_uuid": registry_source_uuid, "catalog_id": source.info["id"],
                        "captured_at": captured_at, "application_id": source.application_id, "user_version": source.version,
                        "catalog_info": source.info, "schema": source.schema, "tables": inventory,
                        "records": sum(item["rows"] for item in inventory.values()), "chunks": writer.chunks,
                        "captures": captures, "references": source.reference_inventory(), "stage": "prepared"}
            body = encode(manifest, MAX_MANIFEST_BYTES)
            with (stage / "manifest.json").open("xb") as file:
                os.fchmod(file.fileno(), 0o600)
                file.write(body)
                file.flush()
                os.fsync(file.fileno())
        sync_directory(stage)
        # The destination must remain absent. mkdir reserves its name without
        # ever replacing another snapshot, then rename publishes the complete
        # private directory over our own empty reservation on this filesystem.
        destination.mkdir(mode=0o700)
        try:
            os.rename(stage, destination)
        except BaseException:
            destination.rmdir()
            raise
        published = True
        sync_directory(parent)
        return summary(manifest, digest(body))
    finally:
        writer.close()
        if not published:
            shutil.rmtree(stage)


def summary(manifest, manifest_sha256):
    return {"format": FORMAT, "snapshot_uuid": manifest["snapshot_uuid"], "catalog_id": manifest["catalog_id"],
            "manifest_sha256": manifest_sha256, "records": manifest["records"], "chunks": len(manifest["chunks"]),
            "captures": manifest["captures"]["count"], "prepared": True, "imported": False}


def validate_manifest(value):
    if (not isinstance(value, dict) or value.get("format") != FORMAT or value.get("reader") != READER_VERSION
            or value.get("stage") != "prepared" or value.get("application_id") != APPLICATION_ID
            or type(value.get("user_version")) is not int or value["user_version"] not in (1, 2, 3)
            or not isinstance(value.get("tables"), dict) or not REQUIRED <= value["tables"].keys()
            or not isinstance(value.get("chunks"), list) or type(value.get("records")) is not int
            or value["records"] < 0):
        raise InvalidData("Unsupported catalog snapshot manifest")
    identifier(value["snapshot_uuid"])
    identifier(value["registry_source_uuid"])
    source_time(value["captured_at"])
    if (not isinstance(value["catalog_id"], str) or not re.fullmatch(r"c_[0-9a-f]{32}", value["catalog_id"])
            or value["catalog_id"] != value["catalog_info"]["id"] or value["catalog_info"].get("schema_version") != str(value["user_version"])
            or value["catalog_info"].get("path_base") != "media-root-relative"):
        raise InvalidData("Snapshot catalog identity/version differs from its inventory")
    normalized = {"sidecar_documents", "sidecar_sources"}
    names = value["tables"].keys()
    if (bool(names & normalized) and not normalized <= names) or ("sidecars" in names) == (normalized <= names):
        raise InvalidData("Unsupported snapshot sidecar representation")
    for name, table in value["tables"].items():
        if name not in TABLES or not isinstance(table, dict):
            raise InvalidData("Unknown retained snapshot table")
        actual = {column["name"] for column in table["columns"]}
        expected = set(TABLES[name])
        if name in ("observations", "observation_details") and "account_refs_json" in actual:
            expected.add("account_refs_json")
        if value["user_version"] == 3 and name in ("observations", "observation_details") and "account_refs_json" not in actual:
            raise InvalidData("Version 3 snapshot lacks profile reference columns")
        if (actual != expected or len(actual) != len(table["columns"]) or table["key"] != list(KEYS[name])
                or any(type(table.get(key)) is not int or table[key] < 0 for key in ("rows", "bytes"))
                or not isinstance(table.get("sha256"), str) or not SHA256.fullmatch(table["sha256"])):
            raise InvalidData("Unsupported snapshot table inventory")
    refs = value.get("references")
    expected_refs = {table + "." + column: value["tables"][table]["rows"]
                     for table, links in REFERENCES.items() if table in names for column, _, _ in links}
    if not isinstance(refs, dict) or set(refs) != set(expected_refs) or any(type(refs[key]) is not int or not 0 <= refs[key] <= count for key, count in expected_refs.items()):
        raise InvalidData("Invalid snapshot reference inventory")
    for chunk in value["chunks"]:
        if (not isinstance(chunk, dict) or type(chunk.get("rows")) is not int or not 1 <= chunk["rows"] <= MAX_CHUNK_ROWS
                or not isinstance(chunk.get("sha256"), str) or not SHA256.fullmatch(chunk["sha256"])):
            raise InvalidData("Invalid snapshot chunk descriptor")
    captures = value["captures"]
    if (not isinstance(captures, dict) or captures.get("encoding") != "legacy-python-json-v1"
            or any(type(captures.get(key)) is not int or captures[key] < 0 for key in ("count", "flat_observations", "profile_references", "max_payload_bytes"))
            or captures["flat_observations"] > value["tables"]["observations"]["rows"]
            or captures["count"] != value["tables"].get("observation_details", {}).get("rows", 0) + captures["flat_observations"]
            or captures["max_payload_bytes"] > 4 << 20 or not isinstance(captures.get("sha256"), str) or not SHA256.fullmatch(captures["sha256"])):
        raise InvalidData("Invalid snapshot capture reconciliation")


def verify(path, expected_sha256=None):
    directory = Path(path).resolve(strict=True)
    manifest_path = directory / "manifest.json"
    if manifest_path.is_symlink():
        raise InvalidData("Snapshot manifest must be a regular file")
    with manifest_path.open("rb") as file:
        body = file.read(MAX_MANIFEST_BYTES + 1)
    value = decode(body, MAX_MANIFEST_BYTES)
    actual = digest(body)
    if expected_sha256 is not None and expected_sha256 != actual:
        raise InvalidData("Snapshot manifest digest changed")
    validate_manifest(value)
    totals = {name: {"rows": 0, "bytes": 0, "hashed": hashlib.sha256()} for name in value["tables"]}
    last = None
    count = 0
    expected_files = {"manifest.json"}
    for index, chunk in enumerate(value["chunks"]):
        name = f"records-{index:06d}.jsonl"
        if chunk["file"] != name or type(chunk["bytes"]) is not int or not 1 <= chunk["bytes"] <= MAX_CHUNK_BYTES:
            raise InvalidData("Invalid snapshot chunk descriptor")
        expected_files.add(name)
        target = directory / name
        if target.is_symlink() or not target.is_file():
            raise InvalidData("Snapshot chunk must be a regular file")
        hashed, size, rows = hashlib.sha256(), 0, 0
        with target.open("rb") as file:
            for line in iter(lambda: file.readline(MAX_ROW_BYTES + 1), b""):
                if not line.endswith(b"\n") or len(line) > MAX_ROW_BYTES:
                    raise InvalidData("Invalid snapshot record size/framing")
                row = decode(line, MAX_ROW_BYTES)
                table = row["table"]
                if table not in totals or set(row) != {"table", "key", "values"} or not isinstance(row["values"], dict):
                    raise InvalidData("Unknown snapshot record family")
                descriptor = value["tables"][table]
                if (table not in KEYS or descriptor["key"] != list(KEYS[table])
                        or set(row["values"]) != {column["name"] for column in descriptor["columns"]}
                        or row["key"] != [row["values"][key] for key in KEYS[table]]):
                    raise InvalidData("Snapshot record shape/key differs from its inventory")
                if any(type(part) not in (str, int) for part in row["key"]):
                    raise InvalidData("Invalid snapshot record key type")
                for column, item in row["values"].items():
                    if isinstance(item, dict):
                        if (table not in ("sidecars", "sidecar_documents") or column != "raw_content"
                                or set(item) != {"sqlite_blob_base64"} or not isinstance(item["sqlite_blob_base64"], str)):
                            raise InvalidData("Invalid binary snapshot column")
                        try:
                            raw = base64.b64decode(item["sqlite_blob_base64"], validate=True)
                        except ValueError:
                            raise InvalidData("Invalid binary snapshot encoding") from None
                        if digest(raw) != row["values"]["content_sha256"]:
                            raise InvalidData("Snapshot sidecar content checksum mismatch")
                    elif item is not None and type(item) not in (str, int, float):
                        raise InvalidData("Invalid SQLite snapshot value")
                key = (table, tuple(row["key"]))
                if last is not None and key <= last:
                    raise InvalidData("Snapshot records are not unique and ordered")
                last = key
                hashed.update(line); size += len(line); rows += 1
                totals[table]["hashed"].update(line)
                totals[table]["rows"] += 1
                totals[table]["bytes"] += len(line)
                if size > MAX_CHUNK_BYTES or rows > MAX_CHUNK_ROWS:
                    raise InvalidData("Snapshot chunk exceeds its limit")
        if size != chunk["bytes"] or rows != chunk["rows"] or hashed.hexdigest() != chunk["sha256"]:
            raise InvalidData("Snapshot chunk checksum/count mismatch")
        count += rows
    if {child.name for child in directory.iterdir()} != expected_files or count != value["records"]:
        raise InvalidData("Snapshot has missing or extra files/records")
    for table, total in totals.items():
        if any(value["tables"][table][key] != total[key] for key in ("rows", "bytes")) or value["tables"][table]["sha256"] != total["hashed"].hexdigest():
            raise InvalidData("Snapshot table inventory differs from its records")
    return summary(value, actual)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--catalog", help="Frozen catalog SQLite input")
    source.add_argument("--verify", help="Prepared snapshot directory")
    parser.add_argument("--output", help="New private snapshot directory")
    parser.add_argument("--snapshot", help="Stable UUID for this catalog snapshot")
    parser.add_argument("--source", help="Original registry source UUID")
    parser.add_argument("--captured-at", help="Fixed time for this frozen snapshot")
    parser.add_argument("--expected-sha256", help="Expected manifest digest when verifying")
    args = parser.parse_args(argv)
    try:
        if args.verify:
            if any((args.output, args.snapshot, args.source, args.captured_at)):
                raise InvalidData("A prepared snapshot cannot be overridden")
            result = verify(args.verify, args.expected_sha256)
        else:
            if not all((args.output, args.snapshot, args.source, args.captured_at)) or args.expected_sha256:
                raise InvalidData("Preparation requires output, snapshot, source and capture time")
            result = prepare(args.catalog, args.output, args.snapshot, args.source, args.captured_at)
        print(encode(result).decode())
        return 0
    except InvalidData as error:
        message = str(error)
    except (OSError, sqlite3.Error):
        message = "Catalog snapshot input or output is unavailable"
    except (KeyError, TypeError, AttributeError, ValueError):
        message = "Invalid catalog snapshot shape"
    print(encode({"error": message, "imported": False}).decode(), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
