"""Prepare and resume native source registration without inferring ownership."""

import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import sys
import uuid

from .activation_client import ActivationClient, integer, sha256
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Client, Unavailable
from .collections import lookup_collections, target_url
from .config_migration import publish_profile
from .encoding import InvalidData, decode, digest, encode, identifier
from .endpoint import origin
from .filesystem import destination_lock
from .publication_lock import publication_lock

FORMAT = "stash-source-management-v1"
MAX_BYTES = 2 << 20
DEFINITION = {"label", "kind", "namespace", "state", "target_url", "account_uuid", "root_uuid", "path_prefix"}
KINDS = {"account", "feed", "subreddit", "search", "manual_batch", "directory", "legacy_catalog", "collection"}
STATES = {"active", "disabled", "retired"}
NAMESPACE = re.compile(r"(?:(?:native|ytdl):[a-z0-9][a-z0-9_.-]*|(?:mirror|legacy):[a-z0-9][a-z0-9_.-]*:[a-z0-9][a-z0-9_.-]*)")


def text(value, limit):
    if (not isinstance(value, str) or len(value.encode("utf-8")) > limit
            or any(ord(c) < 32 or 127 <= ord(c) < 160 for c in value)):
        raise InvalidData("Invalid source definition text")
    return value


def definition(value):
    if not isinstance(value, dict) or set(value) != DEFINITION:
        raise InvalidData("Expected a complete source definition")
    if not text(value["label"], 1024).strip() or value["kind"] not in KINDS or value["state"] not in STATES:
        raise InvalidData("Invalid source label, kind or state")
    namespace = text(value["namespace"], 128)
    if namespace and not NAMESPACE.fullmatch(namespace):
        raise InvalidData("Invalid source namespace")
    if value["target_url"]:
        target_url(value["target_url"])
    elif not isinstance(value["target_url"], str):
        raise InvalidData("Invalid source URL")
    if value["account_uuid"] is not None:
        identifier(value["account_uuid"])
        if not namespace:
            raise InvalidData("A source account requires a namespace")
    path = text(value["path_prefix"], 4096)
    if value["root_uuid"] is None:
        if path:
            raise InvalidData("A source path requires a media root")
    else:
        identifier(value["root_uuid"])
        if (path != "." and (not path or "\\" in path or re.match(r"^[a-z]:", path, re.I)
                or any(part in ("", ".", "..") for part in path.split("/")))):
            raise InvalidData("Source path must remain beneath its media root")
    return value


def collection(value, expected_uuid=None, *, history=False):
    keys = DEFINITION | {"uuid", "revision", "created_at"}
    if history:
        keys |= {"origin", "reason", "recorded_at"}
    if not isinstance(value, dict) or set(value) != keys:
        raise InvalidData("Invalid source collection response")
    definition({key: value[key] for key in DEFINITION})
    identifier(value["uuid"])
    integer(value["revision"], 1, 2147483647)
    source_time(value["created_at"])
    if expected_uuid is not None and value["uuid"] != expected_uuid:
        raise InvalidData("Source response identifies another collection")
    if history:
        source_time(value["recorded_at"])
        text(value["reason"], 4096)
        if value["origin"] not in ("review", "migration", "ingest"):
            raise InvalidData("Invalid source definition origin")
    return value


def mutation(value):
    if not isinstance(value, dict) or set(value) != DEFINITION | {"uuid", "expected_revision", "reason"}:
        raise InvalidData("Invalid source definition mutation")
    definition({key: value[key] for key in DEFINITION})
    identifier(value["uuid"])
    integer(value["expected_revision"], 0, 2147483646)
    text(value["reason"], 4096)
    encode(value, 24576)
    return value


def same_definition(left, right):
    return all(left[key] == right[key] for key in DEFINITION)


def conflict():
    return Unavailable("source_definition_changed", 409)


class SourceManagementClient(ActivationClient):
    kind = "source_management"
    max_http_bytes = MAX_BYTES

    def collection(self, identity):
        value = self.request("GET", "/collections/" + identifier(identity))
        try:
            return collection(value, identity)
        except (InvalidData, TypeError, KeyError):
            raise Unavailable("invalid_source_definition") from None

    def recover(self, value):
        value = mutation(value)
        identity, revision = value["uuid"], value["expected_revision"]
        try:
            rows = self.request("GET", f"/collections/{identity}/history?after={revision}&limit=1")
            if not isinstance(rows, list) or len(rows) > 1:
                raise InvalidData("Invalid source history page")
            if rows:
                found = collection(rows[0], identity, history=True)
                if (found["revision"] != revision + 1 or found["origin"] != "review"
                        or found["reason"] != value["reason"] or not same_definition(value, found)):
                    raise conflict()
                return found
            found = self.collection(identity)
            if found["revision"] != revision:
                raise conflict()
            return found if same_definition(value, found) else None
        except Unavailable as error:
            if error.status == 404 and revision == 0:
                return None
            raise
        except (InvalidData, TypeError, KeyError):
            raise Unavailable("invalid_source_definition_history") from None

    def apply(self, value):
        value = mutation(value)
        previous = self.recover(value)
        if previous is not None:
            return previous
        try:
            result = self.request("PUT", "/collections/" + value["uuid"], value)
        except Unavailable as error:
            if error.status != 409:
                raise
            previous = self.recover(value)
            if previous is None:
                raise
            return previous
        try:
            found = collection(result, value["uuid"])
            if found["revision"] not in (value["expected_revision"], value["expected_revision"] + 1) or not same_definition(value, found):
                raise InvalidData("Source mutation returned another definition")
            return found
        except (InvalidData, TypeError, KeyError):
            raise Unavailable("invalid_source_definition_result") from None


