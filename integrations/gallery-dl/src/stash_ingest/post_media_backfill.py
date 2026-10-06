"""Save, inspect and resume reviewed historical post-to-media matching."""

import argparse
from collections import Counter
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import uuid

from .album_backfill import write_private
from .album_client import integer, sha256
from .catalog_snapshot import sync_directory
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier, utc_now
from .endpoint import origin
from .post_media_client import MAX_POSTS, MAX_RESPONSE, POLICY, STATUSES, PostMediaClient, validate_record

FORMAT = "stash-post-media-backfill-plan-v1"
MAX_MANIFEST = 32 << 20
MAX_PART = MAX_RESPONSE + (1 << 20)
MAX_PLAN = 8 << 30
PART_TARGET = 4 << 20
PART_RECORDS = 100


def prepare(client, output, posts=None):
    destination = Path(output).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Post/media plan already exists; resume it or choose a new destination")
    if posts is not None:
        if not isinstance(posts, list) or len(posts) > MAX_POSTS:
            raise InvalidData("Expected at most one million explicit post UUIDs")
        posts = sorted(identifier(post) for post in posts)
        if len(set(posts)) != len(posts):
            raise InvalidData("Duplicate explicit post")
    parent = destination.parent.resolve(strict=True)
    stage = Path(tempfile.mkdtemp(prefix=".stash-post-media-plan-", dir=parent))
    manifest = {"format": FORMAT, "endpoint": client.endpoint, "policy": POLICY, "created_at": utc_now(),
                "scope": "explicit-posts" if posts is not None else "evidence-posts", "posts": 0, "review_limit_posts": 0, "matches": {}, "parts": []}
    part, part_bytes, total_bytes, last = [], 2, 0, ""
    matches = Counter()

    def flush():
        nonlocal part, part_bytes, total_bytes
        if not part:
            return
        body = encode(part, MAX_PART - 1) + b"\n"
        total_bytes += len(body)
        if total_bytes > MAX_PLAN:
            raise InvalidData("Post/media plan exceeds 8 GiB; prepare smaller explicit scopes")
        name = f"part-{len(manifest['parts']):06d}.json"
        write_private(stage / name, body)
        manifest["parts"].append({"name": name, "sha256": digest(body), "bytes": len(body), "posts": len(part),
                                  "first": part[0]["post_uuid"], "last": part[-1]["post_uuid"]})
        part, part_bytes = [], 2

    try:
        for post in client.posts() if posts is None else posts:
            identifier(post)
            if post <= last or manifest["posts"] >= MAX_POSTS:
                raise InvalidData("Post discovery is unordered, repeated or exceeds its bound")
            last = post
            try:
                preview = client.preview(post)
                record = {"post_uuid": post, "disposition": "ready", "request_uuid": str(uuid.uuid4()), "preview": preview}
                matches.update(row["status"] for row in preview["candidates"])
            except Unavailable as error:
                if error.status != 422:
                    raise
                record = {"post_uuid": post, "disposition": "review_limit"}
                manifest["review_limit_posts"] += 1
            validate_record(record)
            size = len(encode(record, MAX_PART - 2)) + 1
            if part and (len(part) >= PART_RECORDS or part_bytes + size > PART_TARGET):
                flush()
            part.append(record)
            part_bytes += size
            manifest["posts"] += 1
        flush()
        manifest["matches"] = dict(matches)
        body = encode(manifest, MAX_MANIFEST - 1) + b"\n"
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
    return {"action": "prepared", "plan_sha256": digest(body), "posts": manifest["posts"], "parts": len(manifest["parts"]),
            "matches": dict(matches), "review_limit_posts": manifest["review_limit_posts"], "submitted": False}


