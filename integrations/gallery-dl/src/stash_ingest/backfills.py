"""Typed account-backfill protocol; accepted history is not source-run proof."""

from datetime import datetime
import re
import uuid

from .client import Unavailable
from .completion import ticket_snapshot
from .encoding import InvalidData, decode, digest, encode, identifier
from .source_calls import SourceCalls, definition as source_definition

MODES = ("reddit-new", "reddit-top", "twitter", "reddit-profile-new", "reddit-profile-top-all",
         "reddit-search-new", "reddit-search-top-all", "reddit-search-top-year")
REQUIRED = {"twitter": {"twitter"}, "reddit": {"reddit-new", "reddit-top"}}


def subject(root, platform, account):
    identifier(root)
    if not isinstance(account, str):
        raise InvalidData("Invalid backfill account")
    if platform == "twitter" and re.fullmatch(r"[0-9]{1,30}", account):
        account = account.lstrip("0") or "0"
    elif platform == "reddit" and re.fullmatch(r"[A-Za-z0-9_-]{1,32}", account) and account.lower() != "me":
        account = account.lower()
    else:
        raise InvalidData("Invalid backfill platform or account")
    return {"root_uuid": root, "platform": platform, "account": account}


def targets(platform, account, component):
    # This is version 1 of the existing n8n traversal definitions. Preserve
    # spelling and query order: the native server verifies these exact URLs.
    if component not in MODES or (component == "twitter") != (platform == "twitter"):
        raise InvalidData("Unsupported backfill component")
    if platform == "twitter":
        return ["https://x.com/i/user/" + account]
    profile = "https://www.reddit.com/user/" + account + "/submitted/?sort="
    search = "https://www.reddit.com/search?q=author%3A" + account + "+nsfw%3Ayes&include_over_18=on&sort="
    return {
        "reddit-new": [profile + "new", search + "new&t=all"],
        "reddit-top": [profile + "top&t=all", search + "top&t=all", profile + "top&t=year", search + "top&t=year"],
        "reddit-profile-new": [profile + "new&t=all"],
        "reddit-profile-top-all": [profile + "top&t=all"],
        "reddit-search-new": [search + "new&t=all"],
        "reddit-search-top-all": [search + "top&t=all"],
        "reddit-search-top-year": [search + "top&t=year"],
    }[component]


def definition(value):
    if (not isinstance(value, dict) or set(value) != {"version", "root_uuid", "platform", "account", "component",
                                                    "policy_sha256", "window", "targets"}
            or type(value["version"]) is not int or value["version"] != 1):
        raise InvalidData("Unsupported backfill snapshot")
    subject(value["root_uuid"], value["platform"], value["account"])
    if value["targets"] != targets(value["platform"], value["account"], value["component"]):
        raise InvalidData("Backfill snapshot differs from its component targets")
    spec = source_definition({"root_uuid": value["root_uuid"], "policy_sha256": value["policy_sha256"],
                              "operation": "download", "cooldown_seconds": 0, "window": value["window"]})
    if spec["window"]["since"] is not None:
        raise InvalidData("Account backfill requires a full-history window")
    return {**value, "window": spec["window"]}


def source_snapshot(value):
    return {key: value[key] for key in ("root_uuid", "policy_sha256", "window", "targets")} | {
        "operation": "download", "cooldown_seconds": 0}


def check_protocol(client):
    capabilities = client.capabilities()
    if type(capabilities.get("source_backfill_protocol")) is not int or capabilities["source_backfill_protocol"] != 1:
        raise Unavailable("incompatible_backfill_protocol")


def decision(value, platform):
    if (not isinstance(value, dict) or set(value) != {"uuid", "component", "outcome", "basis", "decided_at"}
            or value["outcome"] not in ("completed", "skipped")):
        raise InvalidData("Invalid backfill decision")
    identifier(value["uuid"])
    allowed = (value["basis"] == "legacy_skip" and value["component"] == "*" if value["outcome"] == "skipped"
               else value["basis"] in ("source_runs", "legacy_completion") and value["component"] in MODES
               and (value["component"] == "twitter") == (platform == "twitter"))
    if not allowed or not isinstance(value["decided_at"], str) or len(value["decided_at"]) > 64:
        raise InvalidData("Invalid backfill decision basis")
    try:
        stamp = datetime.fromisoformat(value["decided_at"])
        if stamp.tzinfo is None:
            raise ValueError()
    except ValueError:
        raise InvalidData("Invalid backfill decision time") from None
    return value


