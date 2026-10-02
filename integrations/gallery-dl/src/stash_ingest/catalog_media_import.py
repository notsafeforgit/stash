"""Import frozen catalog assets, files and appearances with an explicit root mapping."""

import argparse
import json
from pathlib import Path, PurePosixPath
import sys

from .catalog_snapshot import MAX_MANIFEST_BYTES, verify
from .catalog_source import source_time
from .catalog_upload import CatalogUploadClient, read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier


MEDIA_POLICY = "catalog-media-v1"
BINDING_FIELDS = ("root_uuid", "root_revision", "collection_revision", "library_root_path")
PHASES = ("assets", "files", "appearances", "complete")


def validate_binding(binding):
    if not isinstance(binding, dict) or set(binding) != set(BINDING_FIELDS):
        raise InvalidData("An explicit catalog media root mapping is required")
    identifier(binding["root_uuid"])
    for key in ("root_revision", "collection_revision"):
        if type(binding[key]) is not int or binding[key] < 1:
            raise InvalidData("The reviewed root and collection revisions are required")
    path = binding["library_root_path"]
    if (not isinstance(path, str) or not 1 <= len(path.encode("utf-8")) <= 4096
            or not PurePosixPath(path).is_absolute() or path.startswith("//") or str(PurePosixPath(path)) != path
            or ".." in PurePosixPath(path).parts or any(ord(c) < 32 or ord(c) == 127 for c in path)):
        raise InvalidData("The historical Stash mount must be an absolute canonical path")
    return binding


class CatalogMediaClient(CatalogUploadClient):
    @staticmethod
    def validate_progress(result, manifest, manifest_sha256, binding, prior=None):
        try:
            if not isinstance(result, dict):
                raise InvalidData("Media import progress must be an object")
            validate_binding({key: result[key] for key in BINDING_FIELDS})
            outcomes = ("mapped_records", "review_records", "unavailable_records")
            counters = ("source_records", "processed_records", "matched_files", "media_associations", *outcomes)
            if any(type(result.get(key)) is not int or result[key] < 0 for key in (*counters, "last_ordinal")):
                raise InvalidData("Invalid media import counters")
            expected = sum(manifest["tables"][table]["rows"] for table in PHASES[:-1])
            processed, state, phase = result["processed_records"], result.get("state"), result.get("phase")
            if (result.get("snapshot_uuid") != manifest["snapshot_uuid"] or result.get("manifest_sha256") != manifest_sha256
                    or any(result.get(key) != binding[key] for key in BINDING_FIELDS)
                    or result.get("policy") != MEDIA_POLICY or result.get("imported") is not False
                    or result["source_records"] != expected or processed > expected
                    or result["last_ordinal"] > manifest["records"] or phase not in PHASES or state not in ("running", "mapped", "review")
                    or (state == "running") != (phase != "complete")
                    or (state != "running" and (processed != expected or result["last_ordinal"] != 0))
                    or sum(result[key] for key in outcomes) != processed
                    or result["matched_files"] > manifest["tables"]["files"]["rows"]
                    or result["media_associations"] > manifest["tables"]["appearances"]["rows"]
                    or result["matched_files"] + result["media_associations"] > result["mapped_records"]
                    or (state == "mapped" and result["review_records"] != 0)
                    or (state == "review" and result["review_records"] == 0)):
                raise InvalidData("Media import progress does not match its reviewed binding")
            for key in ("created_at", "updated_at"):
                source_time(result[key])
            if prior is not None:
                if (any(result[key] < prior[key] for key in counters) or result["created_at"] != prior["created_at"]
                        or PHASES.index(phase) < PHASES.index(prior["phase"])
                        or (phase == prior["phase"] and result["last_ordinal"] < prior["last_ordinal"])
                        or (state == "running" and processed <= prior["processed_records"])):
                    raise InvalidData("Media import progress regressed or stalled")
        except (InvalidData, KeyError, TypeError, ValueError):
            raise Unavailable("invalid_catalog_media_progress") from None
        return result

    def import_media(self, directory, expected_sha256, binding):
        validate_binding(binding)
        directory = Path(directory).resolve(strict=True)
        verify(directory, expected_sha256)
        body = read_regular(directory / "manifest.json", MAX_MANIFEST_BYTES)
        if digest(body) != expected_sha256:
            raise InvalidData("Snapshot manifest changed after verification")
        manifest = decode(body, MAX_MANIFEST_BYTES)
        suffix = f"/{manifest['snapshot_uuid']}"
        staged = self.request("GET", suffix, None, expected_sha256, "application/json")
        self.validate_receipt(staged, manifest, expected_sha256, len(manifest["chunks"]))
        suffix += "/media-import"
        try:
            result = self.validate_progress(self.request("GET", suffix, None, expected_sha256, "application/json"), manifest, expected_sha256, binding)
        except Unavailable as error:
            if error.status != 404:
                raise
            body = encode({"expected_manifest_sha256": expected_sha256, **binding})
            result = self.validate_progress(self.request("POST", suffix, body, expected_sha256, "application/json"), manifest, expected_sha256, binding)
        while result["state"] == "running":
            body = encode({"expected_manifest_sha256": expected_sha256, "after": result["processed_records"]})
            result = self.validate_progress(self.request("POST", suffix + "/advance", body, expected_sha256, "application/json"), manifest, expected_sha256, binding, result)
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", required=True, help="Frozen snapshot with completed evidence import")
    parser.add_argument("--expected-sha256", required=True, help="Reviewed frozen manifest digest")
    parser.add_argument("--root-uuid", required=True, help="Logical native media root; it may remain disabled and offline")
    parser.add_argument("--root-revision", type=int, required=True)
    parser.add_argument("--collection-revision", type=int, required=True)
    parser.add_argument("--library-root-path", required=True, help="Historical mount prefix in the copied Stash database")
    parser.add_argument("--endpoint", required=True, help="Explicit native Stash origin")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    args = parser.parse_args(argv)
    try:
        binding = {key: getattr(args, key) for key in BINDING_FIELDS}
        result = CatalogMediaClient(args.endpoint, args.api_key_env).import_media(args.snapshot, args.expected_sha256, binding)
        print(json.dumps(result, sort_keys=True))
        return 2 if result["state"] == "review" else 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message = "Frozen catalog snapshot or reviewed root mapping is unavailable or invalid"
    print(json.dumps({"error": message, "acknowledged": False, "imported": False,
                      "resume": "repeat_same_snapshot_manifest_and_root_mapping"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
