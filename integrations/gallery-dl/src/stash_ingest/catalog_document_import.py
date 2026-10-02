"""Import retained catalog documents and historical selections without applying entity metadata."""

import argparse
import json
from pathlib import Path
import sys

from .catalog_snapshot import MAX_MANIFEST_BYTES, verify
from .catalog_source import source_time
from .catalog_upload import CatalogUploadClient, read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier


DOCUMENT_POLICY = "catalog-documents-v1"
PHASES = ("sidecar_documents", "sidecar_sources", "sidecars", "sidecar_heads", "complete")


class CatalogDocumentClient(CatalogUploadClient):
    @staticmethod
    def validate_progress(result, manifest, manifest_sha256, collection_uuid, prior=None):
        try:
            if not isinstance(result, dict):
                raise InvalidData("Document progress must be an object")
            identifier(collection_uuid)
            counters = ("source_records", "processed_records", "mapped_records", "review_records")
            if any(type(result.get(key)) is not int or result[key] < 0 for key in (*counters, "last_ordinal")):
                raise InvalidData("Invalid document import counters")
            counts = [manifest["tables"].get(table, {}).get("rows", 0) for table in PHASES[:-1]]
            expected = sum(counts)
            processed, state, phase = result["processed_records"], result.get("state"), result.get("phase")
            if (result.get("snapshot_uuid") != manifest["snapshot_uuid"] or result.get("manifest_sha256") != manifest_sha256
                    or result.get("collection_uuid") != collection_uuid
                    or type(result.get("collection_revision")) is not int or result["collection_revision"] != 1
                    or result.get("policy") != DOCUMENT_POLICY or result.get("imported") is not False
                    or result["source_records"] != expected or processed > expected
                    or result["last_ordinal"] > manifest["records"] or phase not in PHASES or state not in ("running", "mapped", "review")
                    or (state == "running") != (phase != "complete")
                    or (state != "running" and (processed != expected or result["last_ordinal"] != 0))
                    or result["mapped_records"] + result["review_records"] != processed
                    or (state == "mapped" and result["review_records"] != 0)
                    or (state == "review" and result["review_records"] == 0)):
                raise InvalidData("Document progress does not match the frozen snapshot")
            index = PHASES.index(phase)
            prefix = sum(counts[:index])
            if (not prefix <= processed <= prefix + (counts[index] if index < len(counts) else 0)
                    or (processed == prefix) != (result["last_ordinal"] == 0)
                    or result["last_ordinal"] < processed - prefix):
                raise InvalidData("Document progress does not match its phase")
            for key in ("created_at", "updated_at"):
                source_time(result[key])
            if prior is not None:
                if (any(result[key] < prior[key] for key in counters) or result["created_at"] != prior["created_at"]
                        or index < PHASES.index(prior["phase"])
                        or (phase == prior["phase"] and result["last_ordinal"] < prior["last_ordinal"])
                        or (state == "running" and processed <= prior["processed_records"])
                        or (prior["state"] != "running" and result != prior)):
                    raise InvalidData("Document progress regressed or stalled")
        except (InvalidData, KeyError, TypeError, ValueError):
            raise Unavailable("invalid_catalog_document_progress") from None
        return result

    def import_documents(self, directory, expected_sha256):
        directory = Path(directory).resolve(strict=True)
        verify(directory, expected_sha256)
        body = read_regular(directory / "manifest.json", MAX_MANIFEST_BYTES)
        if digest(body) != expected_sha256:
            raise InvalidData("Snapshot manifest changed after verification")
        manifest = decode(body, MAX_MANIFEST_BYTES)
        suffix = f"/{manifest['snapshot_uuid']}"
        staged = self.request("GET", suffix, None, expected_sha256, "application/json")
        self.validate_receipt(staged, manifest, expected_sha256, len(manifest["chunks"]))
        collection = staged["collection_uuid"]
        suffix += "/document-import"
        try:
            result = self.validate_progress(self.request("GET", suffix, None, expected_sha256, "application/json"), manifest, expected_sha256, collection)
        except Unavailable as error:
            if error.status != 404:
                raise
            result = None
        while result is None or result["state"] == "running":
            body = encode({"expected_manifest_sha256": expected_sha256, "after": result["processed_records"] if result else 0})
            result = self.validate_progress(self.request("POST", suffix, body, expected_sha256, "application/json"), manifest, expected_sha256, collection, result)
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", required=True, help="Frozen snapshot with completed upload and evidence-import passes")
    parser.add_argument("--expected-sha256", required=True, help="Reviewed frozen manifest digest")
    parser.add_argument("--endpoint", required=True, help="Explicit native Stash origin")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    args = parser.parse_args(argv)
    try:
        result = CatalogDocumentClient(args.endpoint, args.api_key_env).import_documents(args.snapshot, args.expected_sha256)
        print(json.dumps(result, sort_keys=True))
        return 2 if result["state"] == "review" else 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message = "Frozen catalog snapshot is unavailable or invalid"
    print(json.dumps({"error": message, "acknowledged": False, "imported": False,
                      "resume": "repeat_same_snapshot_and_manifest_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
