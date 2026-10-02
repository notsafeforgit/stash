"""Prepare or verify a frozen automation snapshot; never import or activate jobs."""

import argparse
import base64
import hashlib
import math
import os
from pathlib import Path
import shutil
import sqlite3
import sys
import tempfile

from .automation_source import AutomationSource, KEYS, READER_VERSION, TABLES, open_regular
from .catalog_snapshot import ChunkWriter, MAX_CHUNK_BYTES, MAX_CHUNK_ROWS, MAX_MANIFEST_BYTES, sync_directory
from .catalog_source import APPLICATION_ID, SHA256, source_time
from .encoding import InvalidData, decode, digest, encode, identifier


FORMAT = "stash-automation-snapshot-v1"
MAX_RECORDS = 10_000_000
MANIFEST_KEYS = {"format", "reader", "snapshot_uuid", "registry_source_uuid", "source_sha256", "captured_at",
                 "application_id", "user_version", "tables", "records", "chunks", "stage", "schema", "integrity"}


def validate_record(row, tables):
    if not isinstance(row, dict) or set(row) != {"table", "key", "values"} or not isinstance(row["table"], str):
        raise InvalidData("Invalid automation record")
    table, key, values = row["table"], row["key"], row["values"]
    if (table not in tables or not isinstance(key, list) or not isinstance(values, dict)
            or set(values) != set(TABLES[table]) or len(key) != len(KEYS[table])):
        raise InvalidData("Automation record differs from its inventory")
    for index, part in enumerate(key):
        if (type(part) not in (str, int) or type(part) is int and not -(1 << 63) <= part < 1 << 63
                or type(part) is not type(values[KEYS[table][index]]) or part != values[KEYS[table][index]]):
            raise InvalidData("Invalid automation record key")
    encode(key, 32768)
    for value in values.values():
        if isinstance(value, dict):
            if set(value) != {"sqlite_blob_base64"} or not isinstance(value["sqlite_blob_base64"], str):
                raise InvalidData("Invalid SQLite binary value")
            try:
                raw = base64.b64decode(value["sqlite_blob_base64"], validate=True)
            except ValueError:
                raise InvalidData("Invalid SQLite binary encoding") from None
            if base64.b64encode(raw).decode("ascii") != value["sqlite_blob_base64"]:
                raise InvalidData("Noncanonical SQLite binary encoding")
        elif (value is not None and type(value) not in (str, int, float)
              or type(value) is float and not math.isfinite(value)
              or type(value) is int and not -(1 << 63) <= value < 1 << 63):
            raise InvalidData("Invalid SQLite value")
    # SQLite numbers sort before strings; string ordering uses binary Unicode.
    return table, tuple((1 if isinstance(part, str) else 0, part) for part in key)


def record(table, values, tables):
    row = {"table": table, "key": [values[key] for key in KEYS[table]],
           "values": {key: {"sqlite_blob_base64": base64.b64encode(value).decode("ascii")} if isinstance(value, bytes) else value
                      for key, value in values.items()}}
    key = validate_record(row, tables)
    return encode(row, MAX_CHUNK_BYTES - 1) + b"\n", key