class Plan:
    def __init__(self, directory, expected_sha256, endpoint=None):
        sha256(expected_sha256)
        self.directory = Path(directory).resolve(strict=True)
        body = read_regular(self.directory / "manifest.json", MAX_MANIFEST)
        if digest(body) != expected_sha256:
            raise InvalidData("Post/media plan differs from its reviewed digest")
        manifest = decode(body, MAX_MANIFEST)
        keys = {"format", "endpoint", "policy", "created_at", "scope", "posts", "review_limit_posts", "matches", "parts"}
        if (not isinstance(manifest, dict) or set(manifest) != keys or manifest["format"] != FORMAT or manifest["policy"] != POLICY
                or manifest["scope"] not in ("explicit-posts", "evidence-posts") or not isinstance(manifest["parts"], list)
                or not isinstance(manifest["matches"], dict) or not set(manifest["matches"]) <= STATUSES):
            raise InvalidData("Unsupported post/media plan manifest")
        if origin(manifest["endpoint"]) != manifest["endpoint"] or endpoint is not None and origin(endpoint) != manifest["endpoint"]:
            raise InvalidData("Post/media plan belongs to a different Stash endpoint")
        source_time(manifest["created_at"])
        integer(manifest["posts"], 0, MAX_POSTS)
        integer(manifest["review_limit_posts"], 0, MAX_POSTS)
        for count in manifest["matches"].values():
            integer(count, 0, MAX_POSTS * 8192)
        self.manifest, self.sha256 = manifest, expected_sha256
        total, last, requests, count, limits = 0, "", set(), 0, 0
        matches = Counter()
        for index, item in enumerate(manifest["parts"]):
            if not isinstance(item, dict) or set(item) != {"name", "sha256", "bytes", "posts", "first", "last"} or item["name"] != f"part-{index:06d}.json":
                raise InvalidData("Invalid post/media plan part")
            sha256(item["sha256"])
            total += integer(item["bytes"], 2, MAX_PART)
            integer(item["posts"], 1, PART_RECORDS)
            identifier(item["first"])
            identifier(item["last"])
            if total > MAX_PLAN or item["first"] <= last or item["last"] < item["first"]:
                raise InvalidData("Post/media plan parts are unordered or exceed their size bound")
            for record in self.part(item):
                if record["post_uuid"] <= last:
                    raise InvalidData("Duplicate or unordered planned post")
                last = record["post_uuid"]
                count += 1
                if count > MAX_POSTS:
                    raise InvalidData("Post/media plan exceeds its post bound")
                if record["disposition"] == "review_limit":
                    limits += 1
                else:
                    request = record["request_uuid"]
                    if request in requests:
                        raise InvalidData("Duplicate planned post/media request")
                    requests.add(request)
                    matches.update(row["status"] for row in record["preview"]["candidates"])
        if count != manifest["posts"] or limits != manifest["review_limit_posts"] or dict(matches) != manifest["matches"]:
            raise InvalidData("Post/media manifest obscures a planned outcome")

    def part(self, item):
        body = read_regular(self.directory / item["name"], MAX_PART)
        if len(body) != item["bytes"] or digest(body) != item["sha256"]:
            raise InvalidData("Post/media plan part changed after review")
        rows = decode(body, MAX_PART)
        if not isinstance(rows, list) or not rows or len(rows) != item["posts"]:
            raise InvalidData("Invalid post/media plan part length")
        for row in rows:
            validate_record(row)
        if rows[0]["post_uuid"] != item["first"] or rows[-1]["post_uuid"] != item["last"]:
            raise InvalidData("Post/media part changed its range")
        return rows

    def records(self):
        # Recheck each bounded part just before use, including after a saved
        # Plan instance was opened. No changed request may reach the server.
        for item in self.manifest["parts"]:
            yield from self.part(item)

    def record(self, post):
        identifier(post)
        for item in self.manifest["parts"]:
            if item["first"] <= post <= item["last"]:
                for record in self.part(item):
                    if record["post_uuid"] == post:
                        return record
        raise InvalidData("This plan does not contain that post")


def inspect_plan(client, plan, apply=False, emit=None):
    if client.endpoint != plan.manifest["endpoint"]:
        raise InvalidData("Post/media plan belongs to a different Stash endpoint")
    counts, outcomes, examples = Counter(), Counter(), []
    for record in plan.records():
        receipt = None
        if record["disposition"] == "review_limit":
            state = "review_limit"
        else:
            try:
                receipt = client.apply(record) if apply else client.status(record)
                state = "committed" if receipt is not None else "not_submitted"
                if receipt is not None:
                    outcomes.update({key: receipt[key] for key in ("selected", "preserved", "review", "unavailable")})
            except Unavailable as error:
                if error.status != 409:
                    raise
                state = "conflict"
        counts[state] += 1
        if state in ("review_limit", "conflict") and len(examples) < 20:
            examples.append({"post_uuid": record["post_uuid"], "state": state})
        if emit is not None:
            emit({"post_uuid": record["post_uuid"], "state": state, "receipt": receipt})
    return {"plan_sha256": plan.sha256, "posts": plan.manifest["posts"], "counts": dict(counts), "outcomes": dict(outcomes),
            "processed": counts["committed"] + counts["review_limit"] == plan.manifest["posts"],
            "pending": bool(counts["not_submitted"]),
            "needs_review": bool(counts["review_limit"] or counts["conflict"] or outcomes["review"] or outcomes["unavailable"]),
            "examples": examples}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("prepare", "apply", "status", "show"):
        command = commands.add_parser(name)
        if name != "show":
            command.add_argument("--endpoint", required=True)
            command.add_argument("--api-key-env", default="STASH_API_KEY")
        if name == "prepare":
            command.add_argument("--output", required=True, help="New immutable private plan directory")
            command.add_argument("--posts-file", help="Optional JSON array of post UUIDs; otherwise discover retained post/file evidence")
        else:
            command.add_argument("--plan", required=True)
            command.add_argument("--expected-sha256", required=True)
        if name == "show":
            command.add_argument("--post", required=True)
    args = parser.parse_args(argv)
    try:
        client = None if args.command == "show" else PostMediaClient(args.endpoint, args.api_key_env)
        if args.command == "prepare":
            posts = None if args.posts_file is None else decode(read_regular(Path(args.posts_file), MAX_RESPONSE), MAX_RESPONSE)
            result = prepare(client, args.output, posts)
        else:
            plan = Plan(args.plan, args.expected_sha256, None if client is None else client.endpoint)
            result = plan.record(args.post) if args.command == "show" else inspect_plan(client, plan, args.command == "apply")
        print(encode(result, MAX_PART).decode())
        if args.command in ("apply", "status"):
            if result["needs_review"]:
                return 2
            if result["pending"] or not result["processed"]:
                return 3
        return 0
    except (InvalidData, Unavailable, OSError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
