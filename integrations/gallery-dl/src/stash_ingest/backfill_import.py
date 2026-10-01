"""Import permanent account-backfill decisions from a read-only journal snapshot."""

import argparse
from contextlib import closing
from datetime import datetime
from http.client import HTTPException
import json
import os
from pathlib import Path
import re
import sqlite3
import sys
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener
import uuid

from .client import NoRedirect, Unavailable
from .encoding import InvalidData, decode, encode, identifier
from .endpoint import origin
from .launcher_inputs import reddit_name, twitter_target


TABLES = {
    "backfill_completion": ("platform", "account", "component", "completed_at", "result_json"),
    "legacy_backfill_skip": ("platform", "account", "recorded_at", "reason"),
}
REDDIT_COMPONENTS = {"reddit-new", "reddit-top", "reddit-profile-new", "reddit-profile-top-all",
                     "reddit-search-new", "reddit-search-top-all", "reddit-search-top-year"}


def decision_uuid(source, table, record):
    keys = [record["platform"], record["account"]]
    if table == "backfill_completion":
        keys.append(record["component"])
    key = json.dumps(keys, ensure_ascii=False, separators=(",", ":"))
    return str(uuid.uuid5(uuid.UUID(source), table + "/" + key))


def validate_record(table, record):
    if set(record) != set(TABLES[table]) or any(not isinstance(value, str) for value in record.values()):
        raise InvalidData("Unsupported legacy backfill row shape")
    platform, account = record["platform"], record["account"]
    if platform == "reddit":
        if reddit_name(account) != account:
            raise InvalidData("Legacy Reddit account must be a bare name")
    elif platform == "twitter":
        if not re.fullmatch(r"[0-9]{1,30}", account):
            raise InvalidData("Legacy Twitter account must be a numeric ID")
        twitter_target(account, "id")
    else:
        raise InvalidData("Unsupported legacy backfill platform")
    if table == "backfill_completion":
        allowed = {"twitter"} if platform == "twitter" else REDDIT_COMPONENTS
        if record["component"] not in allowed:
            raise InvalidData("Unsupported legacy backfill component")
        result = decode(record["result_json"].encode(), 1 << 20)
        if (not isinstance(result, dict) or result.get("command_failed") is not False
                or type(result.get("exit_code")) is not int or result["exit_code"] != 0
                or (result.get("network_blocked") is not None and result.get("network_blocked") is not False)):
            raise InvalidData("Legacy backfill does not record a successful or accepted completion")
    elif not record["reason"]:
        raise InvalidData("Legacy skip reason is missing")
    timestamp = record["completed_at" if table == "backfill_completion" else "recorded_at"]
    if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:[0-9]{2})", timestamp):
        raise InvalidData("Invalid legacy backfill timestamp")
    try:
        datetime.fromisoformat(timestamp)
    except ValueError:
        raise InvalidData("Invalid legacy backfill timestamp") from None
    # Validate without rewriting either timestamp or result_json, including
    # unknown historical provenance fields and their exact number tokens.
    encode(record, 1 << 20)


class Journal:
    def __init__(self, path, root, source):
        self.root, self.source = identifier(root), identifier(source)
        self.db = sqlite3.connect(Path(path).absolute().as_uri() + "?mode=ro", uri=True, timeout=5)
        try:
            self.db.row_factory = sqlite3.Row
            self.db.execute("PRAGMA query_only=ON")
            self.db.execute("PRAGMA trusted_schema=OFF")
            self.db.execute("BEGIN")
            self.tables = []
            for table, columns in TABLES.items():
                found = self.db.execute("SELECT type FROM sqlite_schema WHERE name=?", (table,)).fetchone()
                if found is None:
                    continue
                actual = {row["name"] for row in self.db.execute("PRAGMA table_info(" + table + ")")}
                if found[0] != "table" or actual != set(columns):
                    raise InvalidData("Unsupported legacy backfill table schema")
                self.tables.append(table)
            if not self.tables:
                raise InvalidData("Journal contains no recognized account-backfill tables")
        except BaseException:
            self.db.close()
            raise

    def close(self):
        self.db.close()

    def records(self):
        for table in self.tables:
            order = "platform,account,component" if table == "backfill_completion" else "platform,account"
            for row in self.db.execute("SELECT * FROM " + table + " ORDER BY " + order):
                record = dict(row)
                validate_record(table, record)
                yield {"root_uuid": self.root, "source_uuid": self.source, "table": table, "record": record}