def validate_manifest(value):
    if (not isinstance(value, dict) or set(value) != MANIFEST_KEYS or value["format"] != FORMAT
            or value["reader"] != READER_VERSION or value["stage"] != "prepared"
            or type(value["application_id"]) is not int or value["application_id"] != APPLICATION_ID
            or type(value["user_version"]) is not int or value["user_version"] not in (1, 2, 3)
            or type(value["records"]) is not int or not 0 <= value["records"] <= MAX_RECORDS
            or not isinstance(value["tables"], dict) or set(value["tables"]) != set(TABLES)
            or not isinstance(value["chunks"], list) or len(value["chunks"]) > 100_000
            or not isinstance(value["source_sha256"], str) or not SHA256.fullmatch(value["source_sha256"])):
        raise InvalidData("Unsupported automation snapshot manifest")
    identifier(value["snapshot_uuid"])
    identifier(value["registry_source_uuid"])
    source_time(value["captured_at"])
    integrity = value["integrity"]
    if (not isinstance(integrity, dict) or set(integrity) != {"quick_check", "foreign_key_violations"}
            or integrity["quick_check"] != "ok" or type(integrity["foreign_key_violations"]) is not int
            or not 0 <= integrity["foreign_key_violations"] <= value["records"]):
        raise InvalidData("Invalid automation integrity inventory")
    for name, table in value["tables"].items():
        if (not isinstance(table, dict) or set(table) != {"rows", "bytes", "sha256", "columns", "key"}
                or type(table["rows"]) is not int or not 0 <= table["rows"] <= MAX_RECORDS
                or type(table["bytes"]) is not int or not table["rows"] <= table["bytes"] <= table["rows"] * MAX_CHUNK_BYTES
                or not isinstance(table["sha256"], str) or not SHA256.fullmatch(table["sha256"])
                or table["rows"] == 0 and table["sha256"] != digest(b"")
                or table["key"] != KEYS[name] or not isinstance(table["columns"], list)
                or len(table["columns"]) != len(TABLES[name])):
            raise InvalidData("Invalid automation table inventory")
        seen = set()
        for index, column in enumerate(table["columns"]):
            if (not isinstance(column, dict) or set(column) != {"cid", "name", "type", "notnull", "dflt_value", "pk", "hidden"}
                    or any(type(column[key]) is not int for key in ("cid", "notnull", "pk", "hidden"))
                    or column["cid"] != index or column["notnull"] not in (0, 1) or column["hidden"] != 0
                    or not isinstance(column["type"], str) or not isinstance(column["name"], str)
                    or column["name"] not in TABLES[name] or column["name"] in seen
                    or column["dflt_value"] is not None and not isinstance(column["dflt_value"], str)
                    or column["pk"] != (KEYS[name].index(column["name"]) + 1 if column["name"] in KEYS[name] else 0)):
                raise InvalidData("Invalid automation column inventory")
            seen.add(column["name"])
    names, tables = set(), set()
    if not isinstance(value["schema"], list):
        raise InvalidData("Missing automation schema inventory")
    for item in value["schema"]:
        if (not isinstance(item, dict) or set(item) != {"type", "name", "tbl_name", "sql"}
                or any(not isinstance(v, str) for v in item.values()) or item["type"] not in ("table", "index", "trigger")
                or not item["name"] or item["name"] in names or item["tbl_name"] not in TABLES
                or not 0 < len(item["sql"].encode()) <= 1 << 20):
            raise InvalidData("Invalid automation schema object")
        names.add(item["name"])
        if item["type"] == "table":
            if item["name"] != item["tbl_name"]:
                raise InvalidData("Invalid automation table identity")
            tables.add(item["name"])
    if tables != set(TABLES):
        raise InvalidData("Incomplete automation schema inventory")
    for index, chunk in enumerate(value["chunks"]):
        if (not isinstance(chunk, dict) or set(chunk) != {"file", "rows", "bytes", "sha256"}
                or chunk["file"] != f"records-{index:06d}.jsonl" or type(chunk["rows"]) is not int
                or not 1 <= chunk["rows"] <= MAX_CHUNK_ROWS or type(chunk["bytes"]) is not int
                or not chunk["rows"] <= chunk["bytes"] <= MAX_CHUNK_BYTES
                or not isinstance(chunk["sha256"], str) or not SHA256.fullmatch(chunk["sha256"])):
            raise InvalidData("Invalid automation chunk inventory")
    if (sum(t["rows"] for t in value["tables"].values()) != value["records"]
            or sum(c["rows"] for c in value["chunks"]) != value["records"]
            or sum(t["bytes"] for t in value["tables"].values()) != sum(c["bytes"] for c in value["chunks"])):
        raise InvalidData("Automation record totals disagree")


def summary(manifest, manifest_sha256):
    return {"format": FORMAT, "snapshot_uuid": manifest["snapshot_uuid"], "registry_source_uuid": manifest["registry_source_uuid"],
            "source_sha256": manifest["source_sha256"], "manifest_sha256": manifest_sha256, "records": manifest["records"],
            "chunks": len(manifest["chunks"]), "pending_families": sorted(manifest["tables"]), "prepared": True, "imported": False}


