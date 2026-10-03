"""Prepare, review and resume activation of imported queue holds."""

import argparse
from dataclasses import dataclass
from typing import Callable
from collections import Counter
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile

from .album_backfill import write_private
from .catalog_snapshot import sync_directory
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier, utc_now
from .endpoint import origin
from .activation_client import activation_input, integer, sha256

MAX_MANIFEST_BYTES = 8 << 20


@dataclass(frozen=True)
class ActivationRules:
    kind: str
    max_record_bytes: int
    max_batch: int
    max_candidates: int
    dispositions: frozenset
    validate_candidate: Callable
    validate_preview: Callable
    activation_input: Callable = activation_input
    review_dispositions: frozenset = frozenset()

    @property
    def format(self):
        return f"stash-{self.kind}-activation-plan-v1"

    @property
    def max_pages(self):
        return self.max_candidates // self.max_batch


def validate_record(record, snapshot, manifest_sha256, after, rules):
    if not isinstance(record, dict) or set(record) != {"after", "candidates", "preview"} or record["after"] != after:
        raise InvalidData("Invalid activation plan page")
    integer(record["after"])
    candidates = record["candidates"]
    if not isinstance(candidates, list) or not 1 <= len(candidates) <= rules.max_batch:
        raise InvalidData("Invalid activation plan candidate list")
    seen = set()
    for candidate in candidates:
        rules.validate_candidate(candidate, after)
        after = candidate["ordinal"]
        target = candidate["target_uuid"]
        if target in seen:
            raise InvalidData("Duplicate planned activation target")
        seen.add(target)
    eligible = any(row["disposition"] == "eligible" for row in candidates)
    preview = record["preview"]
    if eligible:
        if not isinstance(preview, dict) or not isinstance(preview.get("input"), dict):
            raise InvalidData("Eligible targets require a saved preview")
        expected = rules.activation_input(preview["input"].get("uuid"), snapshot, manifest_sha256, candidates)
        rules.validate_preview(preview, expected, candidates)
    elif preview is not None:
        raise InvalidData("An excluded page cannot release targets")
    return record


def prepare(client, output, snapshot, manifest_sha256, rules):
    identifier(snapshot)
    sha256(manifest_sha256)
    destination = Path(output).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Activation plan already exists; reuse it or choose a new destination")
    parent = destination.parent.resolve(strict=True)
    stage = Path(tempfile.mkdtemp(prefix=f".stash-{rules.kind}-plan-", dir=parent))
    manifest = {"format": rules.format, "endpoint": client.endpoint, "snapshot_uuid": snapshot, "manifest_sha256": manifest_sha256,
                "created_at": utc_now(), "pages": [], "candidates": 0, "counts": {}, "end_ordinal": 0}
    counts, seen, operations = Counter(), set(), set()
    try:
        for index, candidates in enumerate(client.candidate_pages(snapshot, manifest_sha256)):
            if index >= rules.max_pages:
                raise InvalidData("Activation plan has too many pages")
            preview = client.preview(snapshot, manifest_sha256, candidates) if any(row["disposition"] == "eligible" for row in candidates) else None
            record = validate_record({"after": manifest["end_ordinal"], "candidates": candidates, "preview": preview}, snapshot, manifest_sha256, manifest["end_ordinal"], rules)
            for row in candidates:
                if row["target_uuid"] in seen:
                    raise InvalidData("Duplicate activation target across pages")
                seen.add(row["target_uuid"])
                counts[row["disposition"]] += 1
            if preview is not None:
                operation = preview["input"]["uuid"]
                if operation in operations:
                    raise InvalidData("Duplicate activation operation")
                operations.add(operation)
            body = encode(record, rules.max_record_bytes - 1) + b"\n"
            write_private(stage / f"page-{index:06d}.json", body)
            manifest["pages"].append({"index": index, "sha256": digest(body)})
            manifest["end_ordinal"] = candidates[-1]["ordinal"]
        manifest["candidates"], manifest["counts"] = len(seen), dict(counts)
        body = encode(manifest, MAX_MANIFEST_BYTES - 1) + b"\n"
        write_private(stage / "manifest.json", body)
        sync_directory(stage)
        destination.mkdir(mode=0o700)
        try:
            os.rename(stage, destination)
        except BaseException:
            destination.rmdir()
            raise
        sync_directory(parent)
    finally:
        if stage.exists():
            shutil.rmtree(stage)
    return {"action": "prepared", "plan_sha256": digest(body), "candidates": len(seen), "counts": dict(counts),
            "batches": len(operations), "activated": False, "execution_status": "not_checked"}