class ImportClient:
    def __init__(self, endpoint, key_env="STASH_API_KEY"):
        self.endpoint = origin(endpoint)
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key_env):
            raise InvalidData("Invalid Stash application API-key reference")
        self.key_env = key_env
        self.opener = build_opener(ProxyHandler({}), NoRedirect())

    def submit(self, records):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        body = encode({"records": records}, 4 << 20)
        request = Request(self.endpoint + "/api/v3/archive/backfills/import", data=body, method="POST",
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=30) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_import_response")
                result = decode(response.read((64 << 10) + 1), 64 << 10)
        except HTTPError as error:
            status = error.code
            error.close()
            raise Unavailable("backfill_import_rejected", status) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_import_response") from None
        if not isinstance(result, list) or len(result) != len(records):
            raise Unavailable("invalid_import_response")
        for record, decision in zip(records, result):
            completed = record["table"] == "backfill_completion"
            if (not isinstance(decision, dict) or decision.get("uuid") != decision_uuid(record["source_uuid"], record["table"], record["record"])
                    or decision.get("component") != (record["record"]["component"] if completed else "*")
                    or decision.get("outcome") != ("completed" if completed else "skipped")
                    or decision.get("basis") != ("legacy_completion" if completed else "legacy_skip")):
                raise Unavailable("invalid_import_response")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--journal", required=True, help="Consistent run-journal snapshot; opened read-only")
    parser.add_argument("--root", required=True, help="Existing reviewed native root UUID")
    parser.add_argument("--source", required=True, help="Stable input-database UUID from the migration manifest")
    parser.add_argument("--endpoint", help="Native Stash origin; required with --apply")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--apply", action="store_true", help="Import through the application API; otherwise validate and count only")
    args = parser.parse_args(argv)
    acknowledged = 0
    try:
        if args.apply and not args.endpoint:
            raise InvalidData("An explicit native endpoint is required for import")
        with closing(Journal(args.journal, args.root, args.source)) as journal:
            counts = dict.fromkeys(journal.tables, 0)
            # Validate all rows against one SQLite read snapshot before the
            # first network write. Reiterate that same snapshot in bounded batches.
            for record in journal.records():
                counts[record["table"]] += 1
            if args.apply:
                client = ImportClient(args.endpoint, args.api_key_env)
                pending, size = [], 64
                for record in journal.records():
                    length = len(encode(record, (1 << 20) + 1024)) + 1
                    if pending and (len(pending) == 50 or size + length > 4 << 20):
                        client.submit(pending)
                        acknowledged += len(pending)
                        pending, size = [], 64
                    pending.append(record)
                    size += length
                if pending:
                    client.submit(pending)
                    acknowledged += len(pending)
            print(json.dumps({"state": "imported" if args.apply else "validated", "tables": counts,
                              "acknowledged": acknowledged, "source_uuid": journal.source, "root_uuid": journal.root}, sort_keys=True))
            return 0
    except (InvalidData, Unavailable) as error:
        print(json.dumps({"error": str(error), "acknowledged": acknowledged, "resume": "repeat_same_source_uuid"}), file=sys.stderr)
    except (OSError, sqlite3.Error):
        print(json.dumps({"error": "journal_unavailable", "acknowledged": acknowledged, "resume": "repeat_same_source_uuid"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
