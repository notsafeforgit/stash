"""Retain a frozen legacy scan journal through the native application API."""

import argparse
from contextlib import closing
from datetime import datetime
from http.client import HTTPException
import json
from pathlib import Path
import os
import re
import sqlite3
import sys
from urllib.error import HTTPError, URLError
from urllib.request import Request

from .backfill_import import ImportClient, TABLES as BACKFILL_TABLES
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier

TABLES = {
    "scan_jobs": ("id", "run_key", "context", "command_json", "url", "created", "attempts", "retry_after", "last_status"),
    "extractor_jobs": ("id", "url", "context", "pid", "process_start", "last_key", "last_index", "scope"),
    "scan_deferrals": ("context", "url", "reason", "last_status", "deferred_at"),
    "backfill_scan_completion": ("backfill_key", "scan_key", "completed"),
    "collection_backfill_completion": ("platform", "collection_kind", "collection_name", "recorded_at", "result_json"),
    "backfill_policy_migrations": ("name", "applied_at", "details_json"),
    "legacy_handoffs": ("run_key", "unit", "boot_id", "pid", "jobs_json"),
}
KEYS = {
    "scan_jobs": ("id",), "extractor_jobs": ("id",), "scan_deferrals": ("context", "url"),
    "backfill_scan_completion": ("backfill_key", "scan_key"),
    "collection_backfill_completion": ("platform", "collection_kind", "collection_name"),
    "backfill_policy_migrations": ("name",), "legacy_handoffs": ("run_key",),
}


def snapshot(path, captured_at):
    if not isinstance(captured_at, str) or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:[0-9]{2})", captured_at):
        raise InvalidData("A fixed RFC3339 snapshot time is required")
    try:
        datetime.fromisoformat(captured_at)
    except ValueError:
        raise InvalidData("Invalid snapshot time") from None
    tables, external = {}, {}
    with closing(sqlite3.connect(Path(path).absolute().as_uri() + "?mode=ro", uri=True, timeout=5)) as db:
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA query_only=ON")
        db.execute("PRAGMA trusted_schema=OFF")
        db.execute("BEGIN")
        total, size = 0, 0
        for entry in db.execute("SELECT name,type FROM sqlite_schema WHERE type IN ('table','view') AND name NOT GLOB 'sqlite_*' ORDER BY name").fetchall():
            table = entry["name"]
            allowed = TABLES.get(table) or BACKFILL_TABLES.get(table)
            if entry["type"] != "table" or allowed is None:
                raise InvalidData("Unknown table or view in legacy scan journal; review the original snapshot")
            columns = {row["name"] for row in db.execute("PRAGMA table_info(" + table + ")")}
            required = set(allowed[:5] if table == "extractor_jobs" else allowed)
            if not required <= columns <= set(allowed):
                raise InvalidData("Unsupported legacy journal columns")
            if table in BACKFILL_TABLES:
                external[table] = db.execute("SELECT count(*) FROM " + table).fetchone()[0]
                continue
            records, seen = [], set()
            for row in db.execute("SELECT * FROM " + table + " ORDER BY " + ",".join(KEYS[table])):
                record = dict(row)
                keys = tuple(record[key] for key in KEYS[table])
                if any(not isinstance(key, str) or not key for key in keys) or keys in seen:
                    raise InvalidData("Invalid or duplicate legacy journal key")
                seen.add(keys)
                encoded = encode(record, 1 << 20)
                total += 1
                size += len(encoded)
                if total > 10000 or size > 8 << 20:
                    raise InvalidData("Legacy journal exceeds the atomic snapshot limit")
                records.append(record)
            tables[table] = records
    if not tables:
        raise InvalidData("Journal has no recognized scan-history tables")
    document = {"captured_at": captured_at, "tables": tables, "external_tables": external}
    encode(document, 8 << 20)
    return document


class ScanJournalClient(ImportClient):
    def submit(self, value):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        body = encode(value, (8 << 20) + 1024)
        request = Request(self.endpoint + "/api/v3/archive/scan-journals/import", data=body, method="POST",
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_scan_journal_response")
                result = decode(response.read(16385), 16384)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable("scan_journal_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_scan_journal_response") from None
        expected = receipt_fields(value)
        if not isinstance(result, dict) or any(result.get(key) != item for key, item in expected.items()):
            raise Unavailable("invalid_scan_journal_response")
        return result


def receipt_fields(value):
    document = value["document"]
    return {**{key: value[key] for key in ("uuid", "source_uuid", "root_uuid")},
            "input_sha256": digest(encode(document, 8 << 20)), "captured_at": document["captured_at"],
            "record_count": sum(map(len, document["tables"].values())),
            "inventory": {"retained_tables": {key: len(rows) for key, rows in document["tables"].items()},
                          "external_tables": document["external_tables"]}}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--journal", required=True, help="Frozen journal database; opened read-only")
    parser.add_argument("--root", required=True)
    parser.add_argument("--source", required=True, help="Stable original database UUID from the migration manifest")
    parser.add_argument("--snapshot", required=True, help="UUID for this frozen snapshot, unchanged across retries")
    parser.add_argument("--captured-at", required=True, help="Fixed RFC3339 snapshot time from the migration manifest")
    parser.add_argument("--endpoint")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--expected-sha256", help="Reviewed prepared input digest, required with --apply")
    parser.add_argument("--apply", action="store_true", help="Retain the complete snapshot in Stash; does not activate jobs")
    args = parser.parse_args(argv)
    try:
        if args.apply and not all((args.endpoint, args.expected_sha256)):
            raise InvalidData("Apply requires an explicit native endpoint and reviewed snapshot digest")
        value = {"uuid": identifier(args.snapshot), "source_uuid": identifier(args.source), "root_uuid": identifier(args.root),
                 "document": snapshot(args.journal, args.captured_at)}
        result = receipt_fields(value)
        if args.expected_sha256 is not None and args.expected_sha256 != result["input_sha256"]:
            raise InvalidData("Journal differs from the reviewed snapshot digest")
        if args.apply:
            result = ScanJournalClient(args.endpoint, args.api_key_env).submit(value)
        print(json.dumps({**result, "state": "retained" if args.apply else "prepared", "jobs_activated": 0}, sort_keys=True))
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (OSError, sqlite3.Error):
        message = "Legacy journal snapshot is unavailable"
    print(json.dumps({"error": message, "acknowledged": False, "resume": "repeat_same_frozen_snapshot"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
