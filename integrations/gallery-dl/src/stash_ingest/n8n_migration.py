"""Stage reviewed n8n backfill graphs without publishing or activating them."""

import argparse
from copy import deepcopy
import json
from pathlib import Path
import re
import sys
import uuid

from .backfills import MODES
from .config_migration import publish_profile as publish_private_json
from .encoding import InvalidData, decode, digest, identifier

OLD_RUNNER = "=python3 /home/andrew/src/private/scrape-catalog/n8n_runner.py"
RUNNER = "=/opt/stash-ingest/bin/stash-ingest-n8n"
OLD_PARENT_CHECK = "=python3 /home/andrew/src/private/scrape-catalog/n8n_parent_alive.py"
PARENT_CHECK = "=/opt/stash-ingest/bin/stash-n8n-parent-alive"
PARENT_ARGUMENT = " --execution '{{ /^[0-9]+:/.test(String($json.lock_token || \"\")) ? String($json.lock_token).split(\":\")[0] : \"INVALID\" }}'"


def identity_expression(mode):
    pattern, field = ("[0-9]{1,30}", "userId") if mode == "twitter" else ("[A-Za-z0-9_-]{1,32}", "username")
    return "{{ /^" + pattern + "$/.test(String($json." + field + " ?? '')) ? String($json." + field + ") : 'INVALID!' }}"


def old_inspection():
    return OLD_RUNNER + " --inspect='{{ /^[0-9a-f]{32}$/.test(String(JSON.parse($json.stdout).token ?? '')) ? JSON.parse($json.stdout).token : 'INVALID!' }}'"


def only_target(connections, name):
    value = connections.get(name)
    if (not isinstance(value, dict) or set(value) != {"main"} or len(value["main"]) != 1 or len(value["main"][0]) != 1
            or value["main"][0][0].get("type") != "main" or value["main"][0][0].get("index") != 0):
        raise InvalidData("Unsupported n8n backfill connection shape")
    return value["main"][0][0]["node"]


def edge(name):
    return {"node": name, "type": "main", "index": 0}