def intent(value):
    value = decode(encode(value, MAX_BYTES), MAX_BYTES)
    if (not isinstance(value, dict) or set(value) != {"operation", "root_uuid", "reason", "targets"}
            or value["operation"] not in ("ensure", "disable") or not isinstance(value["targets"], list)
            or not 1 <= len(value["targets"]) <= 50):
        raise InvalidData("Choose ensure or disable with one to fifty exact source targets")
    identifier(value["root_uuid"])
    text(value["reason"], 4096)
    seen = set()
    for target in value["targets"]:
        definition(target)
        target_url(target["target_url"])
        if target["root_uuid"] != value["root_uuid"] or target["state"] != "active" or target["target_url"] in seen:
            raise InvalidData("Source intent requires distinct active definitions at one root")
        seen.add(target["target_url"])
    return value


def planned_input(spec, target, before, identity=None):
    if before is None:
        if spec["operation"] == "disable":
            return None
        return mutation({**target, "uuid": identity or str(uuid.uuid4()), "expected_revision": 0, "reason": spec["reason"]})
    if spec["operation"] == "ensure":
        if before["state"] != "active":
            raise conflict()  # Registration cannot silently reactivate a removed source.
        return None
    if before["state"] != "active":
        return None
    return mutation({**{key: before[key] for key in DEFINITION}, "state": "disabled",
                     "uuid": before["uuid"], "expected_revision": before["revision"], "reason": spec["reason"]})


def management_lookup(producer, spec):
    roots = producer.capabilities().get("root_uuids")
    if not isinstance(roots, list) or spec["root_uuid"] not in roots:
        # A collection-limited lookup can hide an existing source at this root.
        # Only a complete root grant can establish absence for registration.
        raise Unavailable("source_management_requires_root_grant", 403)
    return lookup_collections(producer, [item["target_url"] for item in spec["targets"]], spec["root_uuid"])


def validate_plan(plan):
    if not isinstance(plan, dict) or set(plan) != {"format", "endpoint", "producer_uuid", "intent", "entries"} or plan["format"] != FORMAT:
        raise InvalidData("Invalid source management plan")
    if origin(plan["endpoint"]) != plan["endpoint"]:
        raise InvalidData("Invalid source management endpoint")
    identifier(plan["producer_uuid"])
    spec = intent(plan["intent"])
    if not isinstance(plan["entries"], list) or len(plan["entries"]) != len(spec["targets"]):
        raise InvalidData("Source plan does not cover its input targets")
    identities = set()
    for target, entry in zip(spec["targets"], plan["entries"], strict=True):
        if not isinstance(entry, dict) or set(entry) != {"before", "input"}:
            raise InvalidData("Invalid source management entry")
        before, value = entry["before"], entry["input"]
        if before is not None:
            collection(before)
            if any(before[key] != target[key] for key in ("target_url", "root_uuid", "namespace")):
                raise InvalidData("Source plan changed its target, root or service")
        if value is not None:
            mutation(value)
        expected = planned_input(spec, target, before, None if value is None else value["uuid"])
        if expected != value:
            raise InvalidData("Source plan changes more than its declared operation")
        identity = before["uuid"] if before is not None else value["uuid"] if value is not None else None
        if identity is not None:
            if identity in identities:
                raise InvalidData("Source plan repeats a collection identity")
            identities.add(identity)
    encode(plan, MAX_BYTES)
    return plan


def prepare(application, producer, value, output):
    spec = intent(value)
    if application.endpoint != producer.endpoint:
        raise InvalidData("Application and producer must identify the same Stash endpoint")
    output = Path(output)
    if output.exists() or output.is_symlink():
        raise InvalidData("Source plan already exists; resume the original plan")
    matches = management_lookup(producer, spec)
    entries = []
    for target, match in zip(spec["targets"], matches["targets"], strict=True):
        if match["state"] == "ambiguous":
            raise conflict()
        before = None
        if match["candidates"]:
            selected = match["candidates"][0]
            before = application.collection(selected["collection_uuid"])
            if before["revision"] != selected["collection_revision"] or before["state"] != selected["state"]:
                raise conflict()
        entries.append({"before": before, "input": planned_input(spec, target, before)})
    plan = validate_plan({"format": FORMAT, "endpoint": application.endpoint, "producer_uuid": producer.producer,
                          "intent": spec, "entries": entries})
    publish_profile(output, plan)
    return digest(read_regular(output, MAX_BYTES))


