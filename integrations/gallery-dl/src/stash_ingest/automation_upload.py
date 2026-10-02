"""Retain a frozen automation snapshot through Stash's resumable migration API."""

import argparse
import json
import sys

from .automation_snapshot import verify
from .catalog_source import source_time
from .catalog_upload import CatalogUploadClient
from .client import Unavailable
from .encoding import InvalidData, identifier


class AutomationUploadClient(CatalogUploadClient):
    snapshot_path = "/api/v3/archive/automation-snapshots"
    receipt_error = "invalid_automation_snapshot_receipt"
    rejected_error = "automation_snapshot_rejected"
    binding_error = "automation_snapshot_binding_changed"
    binding_keys = ("registry_import_uuid", "created_at")
    verify_snapshot = staticmethod(verify)

    @staticmethod
    def validate_receipt(result, manifest, manifest_sha256, minimum=0):
        try:
            if not isinstance(result, dict):
                raise InvalidData("Receipt must be an object")
            count = result["next_chunk"]
            chunks = manifest["chunks"]
            expected = {key: manifest[key] for key in ("snapshot_uuid", "registry_source_uuid", "source_sha256", "captured_at", "records")}
            expected.update(manifest_sha256=manifest_sha256, chunks=len(chunks), bytes=sum(c["bytes"] for c in chunks),
                            pending_families=sorted(manifest["tables"]), state="received" if count == len(chunks) else "receiving")
            if (type(count) is not int or not minimum <= count <= len(chunks)
                    or any(type(result.get(key)) is not int for key in ("chunks", "records", "bytes", "received_records", "received_bytes"))
                    or any(result.get(key) != value for key, value in expected.items()) or result.get("imported") is not False
                    or result.get("received_records") != sum(c["rows"] for c in chunks[:count])
                    or result.get("received_bytes") != sum(c["bytes"] for c in chunks[:count])):
                raise InvalidData("Receipt does not match the frozen input")
            identifier(result["registry_import_uuid"])
            source_time(result["created_at"])
            source_time(result["updated_at"])
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_automation_snapshot_receipt") from None
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", required=True, help="Directory produced by stash-prepare-automation")
    parser.add_argument("--expected-sha256", required=True, help="Reviewed frozen manifest digest")
    parser.add_argument("--endpoint", required=True, help="Explicit native Stash origin")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    args = parser.parse_args(argv)
    try:
        result = AutomationUploadClient(args.endpoint, args.api_key_env).upload(args.snapshot, args.expected_sha256)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message = "Frozen automation snapshot is unavailable or invalid"
    print(json.dumps({"error": message, "acknowledged": False, "imported": False,
                      "resume": "repeat_same_snapshot_and_manifest_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