def prepare(path, output, snapshot_uuid, registry_source_uuid, captured_at):
    identifier(snapshot_uuid)
    identifier(registry_source_uuid)
    source_time(captured_at)
    destination = Path(output).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Snapshot destination already exists; verify it or choose a new destination")
    parent = destination.parent.resolve(strict=True)
    destination = parent / destination.name
    stage = Path(tempfile.mkdtemp(prefix=".stash-automation-snapshot-", dir=parent))
    writer = ChunkWriter(stage)
    published = False
    try:
        with AutomationSource(path) as source:
            integrity = source.check_integrity()
            inventory, count, last = {}, 0, None
            for table in sorted(source.tables):
                hashed, rows, size = hashlib.sha256(), 0, 0
                for values in source.rows(table):
                    body, key = record(table, values, source.tables)
                    if last is not None and key <= last:
                        raise InvalidData("Automation records are not unique and ordered")
                    last = key
                    count += 1
                    if count > MAX_RECORDS:
                        raise InvalidData("Automation snapshot exceeds its record limit")
                    writer.append(body)
                    hashed.update(body)
                    rows += 1
                    size += len(body)
                inventory[table] = {"rows": rows, "bytes": size, "sha256": hashed.hexdigest(),
                                    "columns": source.tables[table], "key": KEYS[table]}
            writer.finish()
            manifest = {"format": FORMAT, "reader": READER_VERSION, "snapshot_uuid": snapshot_uuid,
                        "registry_source_uuid": registry_source_uuid, "source_sha256": source.source_sha256,
                        "captured_at": captured_at, "application_id": source.application_id, "user_version": source.version,
                        "tables": inventory, "records": count, "chunks": writer.chunks, "stage": "prepared",
                        "schema": source.schema, "integrity": integrity}
            validate_manifest(manifest)
            body = encode(manifest, MAX_MANIFEST_BYTES)
            with (stage / "manifest.json").open("xb") as file:
                os.fchmod(file.fileno(), 0o600)
                file.write(body)
                file.flush()
                os.fsync(file.fileno())
            source.assert_unchanged()
        sync_directory(stage)
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


def verify(path, expected_sha256=None):
    directory = Path(path).resolve(strict=True)
    with open_regular(directory / "manifest.json") as file:
        body = file.read(MAX_MANIFEST_BYTES + 1)
    value = decode(body, MAX_MANIFEST_BYTES)
    actual = digest(body)
    if expected_sha256 is not None and expected_sha256 != actual:
        raise InvalidData("Snapshot manifest digest changed")
    validate_manifest(value)
    totals = {name: {"rows": 0, "bytes": 0, "hashed": hashlib.sha256()} for name in value["tables"]}
    last, count, expected_files = None, 0, {"manifest.json"}
    for chunk in value["chunks"]:
        expected_files.add(chunk["file"])
        hashed, size, rows = hashlib.sha256(), 0, 0
        with open_regular(directory / chunk["file"]) as file:
            for line in iter(lambda: file.readline(MAX_CHUNK_BYTES + 1), b""):
                if not line.endswith(b"\n") or line.count(b"\n") != 1 or len(line) > MAX_CHUNK_BYTES:
                    raise InvalidData("Invalid automation record framing")
                row = decode(line, MAX_CHUNK_BYTES)
                key = validate_record(row, value["tables"])
                if last is not None and key <= last:
                    raise InvalidData("Automation records are not unique and ordered")
                last = key
                hashed.update(line)
                size += len(line)
                rows += 1
                if size > MAX_CHUNK_BYTES or rows > MAX_CHUNK_ROWS:
                    raise InvalidData("Automation chunk exceeds its limit")
                total = totals[row["table"]]
                total["hashed"].update(line)
                total["rows"] += 1
                total["bytes"] += len(line)
        if size != chunk["bytes"] or rows != chunk["rows"] or hashed.hexdigest() != chunk["sha256"]:
            raise InvalidData("Automation chunk checksum/count mismatch")
        count += rows
    if {child.name for child in directory.iterdir()} != expected_files or count != value["records"]:
        raise InvalidData("Automation snapshot has missing or extra files/records")
    for name, total in totals.items():
        if (any(value["tables"][name][key] != total[key] for key in ("rows", "bytes"))
                or value["tables"][name]["sha256"] != total["hashed"].hexdigest()):
            raise InvalidData("Automation table inventory differs from its records")
    return summary(value, actual)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--automation", help="Frozen automation SQLite input")
    source.add_argument("--verify", help="Prepared snapshot directory")
    parser.add_argument("--output", help="New private snapshot directory")
    parser.add_argument("--snapshot", help="Stable UUID for this automation snapshot")
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
            result = prepare(args.automation, args.output, args.snapshot, args.source, args.captured_at)
        print(encode(result).decode())
        return 0
    except InvalidData as error:
        message = str(error)
    except (OSError, sqlite3.Error):
        message = "Automation snapshot input or output is unavailable"
    except (KeyError, TypeError, AttributeError, ValueError):
        message = "Invalid automation snapshot shape"
    print(encode({"error": message, "imported": False}).decode(), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
