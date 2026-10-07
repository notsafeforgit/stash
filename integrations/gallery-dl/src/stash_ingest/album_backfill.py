"""Prepare and resume reviewed source album backfills through the native API."""

import argparse
from collections import Counter
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import uuid

from .album_client import AlbumClient, MAX_POSTS, MAX_PREVIEW_BYTES, POLICIES, integer, sha256, validate_preview, validate_publication
from .catalog_snapshot import sync_directory
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier, utc_now
from .endpoint import origin

FORMAT = "stash-album-backfill-plan-v1"
MAX_MANIFEST_BYTES = 64 << 20


def write_private(path, body):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as output:
        output.write(body)
        output.flush()
        os.fsync(output.fileno())


def validate_record(record, post, policy):
    if not isinstance(record, dict) or set(record) != {"preview", "operation"}:
        raise InvalidData("Invalid album plan record")
    validate_preview(record["preview"], post, policy)
    operation = record["operation"]
    if not isinstance(operation, dict):
        raise InvalidData("Invalid album plan operation")
    identifier(operation.get("request_uuid"))
    expected = {"kind", "request_uuid"}
    if operation.get("kind") == "retry":
        expected |= {"parent_job_uuid", "expected_revision", "resume_publication"}
        identifier(operation.get("parent_job_uuid"))
        integer(operation.get("expected_revision"), 1, (1 << 63) - 1)
        if operation.get("resume_publication") is not None:
            validate_publication(operation["resume_publication"], post)
    elif operation.get("kind") != "apply":
        raise InvalidData("Unsupported album operation")
    if set(operation) != expected:
        raise InvalidData("Unsupported album operation fields")
    return record


def publish_plan(destination, client, policy, scope, records):
    destination = Path(destination).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Album plan output already exists; use the saved plan or a new destination")
    parent = destination.parent.resolve(strict=True)
    stage = Path(tempfile.mkdtemp(prefix=".stash-album-plan-", dir=parent))
    manifest = {"format": FORMAT, "endpoint": client.endpoint, "policy": policy,
                "created_at": utc_now(), "scope": scope, "items": []}
    actions, matches, seen, requests = Counter(), Counter(), set(), set()
    try:
        for post, record in records:
            identifier(post)
            if post in seen or len(seen) >= MAX_POSTS:
                raise InvalidData("Duplicate post or album plan exceeds 100000 posts")
            seen.add(post)
            if record is None:
                manifest["items"].append({"post_uuid": post, "disposition": "forgotten"})
                actions["forgotten"] += 1
                continue
            validate_record(record, post, policy)
            request = record["operation"]["request_uuid"]
            if request in requests:
                raise InvalidData("Album requests must have distinct identities")
            requests.add(request)
            body = encode(record, MAX_PREVIEW_BYTES) + b"\n"
            if len(body) > MAX_PREVIEW_BYTES:
                raise InvalidData("Album plan record exceeds its size limit")
            write_private(stage / (post + ".json"), body)
            preview = record["preview"]
            disposition = "review" if preview["action"] == "review" else "ready"
            manifest["items"].append({"post_uuid": post, "disposition": disposition, "sha256": digest(body)})
            actions[preview["action"]] += 1
            matches.update(match["status"] for match in preview["matches"])
        body = encode(manifest, MAX_MANIFEST_BYTES - 1) + b"\n"
        write_private(stage / "manifest.json", body)
        sync_directory(stage)
        # Reserve the destination without replacing another plan. Publishing
        # the completed directory makes every request durable before any Apply.
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
    return {"action": "prepared", "plan_sha256": digest(body), "posts": len(seen), "actions": dict(actions),
            "matches": dict(matches), "submitted": False}