def load_plan(path, expected_sha256, application, producer):
    sha256(expected_sha256)
    body = read_regular(path, MAX_BYTES)
    if digest(body) != expected_sha256:
        raise InvalidData("Source plan differs from its saved digest")
    plan = validate_plan(decode(body, MAX_BYTES))
    if plan["endpoint"] != application.endpoint or plan["endpoint"] != producer.endpoint or plan["producer_uuid"] != producer.producer:
        raise InvalidData("Source plan belongs to another Stash endpoint or producer")
    return plan


def inspect(application, producer, plan, *, apply=False):
    plan = validate_plan(plan)
    if plan["endpoint"] != application.endpoint or plan["endpoint"] != producer.endpoint or plan["producer_uuid"] != producer.producer:
        raise InvalidData("Source plan belongs to another Stash endpoint or producer")
    spec = plan["intent"]
    matches = management_lookup(producer, spec)
    states, pending = [], []
    for index, (entry, match) in enumerate(zip(plan["entries"], matches["targets"], strict=True)):
        value, before = entry["input"], entry["before"]
        try:
            receipt = None if value is None else application.recover(value)
        except Unavailable as error:
            if error.status != 409:
                raise
            states.append("changed")
            continue
        expected = receipt or before
        bindings = [] if expected is None else [{"collection_uuid": expected["uuid"], "collection_revision": expected["revision"], "state": expected["state"]}]
        if match["has_more"] or match["candidates"] != bindings:
            states.append("changed")
        elif value is not None and receipt is None:
            states.append("pending")
            pending.append(index)
        else:
            states.append("completed" if value is not None else "unchanged")
    # Preflight every target before any write. A later concurrent edit can still
    # stop a partial batch; its saved identities/revisions remain resumable.
    if apply and pending and "changed" not in states:
        for index in pending:
            application.apply(plan["entries"][index]["input"])
        return inspect(application, producer, plan)
    return {"operation": spec["operation"], "targets": len(states), "states": states,
            "needs_review": "changed" in states, "pending": "pending" in states,
            "complete": not any(state in ("changed", "pending") for state in states), "scrape_completion": "not_checked"}


@contextmanager
def management_lock(directory, root):
    # The shared root also covers host/container callers and backup capture.
    with publication_lock(directory), destination_lock(directory, root, "native-source-management", lambda: None):
        yield


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "apply", "status"))
    parser.add_argument("--endpoint", default=os.environ.get("STASH_INGEST_ENDPOINT"), required=False)
    parser.add_argument("--producer", default=os.environ.get("STASH_INGEST_PRODUCER"), required=False)
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--token-env", default="STASH_INGEST_TOKEN")
    parser.add_argument("--plan", required=True, help="New plan for prepare; original saved plan for apply/status")
    parser.add_argument("--input", help="Complete bounded source definitions for prepare")
    parser.add_argument("--expected-sha256", help="Saved plan digest for apply/status")
    parser.add_argument("--locks", help="Shared, inventoried producer lock directory; required for prepare/apply")
    args = parser.parse_args(argv)
    try:
        if not args.endpoint or not args.producer:
            raise InvalidData("Configure the Stash endpoint and producer")
        app = SourceManagementClient(args.endpoint, args.api_key_env)
        producer = Client(args.endpoint, args.producer, token_env=args.token_env)
        if args.command == "prepare":
            if not args.input or not args.locks or args.expected_sha256:
                raise InvalidData("Preparation requires an input and shared locks")
            spec = intent(decode(read_regular(args.input, MAX_BYTES), MAX_BYTES))
            with management_lock(args.locks, spec["root_uuid"]):
                result = {"prepared": True, "plan_sha256": prepare(app, producer, spec, args.plan), "applied": False}
        else:
            if args.input or not args.expected_sha256 or (args.command == "apply" and not args.locks):
                raise InvalidData("Resume the saved plan and digest; applying also requires shared locks")
            plan = load_plan(args.plan, args.expected_sha256, app, producer)
            if args.command == "apply":
                with management_lock(args.locks, plan["intent"]["root_uuid"]):
                    result = inspect(app, producer, plan, apply=True)
            else:
                result = inspect(app, producer, plan)
        print(json.dumps(result, sort_keys=True))
        return 2 if result.get("needs_review") else 3 if result.get("pending") else 0
    except Unavailable as error:
        if error.status == 409:
            print(json.dumps({"error": str(error), "complete": False, "needs_review": True}, sort_keys=True))
            return 2
        message = str(error)
    except InvalidData as error:
        message = str(error)
    except (OSError, TypeError, KeyError, ValueError):
        message = "Source management input, plan or storage is unavailable"
    print(json.dumps({"error": message, "complete": False}, sort_keys=True), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
