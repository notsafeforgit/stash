"""Convert retained legacy checkpoints without activating or publishing them."""

import argparse
import json
from pathlib import Path
import sys

from .catalog_snapshot import MAX_MANIFEST_BYTES, MAX_CHUNK_BYTES
from .automation_snapshot import verify
from .catalog_source import source_time
from .catalog_upload import read_regular
from .automation_upload import AutomationUploadClient
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode


CHECKPOINT_POLICY = "legacy-enrichment-staging-v1"


def checkpoint_count(directory, manifest):
    """Bind the expected converted/review count to the verified frozen chunks."""
    count = 0
    for chunk in manifest["chunks"]:
        body = read_regular(directory / chunk["file"], MAX_CHUNK_BYTES)
        if len(body) != chunk["bytes"] or digest(body) != chunk["sha256"]:
            raise InvalidData("Automation chunk changed after verification")
        for line in body.splitlines():
            row = decode(line, MAX_CHUNK_BYTES, preserve_numbers=True)
            if row["table"] == "enrichment_jobs" and row["values"]["staged_json"] is not None:
                count += 1
    return count


class AutomationCheckpointClient(AutomationUploadClient):
    @staticmethod
    def validate_progress(result, manifest, manifest_sha256, staged_count, prior=None):
        try:
            if not isinstance(result, dict):
                raise InvalidData("Checkpoint progress must be an object")
            counters = ("last_ordinal", "source_records", "processed_records", "mapped_records", "review_records")
            if any(type(result.get(key)) is not int or result[key] < 0 for key in counters):
                raise InvalidData("Invalid checkpoint counters")
            expected = staged_count
            processed = result["processed_records"]
            state = result.get("state")
            if (result.get("snapshot_uuid") != manifest["snapshot_uuid"] or result.get("manifest_sha256") != manifest_sha256
                    or result.get("policy") != CHECKPOINT_POLICY or result.get("imported") is not False or result["source_records"] != expected
                    or not processed <= result["last_ordinal"] <= manifest["records"] or processed > expected
                    or state not in ("running", "mapped", "review")
                    or (state != "running" and processed != expected)
                    or result["mapped_records"] + result["review_records"] != processed
                    or (state == "mapped" and result["review_records"] != 0)
                    or (state == "review" and result["review_records"] == 0)):
                raise InvalidData("Checkpoint progress does not match the frozen snapshot")
            for key in ("created_at", "updated_at"):
                source_time(result[key])
            if prior is not None:
                if (any(result[key] < prior[key] for key in counters) or result["created_at"] != prior["created_at"]
                        or (state == "running" and result["last_ordinal"] <= prior["last_ordinal"])
                        or (prior["state"] != "running" and result != prior)):
                    raise InvalidData("Checkpoint progress regressed or stalled")
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_automation_checkpoint_progress") from None
        return result

    def import_checkpoints(self, directory, expected_sha256):
        directory = Path(directory).resolve(strict=True)
        verify(directory, expected_sha256)
        body = read_regular(directory / "manifest.json", MAX_MANIFEST_BYTES)
        if digest(body) != expected_sha256:
            raise InvalidData("Snapshot manifest changed after verification")
        manifest = decode(body, MAX_MANIFEST_BYTES)
        staged_count = checkpoint_count(directory, manifest)
        suffix = f"/{manifest['snapshot_uuid']}"
        staged = self.request("GET", suffix, None, expected_sha256, "application/json")
        self.validate_receipt(staged, manifest, expected_sha256, len(manifest["chunks"]))
        suffix += "/enrichment-checkpoints"
        try:
            result = self.validate_progress(self.request("GET", suffix, None, expected_sha256, "application/json"), manifest, expected_sha256, staged_count)
        except Unavailable as error:
            if error.status != 404:
                raise
            result = None
        while result is None or result["state"] == "running":
            body = encode({"expected_manifest_sha256": expected_sha256, "after": result["last_ordinal"] if result else 0})
            result = self.validate_progress(self.request("POST", suffix, body, expected_sha256, "application/json"), manifest, expected_sha256, staged_count, result)
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", required=True, help="Received frozen automation input; finish the enrichment mapping first")
    parser.add_argument("--expected-sha256", required=True, help="Reviewed frozen manifest digest")
    parser.add_argument("--endpoint", required=True, help="Explicit native Stash origin")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    args = parser.parse_args(argv)
    try:
        result = AutomationCheckpointClient(args.endpoint, args.api_key_env).import_checkpoints(args.snapshot, args.expected_sha256)
        print(json.dumps(result, sort_keys=True))
        return 2 if result["state"] == "review" else 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message = "Frozen automation snapshot is unavailable or invalid"
    print(json.dumps({"error": message, "acknowledged": False, "imported": False,
                      "resume": "repeat_same_snapshot_and_manifest_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