def convert(workflow):
    """Preserve workflow/node IDs, credentials, inputs and existing result nodes."""
    if not isinstance(workflow, dict) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", workflow.get("id", "")):
        raise InvalidData("A reviewed workflow requires its existing ID")
    value = deepcopy(workflow)
    nodes, connections = value.get("nodes"), value.get("connections")
    if not isinstance(nodes, list) or not isinstance(connections, dict):
        raise InvalidData("A reviewed workflow requires nodes and connections")
    by_name = {node["name"]: node for node in nodes}
    if len(by_name) != len(nodes) or len({node["id"] for node in nodes}) != len(nodes):
        raise InvalidData("Workflow node names and IDs must be unique")
    parent_checks = [node for node in nodes if node.get("type") == "n8n-nodes-base.executeCommand"
                     and OLD_PARENT_CHECK in node.get("parameters", {}).get("command", "")]
    if parent_checks:
        if len(parent_checks) != 1 or parent_checks[0]["parameters"]["command"] != OLD_PARENT_CHECK + PARENT_ARGUMENT:
            raise InvalidData("Workflow differs from the reviewed parent-execution check")
        if any(OLD_RUNNER in node.get("parameters", {}).get("command", "") for node in nodes):
            raise InvalidData("A parent heartbeat cannot also be a scraper workflow")
        parent_checks[0]["parameters"]["command"] = PARENT_CHECK + PARENT_ARGUMENT
        return value
    commands = [node for node in nodes if node.get("type") == "n8n-nodes-base.executeCommand"
                and OLD_RUNNER in node.get("parameters", {}).get("command", "")]
    run = [(node, mode) for node in commands for mode in MODES
           if node["parameters"]["command"] == OLD_RUNNER + " --mode " + mode + " --identity='" + identity_expression(mode) + "'"]
    inspections = [node for node in commands if node["parameters"]["command"] == old_inspection()]
    if len(commands) != 2 or len(run) != 1 or len(inspections) != 1:
        raise InvalidData("Workflow differs from the reviewed backfill runner contract")
    (start, mode), inspect = run[0], inspections[0]
    if start["parameters"].get("executeOnce") is not False or inspect["parameters"].get("executeOnce") is not False:
        raise InvalidData("Backfill commands must preserve each input item")
    if only_target(connections, start["name"]) != inspect["name"]:
        raise InvalidData("Backfill launcher does not lead to its inspection")
    parse = by_name[only_target(connections, inspect["name"])]
    error_node = by_name[only_target(connections, parse["name"])]
    if parse["type"] != "n8n-nodes-base.set" or error_node["type"] != "n8n-nodes-base.if":
        raise InvalidData("Backfill inspection requires the reviewed parsing/error branches")
    assignments = parse["parameters"].get("assignments", {}).get("assignments", [])
    if not isinstance(assignments, list) or {item["name"] for item in assignments} not in (
            {"command_failed", "exit_code", "stderr_tail", "stdout_tail"},
            {"network_blocked", "command_failed", "exit_code", "stderr_tail", "stdout_tail"}):
        raise InvalidData("Backfill result assignments differ from the reviewed contract")
    for item in assignments:
        if item["value"] != "={{ JSON.parse($json.stdout)." + item["name"] + " }}":
            raise InvalidData("Backfill result parsing has changed")
    namespace = uuid.UUID(identifier(start["id"]))
    pending_name, wait_name = "If native backfill pending", "Wait for native backfill"
    pending_id, wait_id = (str(uuid.uuid5(namespace, label)) for label in ("native-pending", "native-wait"))
    if any(node["name"] in (pending_name, wait_name) or node["id"] in (pending_id, wait_id) for node in nodes):
        raise InvalidData("Native backfill node name or identity already exists")
    execution = "{{ /^[0-9]{1,30}$/.test(String($execution.id ?? '')) ? String($execution.id) : 'INVALID!' }}"
    item_index = "{{ /^[0-9]{1,10}$/.test(String($itemIndex)) ? String($itemIndex) : 'INVALID!' }}"
    start["parameters"]["command"] = (RUNNER + " --mode " + mode + " --identity='" + identity_expression(mode)
        + "' --workflow='" + workflow["id"] + "' --execution='" + execution + "' --node='" + start["id"]
        + "' --item='" + item_index + "'")
    token = "$json.token ?? JSON.parse($json.stdout ?? '{}').token"
    inspect["parameters"]["command"] = (RUNNER + " --inspect='{{ /^[0-9a-f]{32}$/.test(String(" + token
        + " ?? '')) ? String(" + token + ") : 'INVALID!' }}'")
    for field, kind in (("backfill_pending", "boolean"), ("token", "string")):
        assignments.append({"id": str(uuid.uuid5(namespace, "result/" + field)), "name": field, "type": kind,
                            "value": "={{ JSON.parse($json.stdout)." + field + " }}"})
    position = parse.get("position", [0, 0])
    nodes.extend([
        {"id": pending_id, "name": pending_name, "type": "n8n-nodes-base.if", "typeVersion": 2.3,
         "position": [position[0], position[1] + 220], "parameters": {
             "conditions": {"combinator": "and", "conditions": [{"id": str(uuid.uuid5(namespace, "pending-condition")),
                 "leftValue": "={{ $json.backfill_pending }}", "rightValue": True,
                 "operator": {"type": "boolean", "operation": "true", "singleValue": True}}],
                 "options": {"caseSensitive": True, "leftValue": "", "typeValidation": "strict", "version": 3}}, "options": {}}},
        {"id": wait_id, "name": wait_name, "type": "n8n-nodes-base.wait", "typeVersion": 1.1,
         "position": [position[0] - 220, position[1] + 220], "webhookId": str(uuid.uuid5(namespace, "wait-webhook")),
         "parameters": {"resume": "timeInterval", "amount": 90, "unit": "seconds"}},
    ])
    connections[parse["name"]] = {"main": [[edge(pending_name)]]}
    connections[pending_name] = {"main": [[edge(wait_name)], [edge(error_node["name"])]]}
    connections[wait_name] = {"main": [[edge(inspect["name"])]]}
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="Reviewed workflow export JSON array")
    parser.add_argument("--output", required=True, help="New private staging file; never overwrites an existing export")
    args = parser.parse_args(argv)
    try:
        with Path(args.input).open("rb") as file:
            raw = file.read((1 << 20) + 1)
        value = decode(raw, 1 << 20)
        if not isinstance(value, list) or not 1 <= len(value) <= 100:
            raise InvalidData("Expected a bounded list of reviewed workflow exports")
        converted = [convert(workflow) for workflow in value]
        publish_private_json(args.output, converted)
        print(json.dumps({"state": "staged", "workflows": len(converted), "input_sha256": digest(raw)}, sort_keys=True))
        return 0
    except (InvalidData, KeyError, TypeError, ValueError):
        print("Workflow input differs from the reviewed native conversion contract", file=sys.stderr)
    except OSError:
        print("Workflow staging input is unavailable or output already exists", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