def prepare(client, output, policy, posts=None):
    if policy not in POLICIES:
        raise InvalidData("Unsupported album matching policy")
    if posts is not None:
        if not isinstance(posts, list) or len(posts) > MAX_POSTS:
            raise InvalidData("Expected a bounded array of post UUIDs")
        checked = [identifier(post) for post in posts]
        if len(set(checked)) != len(checked):
            raise InvalidData("Duplicate post UUID")
        # Resolve explicit historical aliases only when preparing a new plan.
        # Recovery and retry retain their original post and request identities.
        def current_rows():
            for post in sorted({client.current_post(post) for post in checked}):
                yield {"post_uuid": post, "post_state": "active"}
        rows = current_rows()
    else:
        rows = client.selected_posts()

    def records():
        for row in rows:
            post = row["post_uuid"]
            if row["post_state"] == "forgotten":
                yield post, None
            else:
                yield post, {"preview": client.preview(post, policy),
                             "operation": {"kind": "apply", "request_uuid": str(uuid.uuid4())}}
    return publish_plan(output, client, policy, "explicit-posts" if posts is not None else "selected-posts", records())


class Plan:
    def __init__(self, directory, expected_sha256, endpoint=None):
        sha256(expected_sha256)
        self.directory = Path(directory).resolve(strict=True)
        body = read_regular(self.directory / "manifest.json", MAX_MANIFEST_BYTES)
        if digest(body) != expected_sha256:
            raise InvalidData("Album plan manifest differs from the reviewed digest")
        manifest = decode(body, MAX_MANIFEST_BYTES)
        if (not isinstance(manifest, dict) or set(manifest) != {"format", "endpoint", "policy", "created_at", "scope", "items"}
                or manifest["format"] != FORMAT or manifest["policy"] not in POLICIES
                or manifest["scope"] not in ("selected-posts", "explicit-posts", "retry")
                or not isinstance(manifest["items"], list) or len(manifest["items"]) > MAX_POSTS):
            raise InvalidData("Unsupported album plan manifest")
        if origin(manifest["endpoint"]) != manifest["endpoint"] or (endpoint is not None and origin(endpoint) != manifest["endpoint"]):
            raise InvalidData("Album plan belongs to a different Stash endpoint")
        source_time(manifest["created_at"])
        self.manifest, self.sha256, self.items = manifest, expected_sha256, {}
        requests = set()
        # Validate every record before the first network mutation, including
        # records that may appear later than an interrupted submission.
        for item in manifest["items"]:
            if not isinstance(item, dict):
                raise InvalidData("Invalid album plan item")
            post = identifier(item.get("post_uuid"))
            if post in self.items:
                raise InvalidData("Duplicate planned post")
            self.items[post] = item
            disposition = item.get("disposition")
            expected = {"post_uuid", "disposition"}
            if disposition in ("ready", "review"):
                expected.add("sha256")
                sha256(item.get("sha256"))
                record = self.record(post)
                if (record["preview"]["action"] == "review") != (disposition == "review"):
                    raise InvalidData("Album plan obscures a required review")
                request = record["operation"]["request_uuid"]
                if request in requests:
                    raise InvalidData("Duplicate planned request")
                requests.add(request)
                if (manifest["scope"] == "retry") != (record["operation"]["kind"] == "retry"):
                    raise InvalidData("Album plan changed its operation kind")
            elif disposition != "forgotten" or manifest["scope"] != "selected-posts":
                raise InvalidData("Unsupported album plan disposition")
            if set(item) != expected:
                raise InvalidData("Unsupported album plan item fields")
        if manifest["scope"] == "retry" and len(self.items) != 1:
            raise InvalidData("A retry plan must identify exactly one original operation")

    def record(self, post):
        identifier(post)
        item = self.items.get(post)
        if item is None or item["disposition"] == "forgotten":
            raise InvalidData("This plan has no operation for the selected post")
        body = read_regular(self.directory / (post + ".json"), MAX_PREVIEW_BYTES)
        if digest(body) != item["sha256"]:
            raise InvalidData("Album plan record changed after review")
        return validate_record(decode(body, MAX_PREVIEW_BYTES), post, self.manifest["policy"])


