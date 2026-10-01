"""Import native accounts and catalog collections from a frozen registry."""

import argparse
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
from .catalog_identity_import import EXTERNAL_TABLES, TABLES, snapshot as read_snapshot
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier


KEYS = {
    "catalogs": ("id",), "routes": ("route",), "links": ("source_id", "target_id"),
    "account_identifiers": ("catalog_id", "account_key", "namespace", "kind", "value", "alias_key", "basis", "origin"),
    "account_identifier_checkpoints": ("catalog_id",), "account_profile_urls": ("account_key", "url"),
}


def snapshot(path, captured_at):
    document = read_snapshot(path, captured_at, tables_schema=EXTERNAL_TABLES, external_schema=TABLES,
                             key_schema=KEYS, empty_keys={("account_identifiers", "alias_key")},
                             max_records=25000, max_bytes=16 << 20)
    tables = document["tables"]
    if not {"catalogs", "routes", "links"} <= tables.keys():
        raise InvalidData("Registry catalog tables are incomplete")
    if ("account_identifiers" in tables) != ("account_identifier_checkpoints" in tables):
        raise InvalidData("Registry identifier tables are incomplete")
    return document


def receipt_fields(binding):
    document = binding["document"]
    hashed = {key: binding[key] for key in ("uuid", "source_uuid", "identity_import_uuid")}
    hashed["document_sha256"] = digest(encode(document, 16 << 20))
    return {**{key: binding[key] for key in ("uuid", "source_uuid", "identity_import_uuid")},
            "input_sha256": digest(encode(hashed)), "captured_at": document["captured_at"],
            "record_count": sum(map(len, document["tables"].values())),
            "inventory": {"retained_tables": {key: len(rows) for key, rows in document["tables"].items()},
                          "external_tables": document["external_tables"]}}


class CatalogRegistryClient(ImportClient):
    def submit(self, binding, expected=None):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        if expected is not None and not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise InvalidData("Apply requires the reviewed plan digest")
        value = binding if expected is None else {"binding": binding, "expected_plan_sha256": expected}
        suffix = "/preview" if expected is None else ""
        request = Request(self.endpoint + "/api/v3/archive/catalog-registry-imports" + suffix,
                          data=encode(value, 17 << 20), method="POST",
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_catalog_registry_response")
                result = decode(response.read((33 << 20) + 1), 33 << 20)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable("catalog_registry_import_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_catalog_registry_response") from None
        if (not isinstance(result, dict) or any(result.get(key) != item for key, item in receipt_fields(binding).items())
                or not isinstance(result.get("plan_sha256"), str) or not re.fullmatch(r"[0-9a-f]{64}", result["plan_sha256"])
                or (expected is not None and result["plan_sha256"] != expected)
                or any(not isinstance(result.get(key), list) for key in ("accounts", "account_keys", "collections", "ownership", "records"))
                or len(result["records"]) != result["record_count"]
                or (expected is not None and not isinstance(result.get("created_at"), str))):
            raise Unavailable("invalid_catalog_registry_response")
        return result


def read_file(path):
    with Path(path).open("rb") as source:
        value = decode(source.read((33 << 20) + 1), 33 << 20)
    if not isinstance(value, dict):
        raise InvalidData("Frozen input must be an object")
    return value.get("binding", value)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--registry", help="The same frozen registry used by the performer identity import")
    source.add_argument("--binding", help="Previously prepared registry binding or saved preview")
    parser.add_argument("--identity-import", help="Saved performer import binding/preview/receipt for this frozen registry")
    parser.add_argument("--snapshot", help="UUID for this registry import, unchanged across retries")
    parser.add_argument("--endpoint")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--expected-sha256", help="Reviewed server plan digest, required with --apply")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args(argv)
    try:
        if args.apply and not all((args.endpoint, args.expected_sha256)):
            raise InvalidData("Apply requires an explicit native endpoint and reviewed plan digest")
        if args.binding:
            if args.identity_import or args.snapshot:
                raise InvalidData("A frozen registry binding cannot be overridden")
            binding = read_file(args.binding)
        else:
            if not args.identity_import or not args.snapshot:
                raise InvalidData("Preparation requires the saved identity import and a stable registry import UUID")
            parent = read_file(args.identity_import)
            captured_at = parent.get("captured_at") or parent["document"]["captured_at"]
            binding = {"uuid": identifier(args.snapshot), "source_uuid": identifier(parent["source_uuid"]),
                       "identity_import_uuid": identifier(parent["uuid"]), "document": snapshot(args.registry, captured_at)}
        if not isinstance(binding, dict) or set(binding) != {"uuid", "source_uuid", "identity_import_uuid", "document"}:
            raise InvalidData("Unsupported registry binding shape")
        for key in ("uuid", "source_uuid", "identity_import_uuid"):
            identifier(binding[key])
        result = receipt_fields(binding)
        if args.endpoint:
            result = CatalogRegistryClient(args.endpoint, args.api_key_env).submit(binding, args.expected_sha256 if args.apply else None)
        if args.expected_sha256 is not None and result.get("plan_sha256") != args.expected_sha256:
            raise InvalidData("Registry plan differs from the reviewed digest")
        print(json.dumps({**result, "binding": binding, "action": "applied" if args.apply else "preview" if args.endpoint else "prepared"}, sort_keys=True))
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (KeyError, TypeError, AttributeError):
        message = "Unsupported registry binding shape"
    except (OSError, sqlite3.Error):
        message = "Frozen registry input is unavailable"
    print(json.dumps({"error": message, "acknowledged": False, "resume": "repeat_same_binding_and_plan_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
