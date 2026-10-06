"""Save, inspect and resume a reviewed discovery collection binding."""

import argparse
import json
import os
from pathlib import Path
import sys
import tempfile
import uuid

from .activation_client import sha256
from .catalog_snapshot import sync_directory
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, utc_now
from .endpoint import origin
from .discovery_scope_client import DiscoveryScopeClient, MAX_INPUT_BYTES, canonical_input, validate_preview

FORMAT = "stash-discovery-collection-review-v1"
MAX_PLAN_BYTES = (128 << 10) + (8 << 10)


def prepare(client, output, selection):
    destination = Path(output).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Collection review already exists; reuse its digest or choose a new file")
    parent = destination.parent.resolve(strict=True)
    selection = decode(encode(selection, MAX_INPUT_BYTES), MAX_INPUT_BYTES)
    if not isinstance(selection, dict):
        raise InvalidData("Expected a collection review selection")
    selection.setdefault("uuid", str(uuid.uuid4()))
    preview = client.preview(canonical_input(selection))
    value = {"format": FORMAT, "endpoint": client.endpoint, "created_at": utc_now(), "preview": preview}
    body = encode(value, MAX_PLAN_BYTES - 1) + b"\n"
    fd, temporary = tempfile.mkstemp(prefix=".stash-collection-review-", dir=parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, destination)
        sync_directory(parent)
    finally:
        os.unlink(temporary)
        sync_directory(parent)
    return {"action": "prepared", "plan_sha256": digest(body), "review_uuid": preview["input"]["uuid"],
            "listing_uuid": preview["input"]["listing_uuid"], "rebound": False, "execution_status": "not_checked"}


class Plan:
    def __init__(self, path, expected_sha256, endpoint=None):
        self.path = Path(path).absolute()
        self.sha256 = sha256(expected_sha256)
        self.endpoint = None if endpoint is None else origin(endpoint)
        self.read()

    def read(self):
        body = read_regular(self.path, MAX_PLAN_BYTES)
        if digest(body) != self.sha256:
            raise InvalidData("Collection review differs from its saved digest")
        value = decode(body, MAX_PLAN_BYTES)
        if (not isinstance(value, dict) or set(value) != {"format", "endpoint", "created_at", "preview"}
                or value["format"] != FORMAT or origin(value["endpoint"]) != value["endpoint"]
                or self.endpoint is not None and value["endpoint"] != self.endpoint):
            raise InvalidData("Collection review belongs to another format or Stash endpoint")
        source_time(value["created_at"])
        validate_preview(value["preview"])
        return value


def inspect_plan(client, plan, apply=False):
    saved = plan.read()
    if client.endpoint != saved["endpoint"]:
        raise InvalidData("Collection review belongs to another Stash endpoint")
    preview = saved["preview"]
    receipt = client.apply(preview) if apply else client.status(preview)
    return {"plan_sha256": plan.sha256, "review_uuid": preview["input"]["uuid"], "listing_uuid": preview["input"]["listing_uuid"],
            "rebound": receipt is not None, "pending": receipt is None, "execution_status": "not_checked"}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("candidates", "prepare", "show", "status", "apply"):
        command = commands.add_parser(name)
        if name != "show":
            command.add_argument("--endpoint", required=True)
            command.add_argument("--api-key-env", default="STASH_API_KEY")
        if name == "candidates":
            command.add_argument("--collection", required=True)
            command.add_argument("--revision", type=int, required=True)
            command.add_argument("--after")
            command.add_argument("--limit", type=int, default=100)
        elif name == "prepare":
            command.add_argument("--input", required=True, help="Exact listing UUID/definition digest, new collection revision and reason")
            command.add_argument("--output", required=True, help="New private plan file; never overwritten")
        else:
            command.add_argument("--plan", required=True)
            command.add_argument("--expected-sha256", required=True)
    args = parser.parse_args(argv)
    try:
        client = None if args.command == "show" else DiscoveryScopeClient(args.endpoint, args.api_key_env)
        if args.command == "candidates":
            rows = client.candidates(args.collection, args.revision, after=args.after, limit=args.limit)
            result = {"candidates": rows, "next_cursor": None if not rows else rows[-1]["listing"]["uuid"]}
        elif args.command == "prepare":
            result = prepare(client, args.output, decode(read_regular(Path(args.input), MAX_INPUT_BYTES), MAX_INPUT_BYTES))
        else:
            plan = Plan(args.plan, args.expected_sha256, None if client is None else client.endpoint)
            result = plan.read() if args.command == "show" else inspect_plan(client, plan, args.command == "apply")
        print(json.dumps(result, sort_keys=True, indent=2 if args.command in ("show", "candidates") else None))
        return 3 if result.get("pending") else 0
    except (InvalidData, Unavailable) as error:
        message, status = str(error), error.status if isinstance(error, Unavailable) else None
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message, status = "Collection review is unavailable or invalid", None
    print(json.dumps({"error": message, "status": status, "rebound": False, "needs_review": status == 409,
                      "resume": "reuse_saved_plan_and_reviewed_digest"}), file=sys.stderr)
    return 2 if status == 409 else 1


if __name__ == "__main__":
    sys.exit(main())