def inspect_plan(client, plan, apply=False):
    records, counts = [], Counter()
    for post, item in plan.items.items():
        result = {"post_uuid": post, "disposition": item["disposition"]}
        if item["disposition"] != "ready":
            state = "review_required" if item["disposition"] == "review" else "forgotten"
        else:
            record = plan.record(post)
            try:
                status = client.apply(record) if apply else client.status(record)
                state = status["state"] if status else "not_submitted"
                if status is not None:
                    result["job"] = status
            except Unavailable as error:
                if error.status != 409:
                    raise
                state = "conflict"
        result["state"] = state
        counts[state] += 1
        records.append(result)
    review = any(counts[state] for state in ("review_required", "failed", "cancelled", "conflict"))
    pending = any(counts[state] for state in ("queued", "running", "not_submitted"))
    return {"plan_sha256": plan.sha256, "posts": len(records), "counts": dict(counts),
            "complete": not (review or pending), "needs_review": review, "pending": pending, "records": records}


def prepare_retry(client, plan, post, output):
    original = plan.record(post)
    status = client.status(original)
    if status is None or status["state"] not in ("failed", "cancelled"):
        raise InvalidData("Retry requires a failed or cancelled original job")
    record = {"preview": original["preview"], "operation": {"kind": "retry", "request_uuid": str(uuid.uuid4()),
              "parent_job_uuid": status["job_uuid"], "expected_revision": status["revision"],
              "resume_publication": status.get("publication")}}
    return publish_plan(output, client, plan.manifest["policy"], "retry", [(post, record)])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("prepare", "apply", "status", "prepare-retry", "cancel", "show"):
        command = commands.add_parser(name)
        if name != "show":
            command.add_argument("--endpoint", required=True, help="Explicit native Stash origin; must match the saved plan")
            command.add_argument("--api-key-env", default="STASH_API_KEY")
        if name != "prepare":
            command.add_argument("--plan", required=True)
            command.add_argument("--expected-sha256", required=True, help="Digest of the reviewed plan manifest")
        if name in ("prepare", "prepare-retry"):
            command.add_argument("--output", required=True, help="New private plan directory; never overwritten")
        if name == "prepare":
            command.add_argument("--policy", choices=POLICIES, required=True)
            source = command.add_mutually_exclusive_group(required=True)
            source.add_argument("--all-selected", action="store_true")
            source.add_argument("--post", action="append", help="Native post UUID; may be repeated")
            source.add_argument("--posts-file", help="JSON array of native post UUIDs")
        if name in ("show", "prepare-retry", "cancel"):
            command.add_argument("--post", required=True)
        if name == "cancel":
            command.add_argument("--expected-revision", required=True, type=int)
    args = parser.parse_args(argv)
    try:
        client = None if args.command == "show" else AlbumClient(args.endpoint, args.api_key_env)
        plan = None if args.command == "prepare" else Plan(args.plan, args.expected_sha256, None if client is None else client.endpoint)
        if args.command == "prepare":
            posts = args.post
            if args.posts_file:
                posts = decode(read_regular(args.posts_file, 8 << 20), 8 << 20)
            result = prepare(client, args.output, args.policy, posts)
        elif args.command in ("apply", "status"):
            result = inspect_plan(client, plan, args.command == "apply")
        elif args.command == "prepare-retry":
            result = prepare_retry(client, plan, args.post, args.output)
        elif args.command == "cancel":
            result = client.cancel(plan.record(args.post), args.expected_revision)
        else:
            result = plan.record(args.post)
        print(json.dumps(result, ensure_ascii=False, sort_keys=True, indent=2 if args.command == "show" else None))
        if result.get("needs_review"):
            return 2
        return 3 if result.get("pending") else 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
        status = error.status if isinstance(error, Unavailable) else None
    except (OSError, KeyError, TypeError, ValueError):
        message, status = "Album plan is unavailable or invalid", None
    print(json.dumps({"error": message, "status": status, "complete": False,
                      "resume": "reuse_the_same_saved_plan_and_digest; accepted_work_may_already_exist"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
