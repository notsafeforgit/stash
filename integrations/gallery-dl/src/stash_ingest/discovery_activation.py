"""Save and apply a reviewed account listing and explicit held target selection."""

import argparse
import json
import os
from pathlib import Path
import sys
import tempfile

from .activation_client import sha256
from .catalog_snapshot import sync_directory
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .discovery_activation_client import (DiscoveryActivationClient, MAX_HTTP_BYTES, binding_input,
                                          validate_preview)
from .encoding import InvalidData, decode, digest, encode, utc_now
from .endpoint import origin

FORMAT = "stash-discovery-activation-plan-v1"
MAX_PLAN_BYTES = MAX_HTTP_BYTES + (64 << 10)


def prepare(client, output, binding):
    destination = Path(output).absolute()
    if destination.exists() or destination.is_symlink():
        raise InvalidData("Discovery plan already exists; reuse its reviewed digest or choose a new file")
    parent = destination.parent.resolve(strict=True)
    preview = client.preview(binding_input(binding))
    plan = {"format": FORMAT, "endpoint": client.endpoint, "created_at": utc_now(), "preview": preview}
    body = encode(plan, MAX_PLAN_BYTES - 1) + b"\n"
    # Link a flushed private file without replacing any existing reviewed plan.
    # Preparing only calls the read-only preview route; Apply is a separate step.
    fd, temporary = tempfile.mkstemp(prefix=".stash-discovery-plan-", dir=parent)
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
    return {"action": "prepared", "plan_sha256": digest(body), "activation_uuid": preview["input"]["uuid"],
            "listing_uuid": preview["input"]["listing"]["uuid"], "targets": len(preview["entries"]),
            "activated": False, "execution_status": "not_checked"}


class Plan:
    def __init__(self, path, expected_sha256, endpoint=None):
        self.path = Path(path).absolute()
        self.sha256 = sha256(expected_sha256)
        self.endpoint = None if endpoint is None else origin(endpoint)
        self.read()

    def read(self):
        body = read_regular(self.path, MAX_PLAN_BYTES)
        if digest(body) != self.sha256:
            raise InvalidData("Discovery plan differs from its reviewed digest")
        value = decode(body, MAX_PLAN_BYTES)
        if (not isinstance(value, dict) or set(value) != {"format", "endpoint", "created_at", "preview"}
                or value["format"] != FORMAT or origin(value["endpoint"]) != value["endpoint"]
                or (self.endpoint is not None and value["endpoint"] != self.endpoint)):
            raise InvalidData("Discovery plan belongs to another format or Stash endpoint")
        source_time(value["created_at"])
        validate_preview(value["preview"])
        return value


def inspect_plan(client, plan, apply=False):
    saved = plan.read()
    if client.endpoint != saved["endpoint"]:
        raise InvalidData("Discovery plan belongs to another Stash endpoint")
    preview = saved["preview"]
    receipt = client.apply(preview) if apply else client.status(preview)
    return {"plan_sha256": plan.sha256, "activation_uuid": preview["input"]["uuid"],
            "listing_uuid": preview["input"]["listing"]["uuid"], "targets": len(preview["entries"]),
            "activated": receipt is not None, "pending": receipt is None, "execution_status": "not_checked"}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("prepare", "show", "status", "apply"):
        command = commands.add_parser(name)
        if name != "show":
            command.add_argument("--endpoint", required=True)
            command.add_argument("--api-key-env", default="STASH_API_KEY")
        if name == "prepare":
            command.add_argument("--input", required=True, help="JSON with an explicit listing and original target ordinals/hashes")
            command.add_argument("--output", required=True, help="New private JSON plan file; never overwritten")
        else:
            command.add_argument("--plan", required=True)
            command.add_argument("--expected-sha256", required=True, help="Reviewed saved plan file digest")
    args = parser.parse_args(argv)
    try:
        client = None if args.command == "show" else DiscoveryActivationClient(args.endpoint, args.api_key_env)
        if args.command == "prepare":
            binding = decode(read_regular(Path(args.input), MAX_HTTP_BYTES), MAX_HTTP_BYTES)
            result = prepare(client, args.output, binding)
        else:
            plan = Plan(args.plan, args.expected_sha256, None if client is None else client.endpoint)
            result = plan.read() if args.command == "show" else inspect_plan(client, plan, args.command == "apply")
        print(json.dumps(result, sort_keys=True, indent=2 if args.command == "show" else None))
        return 3 if result.get("pending") else 0
    except (InvalidData, Unavailable) as error:
        message, status = str(error), error.status if isinstance(error, Unavailable) else None
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message, status = "Discovery activation plan is unavailable or invalid", None
    print(json.dumps({"error": message, "status": status, "activated": False,
                      "needs_review": status == 409, "resume": "reuse_saved_plan_and_reviewed_digest"}), file=sys.stderr)
    return 2 if status == 409 else 1


if __name__ == "__main__":
    sys.exit(main())