def validate_status(value, spec):
    wanted = subject(spec["root_uuid"], spec["platform"], spec["account"])
    if (not isinstance(value, dict) or any(value.get(key) != item for key, item in wanted.items())
            or value.get("component") != spec["component"] or value.get("state") not in ("needed", "completed", "skipped")
            or type(value.get("account_complete")) is not bool or not isinstance(value.get("decisions"), list)
            or len(value["decisions"]) > 3):
        raise InvalidData("Backfill status does not identify this account and component")
    rows = [decision(item, spec["platform"]) for item in value["decisions"]]
    completed = {item["component"] for item in rows if item["outcome"] == "completed"}
    all_done = REQUIRED[spec["platform"]] <= completed
    expected = ("completed" if spec["component"] in completed or all_done else
                "skipped" if any(item["outcome"] == "skipped" for item in rows) else "needed")
    if len({item["uuid"] for item in rows}) != len(rows) or value["account_complete"] != all_done or value["state"] != expected:
        raise InvalidData("Backfill status contradicts its retained decisions")
    return value


def status(client, spec):
    body = {**subject(spec["root_uuid"], spec["platform"], spec["account"]), "component": spec["component"]}
    return validate_status(client._request("POST", "/backfills/status", encode(body), max_response_bytes=16384), spec)


def completion_proof(box, backfill_uuid, source_uuid, spec):
    """Reconstruct only the immutable submissions assigned to this caller."""
    calls, requests = SourceCalls(box), {}
    summary = calls.summary(source_uuid)
    expected = source_snapshot(spec)
    if summary["definition"] != {key: item for key, item in expected.items() if key != "targets"}:
        raise InvalidData("Backfill source call changed its definition")
    page = calls.page(source_uuid)
    if [item["target_url"] for item in page] != expected["targets"] or len(page) != summary["target_count"]:
        raise InvalidData("Backfill source call changed its targets")
    for position, item in enumerate(page, 1):
        if (item["state"] != "queued" or item["position"] != position
                or item["ticket_uuid"] != str(uuid.uuid5(uuid.UUID(source_uuid), "source/" + str(position)))):
            raise InvalidData("Backfill source call is not fully bound")
        requested, pending, assignments = ticket_snapshot(box, item["ticket_uuid"])
        wanted = {key: value for key, value in summary["definition"].items() if key != "root_uuid"}
        wanted.update(collection_uuid=item["collection_uuid"], collection_revision=item["collection_revision"])
        if "retrieval_url" in requested:
            wanted["retrieval_url"] = item["target_url"]
        if pending or requested != wanted:
            raise InvalidData("Backfill ticket differs from its original binding")
        for assignment in assignments:
            if assignment["state"] != "admitted":
                raise InvalidData("Backfill proof requires admitted original requests")
            request = {**requested, "request_uuid": assignment["request_uuid"], "window": assignment["request_window"]}
            stored = box.db.execute("SELECT sha256 FROM run_requests WHERE request_uuid=?", (assignment["request_uuid"],)).fetchone()
            if stored is None or digest(encode(request, 8192)) != stored[0]:
                raise InvalidData("Backfill request differs from its original digest")
            requests[assignment["request_uuid"]] = request
    if not 1 <= len(requests) <= 512:
        raise InvalidData("Backfill proof exceeds the supported request limit")
    return {"uuid": str(uuid.uuid5(uuid.UUID(backfill_uuid), "completion")),
            **{key: spec[key] for key in ("root_uuid", "platform", "account", "component", "window", "policy_sha256")},
            "requests": [requests[key] for key in sorted(requests)]}


def complete(client, body, spec):
    proof = decode(body, 1 << 20)
    result = decision(client._request("POST", "/backfills/complete", body, max_response_bytes=4096), spec["platform"])
    if (result["uuid"] != proof["uuid"] or result["component"] != spec["component"]
            or result["outcome"] != "completed" or result["basis"] != "source_runs"):
        raise InvalidData("Native backfill receipt does not match its proof")
    return result
