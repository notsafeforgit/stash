"""Prepare, preview and apply a frozen catalog performer-registry snapshot."""

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
from urllib.request import Request

from .backfill_import import ImportClient
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier


TABLES = {
    "performer_identities": ("id", "profile_json", "redirect_to", "created_at", "updated_at"),
    "performer_identity_bindings": ("namespace", "performer_id", "identity_id", "profile_json", "redirect_to", "updated_at"),
    "performer_account_associations": ("account_key", "identity_id", "source", "reason", "updated_at"),
    "performer_identity_events": ("id", "kind", "payload_json", "created_at"),
    "performer_identity_migrations": ("name", "summary_json", "completed_at"),
    "catalog_metadata_performers": ("namespace", "performer_id", "profile_json", "redirect_to"),
    "catalog_metadata_accounts": ("namespace", "account_key", "performer_id"),
}
EXTERNAL_TABLES = {
    "catalogs": ("id", "kind", "label", "owner_key", "created_at", "redirect_to"),
    "routes": ("route", "catalog_id"),
    "links": ("source_id", "target_id", "reason", "linked_at"),
    "account_identifiers": ("catalog_id", "account_key", "platform", "namespace", "kind", "value", "handle", "alias_key", "basis", "origin", "first_observed", "last_observed"),
    "account_identifier_checkpoints": ("catalog_id", "captured_at", "version"),
    "account_profile_urls": ("account_key", "url", "basis", "first_observed"),
}
KEYS = {
    "performer_identities": ("id",), "performer_identity_bindings": ("namespace", "performer_id"),
    "performer_account_associations": ("account_key",), "performer_identity_events": ("id",),
    "performer_identity_migrations": ("name",), "catalog_metadata_performers": ("namespace", "performer_id"),
    "catalog_metadata_accounts": ("namespace", "account_key"),
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
            columns = TABLES.get(table) or EXTERNAL_TABLES.get(table)
            if entry["type"] != "table" or columns is None:
                raise InvalidData("Unknown registry table or view; review the original snapshot")
            actual = {row["name"] for row in db.execute("PRAGMA table_info(" + table + ")")}
            if actual != set(columns):
                raise InvalidData("Unsupported registry columns; review the original snapshot")
            if table in EXTERNAL_TABLES:
                external[table] = db.execute("SELECT count(*) FROM " + table).fetchone()[0]
                continue
            records, seen = [], set()
            for row in db.execute("SELECT * FROM " + table + " ORDER BY " + ",".join(KEYS[table])):
                record = dict(row)
                keys = tuple(record[key] for key in KEYS[table])
                if any(not isinstance(key, str) or not key for key in keys) or keys in seen:
                    raise InvalidData("Invalid or duplicate registry row key")
                seen.add(keys)
                body = encode(record, 1 << 20)
                total += 1
                size += len(body)
                if total > 10000 or size > 8 << 20:
                    raise InvalidData("Registry identities exceed the atomic import limit")
                for key in ("profile_json", "payload_json", "summary_json"):
                    if key in record:
                        value = record[key]
                        if not isinstance(value, str) or not isinstance(decode(value.encode(), 1 << 20), dict):
                            raise InvalidData("Registry embedded JSON must be an object")
                records.append(record)
            tables[table] = records
    if not tables:
        raise InvalidData("Registry has no recognized performer identity tables")
    modern = {table for table in TABLES if table.startswith("performer_")}
    if tables.keys() & modern and not modern <= tables.keys():
        raise InvalidData("Registry performer identity tables are incomplete")
    document = {"captured_at": captured_at, "tables": tables, "external_tables": external}
    encode(document, 8 << 20)
    return document


def receipt_fields(binding):
    # Matches json-v1 Go map encoding. The document digest uses the actual
    # Python request bytes; embedded profile JSON text remains unchanged.
    document = binding["document"]
    hashed = {key: binding[key] for key in ("uuid", "source_uuid", "namespace", "account_bindings")}
    hashed["document_sha256"] = digest(encode(document, 8 << 20))
    body = encode(hashed, 1 << 20).replace(b"\xe2\x80\xa8", b"\\u2028").replace(b"\xe2\x80\xa9", b"\\u2029")
    return {**{key: binding[key] for key in ("uuid", "source_uuid", "namespace")}, "input_sha256": digest(body),
            "captured_at": document["captured_at"], "record_count": sum(map(len, document["tables"].values())),
            "inventory": {"retained_tables": {key: len(rows) for key, rows in document["tables"].items()},
                          "external_tables": document["external_tables"]}}


class CatalogIdentityClient(ImportClient):
    def submit(self, binding, expected=None):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        if expected is not None and not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise InvalidData("Apply requires the reviewed plan digest")
        value = binding if expected is None else {"binding": binding, "expected_plan_sha256": expected}
        suffix = "/preview" if expected is None else ""
        request = Request(self.endpoint + "/api/v3/archive/catalog-identity-imports" + suffix,
                          data=encode(value, 9 << 20), method="POST",
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_catalog_identity_response")
                result = decode(response.read((9 << 20) + 1), 9 << 20)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable("catalog_identity_import_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_catalog_identity_response") from None
        if (not isinstance(result, dict) or any(result.get(key) != item for key, item in receipt_fields(binding).items())
                or not isinstance(result.get("plan_sha256"), str) or not re.fullmatch(r"[0-9a-f]{64}", result["plan_sha256"])
                or (expected is not None and result["plan_sha256"] != expected)
                or not isinstance(result.get("identities"), list) or not isinstance(result.get("ownership"), list)
                or not isinstance(result.get("records"), list) or len(result["records"]) != result["record_count"]
                or (expected is not None and not isinstance(result.get("created_at"), str))):
            raise Unavailable("invalid_catalog_identity_response")
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--registry", help="Frozen registry SQLite database, opened read-only")
    source.add_argument("--binding", help="Previously prepared binding JSON or a saved preview containing binding")
    parser.add_argument("--source", help="Stable original registry database UUID from the migration manifest")
    parser.add_argument("--snapshot", help="UUID for this frozen snapshot, unchanged across retries")
    parser.add_argument("--captured-at", help="Fixed snapshot time from the migration manifest")
    parser.add_argument("--namespace", help="Explicit Stash binding namespace in this registry")
    parser.add_argument("--account-bindings", help="JSON object mapping old account keys to explicitly reviewed native account UUIDs")
    parser.add_argument("--endpoint")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--expected-sha256", help="Reviewed server plan digest; required with --apply")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args(argv)
    try:
        if args.apply and not all((args.endpoint, args.expected_sha256)):
            raise InvalidData("Apply requires an explicit native endpoint and reviewed plan digest")
        if args.binding:
            if any((args.source, args.snapshot, args.captured_at, args.namespace, args.account_bindings)):
                raise InvalidData("A frozen binding cannot be overridden by registry preparation options")
            with Path(args.binding).open("rb") as file:
                value = decode(file.read((9 << 20) + 1), 9 << 20)
            if not isinstance(value, dict):
                raise InvalidData("Frozen binding must be an object")
            binding = value.get("binding", value)
        else:
            if not all((args.source, args.snapshot, args.captured_at, args.namespace)):
                raise InvalidData("Registry preparation requires source/snapshot UUIDs, namespace and snapshot time")
            accounts = {}
            if args.account_bindings:
                with Path(args.account_bindings).open("rb") as file:
                    accounts = decode(file.read((1 << 20) + 1), 1 << 20)
            binding = {"uuid": identifier(args.snapshot), "source_uuid": identifier(args.source), "namespace": args.namespace,
                       "account_bindings": accounts, "document": snapshot(args.registry, args.captured_at)}
        if (not isinstance(binding, dict) or set(binding) != {"uuid", "source_uuid", "namespace", "account_bindings", "document"}
                or not isinstance(binding["account_bindings"], dict) or not isinstance(binding["namespace"], str)):
            raise InvalidData("Unsupported frozen binding shape")
        identifier(binding["uuid"])
        identifier(binding["source_uuid"])
        for key, value in binding["account_bindings"].items():
            if not isinstance(key, str) or not key:
                raise InvalidData("Account bindings require exact legacy keys")
            identifier(value)
        result = receipt_fields(binding)
        if args.endpoint:
            result = CatalogIdentityClient(args.endpoint, args.api_key_env).submit(binding, args.expected_sha256 if args.apply else None)
        if args.expected_sha256 is not None and result.get("plan_sha256") != args.expected_sha256:
            raise InvalidData("Catalog identity plan differs from the reviewed digest")
        print(json.dumps({**result, "binding": binding, "action": "applied" if args.apply else "preview" if args.endpoint else "prepared"}, sort_keys=True))
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (KeyError, TypeError, AttributeError):
        message = "Unsupported frozen binding shape"
    except (OSError, sqlite3.Error):
        message = "Frozen registry snapshot is unavailable"
    print(json.dumps({"error": message, "acknowledged": False, "resume": "repeat_same_binding_and_plan_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