class Plan:
    def __init__(self, directory, expected_sha256, endpoint=None, *, rules, verify_pages=True):
        self.rules = rules
        sha256(expected_sha256)
        self.directory = Path(directory).resolve(strict=True)
        body = read_regular(self.directory / "manifest.json", MAX_MANIFEST_BYTES)
        if digest(body) != expected_sha256:
            raise InvalidData("Activation plan differs from the reviewed manifest digest")
        manifest = decode(body, MAX_MANIFEST_BYTES)
        fields = {"format", "endpoint", "snapshot_uuid", "manifest_sha256", "created_at", "pages", "candidates", "counts", "end_ordinal"}
        if (not isinstance(manifest, dict) or set(manifest) != fields or manifest["format"] != rules.format
                or not isinstance(manifest["pages"], list) or len(manifest["pages"]) > rules.max_pages
                or not isinstance(manifest["counts"], dict) or set(manifest["counts"]) - rules.dispositions):
            raise InvalidData("Unsupported activation plan")
        if origin(manifest["endpoint"]) != manifest["endpoint"] or (endpoint is not None and origin(endpoint) != manifest["endpoint"]):
            raise InvalidData("Activation plan belongs to another Stash endpoint")
        identifier(manifest["snapshot_uuid"])
        sha256(manifest["manifest_sha256"])
        source_time(manifest["created_at"])
        integer(manifest["candidates"], 0, rules.max_candidates)
        integer(manifest["end_ordinal"])
        for value in manifest["counts"].values():
            integer(value, 1, rules.max_candidates)
        self.manifest, self.sha256 = manifest, expected_sha256
        self.verified = False
        for index, item in enumerate(manifest["pages"]):
            if not isinstance(item, dict) or set(item) != {"index", "sha256"} or integer(item["index"]) != index:
                raise InvalidData("Invalid activation plan page index")
            sha256(item["sha256"])
        if verify_pages:
            self.verify()

    def verify(self):
        rules = self.rules
        manifest = self.manifest
        counts, seen, operations, after = Counter(), set(), set(), 0
        # Validate all saved pages before any Apply. Each later read also checks
        # its hash; a response loss never replaces reviewed operation identities.
        for index, item in enumerate(manifest["pages"]):
            record = self.record(index, after)
            for candidate in record["candidates"]:
                if candidate["target_uuid"] in seen:
                    raise InvalidData("Duplicate planned target across pages")
                seen.add(candidate["target_uuid"])
                counts[candidate["disposition"]] += 1
            preview = record["preview"]
            if preview is not None:
                operation = preview["input"]["uuid"]
                if operation in operations:
                    raise InvalidData("Duplicate planned operation")
                operations.add(operation)
            after = record["candidates"][-1]["ordinal"]
            if index < len(manifest["pages"]) - 1 and len(record["candidates"]) != rules.max_batch:
                raise InvalidData("A short candidate page must end discovery")
        if len(seen) != manifest["candidates"] or dict(counts) != manifest["counts"] or after != manifest["end_ordinal"]:
            raise InvalidData("Activation plan summary differs from its pages")
        self.verified = True

    def record(self, index, after=None):
        rules = self.rules
        integer(index, 0, len(self.manifest["pages"]) - 1)
        item = self.manifest["pages"][index]
        body = read_regular(self.directory / f"page-{index:06d}.json", rules.max_record_bytes)
        if digest(body) != item["sha256"]:
            raise InvalidData("Activation plan page changed after review")
        record = decode(body, rules.max_record_bytes)
        cursor = record.get("after") if isinstance(record, dict) and after is None else after
        return validate_record(record, self.manifest["snapshot_uuid"], self.manifest["manifest_sha256"], cursor, rules)


def inspect_plan(client, plan, apply=False):
    if not plan.verified:
        plan.verify()
    counts, conflicts = Counter(), []
    for index in range(len(plan.manifest["pages"])):
        record = plan.record(index)
        preview = record["preview"]
        if preview is None:
            continue
        size = len(preview["input"]["targets"])
        try:
            receipt = client.apply(preview) if apply else client.status(preview)
            counts["activated" if receipt is not None else "not_activated"] += size
        except Unavailable as error:
            if error.status != 409:
                raise
            counts["conflict"] += size
            conflicts.append({"page": index, "activation_uuid": preview["input"]["uuid"]})
    review = sum(plan.manifest["counts"].get(state, 0) for state in plan.rules.review_dispositions)
    return {"plan_sha256": plan.sha256, "candidates": plan.manifest["candidates"],
            "eligible": plan.manifest["counts"].get("eligible", 0), "discovery_counts": plan.manifest["counts"],
            "counts": dict(counts), "conflicts": conflicts, "activation_complete": not (counts["not_activated"] or counts["conflict"] or review),
            "needs_review": bool(conflicts or review), "pending": bool(counts["not_activated"]), "execution_status": "not_checked"}


def main(argv, rules, client_factory):
    parser = argparse.ArgumentParser(description=f"Prepare, review and resume imported {rules.kind} holds.")
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("prepare", "apply", "status", "show"):
        command = commands.add_parser(name)
        if name != "show":
            command.add_argument("--endpoint", required=True)
            command.add_argument("--api-key-env", default="STASH_API_KEY")
        if name == "prepare":
            command.add_argument("--snapshot", required=True, help=f"Native UUID of the completed frozen {rules.kind} mapping")
            command.add_argument("--manifest-sha256", required=True, help="Original frozen automation manifest digest")
            command.add_argument("--output", required=True, help="New private plan directory; never overwritten")
        else:
            command.add_argument("--plan", required=True)
            command.add_argument("--expected-sha256", required=True, help="Reviewed activation plan manifest digest")
        if name == "show":
            command.add_argument("--page", required=True, type=int)
    args = parser.parse_args(argv)
    try:
        client = None if args.command == "show" else client_factory(args.endpoint, args.api_key_env)
        if args.command == "prepare":
            result = prepare(client, args.output, args.snapshot, args.manifest_sha256, rules)
        else:
            plan = Plan(args.plan, args.expected_sha256, None if client is None else client.endpoint, rules=rules, verify_pages=args.command != "show")
            result = plan.record(args.page) if args.command == "show" else inspect_plan(client, plan, args.command == "apply")
        print(json.dumps(result, sort_keys=True, indent=2 if args.command == "show" else None))
        return 2 if result.get("needs_review") else 3 if result.get("pending") else 0
    except (InvalidData, Unavailable) as error:
        message, status = str(error), error.status if isinstance(error, Unavailable) else None
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message, status = "Activation plan is unavailable or invalid", None
    print(json.dumps({"error": message, "status": status, "activation_complete": False,
                      "resume": "reuse_saved_plan_and_reviewed_digest"}), file=sys.stderr)
    return 1
