"""Apply durable source subscriptions for the reviewed n8n add/remove workflows."""

import argparse
from copy import deepcopy
import json
import os
from pathlib import Path
import re
import sys

from . import backfills, source_accounts, source_lists, source_management, source_policy
from .catalog_upload import read_regular
from .client import Client, Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier
from .endpoint import origin
from .n8n_runner import execution_uuid

MAX_STATE_BYTES = 16 << 20
FORMAT = "stash-n8n-source-request-v1"


class Runtime:
    def __init__(self, path):
        value = decode(read_regular(path, 256 << 10), 256 << 10)
        if (not isinstance(value, dict) or set(value) != {"version", "root_uuid", "locks", "state", "lists", "new_source_policy"}
                or type(value["version"]) is not int or value["version"] != 1
                or not isinstance(value["lists"], dict) or set(value["lists"]) != {"reddit", "twitter"}):
            raise InvalidData("Configure the reviewed native source-management runtime")
        self.root = identifier(value["root_uuid"])
        self.locks, self.state = (self.directory(value[key]) for key in ("locks", "state"))
        self.lists = {platform: self.filename(filename) for platform, filename in value["lists"].items()}
        if len(set(self.lists.values())) != 2:
            raise InvalidData("Each service needs its own saved source list")
        self.policy = source_policy.definition(value["new_source_policy"])
        if self.policy["enabled"] is not True or self.policy["apply_to_scans"] is not False:
            raise InvalidData("New source defaults must be enabled and confined to source intake")

    @staticmethod
    def directory(value):
        path = Runtime.filename(value)
        if not path.is_dir() or path.is_symlink():
            raise InvalidData("Source state and shared locks require existing directories")
        return path

    @staticmethod
    def filename(value):
        if not isinstance(value, str) or not value or any(ord(c) < 32 for c in value):
            raise InvalidData("Invalid source-management path")
        path = Path(value)
        if not path.is_absolute() or str(path.resolve()) != value:
            raise InvalidData("Source-management paths must be canonical absolute paths")
        return path


def request(producer, endpoint, root, action, platform, identity, workflow, execution, node, item):
    if action not in {"add", "remove", "remove-performer"} or platform not in {"reddit", "twitter"}:
        raise InvalidData("Choose a supported source operation and service")
    if action == "remove-performer":
        if not isinstance(identity, str) or not re.fullmatch(r"[1-9][0-9]{0,9}", identity) or int(identity) > 2147483647:
            raise InvalidData("Choose a valid Stash performer ID")
    else:
        backfills.subject(root, platform, identity)
    # A changed input at the same workflow node must conflict, not create a
    # second operation UUID. Account values are deliberately outside this key.
    call = execution_uuid(producer, "source-management", "request", workflow, execution, node, item)
    return call, {"format": FORMAT, "endpoint": origin(endpoint), "producer_uuid": identifier(producer),
                  "root_uuid": identifier(root), "action": action, "platform": platform, "identity": identity,
                  "workflow": workflow, "execution": execution, "node": node, "item": item}


def save(path, value):
    source_lists.publish(path, encode(value, MAX_STATE_BYTES))


def load(path):
    return decode(read_regular(path, MAX_STATE_BYTES), MAX_STATE_BYTES)


def target_urls(platform, references):
    urls = []
    for kind, value in references:
        if platform == "twitter":
            urls.append("https://x.com/i/user/" + value if kind == "id" else "https://x.com/" + value)
        elif kind == "handle":
            backfills.subject("00000000-0000-4000-8000-000000000001", platform, value)
            urls.extend(backfills.targets(platform, value, "reddit-new"))
            urls.extend(backfills.targets(platform, value, "reddit-top"))
        else:
            raise InvalidData("A Reddit subscription requires a handle")
    return list(dict.fromkeys(urls))


def source_target(url, platform, root, account_uuid):
    return {"label": f"{platform}: {url}", "kind": "search" if "/search?" in url else "account",
            "namespace": "native:" + platform, "state": "active", "target_url": url,
            "account_uuid": account_uuid, "root_uuid": root, "path_prefix": "."}


def prepare(app, policies, producer, runtime, value, directory, call):
    platform, action = value["platform"], value["action"]
    list_path = runtime.lists[platform]
    before = read_regular(list_path, source_lists.MAX_LIST_BYTES)
    guards, performer = [], None
    if action == "remove-performer":
        selected = source_accounts.owned_list_accounts(app, platform, value["identity"], before)
        references, guards, performer = selected["references"], selected["guards"], selected["performer"]
    else:
        references = [("id" if platform == "twitter" else "handle", value["identity"])]
    after, matched, removed = source_lists.change(before, platform, references,
                                                  add=value["identity"] if action == "add" else None)
    # Include the exact incoming spelling for backfills and retained list
    # spellings for scheduled runs. Do not silently rebind old URL definitions.
    source_refs = list(dict.fromkeys(references + matched)) if action == "add" else list(dict.fromkeys(matched))
    targets, bindings = [], {}
    for ref in source_refs:
        found = source_accounts.lookup(app, platform, ref)
        account_uuid = None if found is None else found["uuid"]
        for url in target_urls(platform, [ref]):
            if url not in bindings:
                bindings[url] = account_uuid
                targets.append(source_target(url, platform, runtime.root, account_uuid))
            elif bindings[url] != account_uuid:
                raise source_management.conflict()
    if len(targets) > 500:
        raise InvalidData("Choose fewer accounts for one source-management request")
    batches = []
    for index in range(0, len(targets), 50):
        filename = f"sources-{index // 50}.json"
        path = directory / filename
        spec = {"operation": "ensure" if action == "add" else "disable", "root_uuid": runtime.root,
                "reason": "n8n source request " + call, "targets": targets[index:index+50]}
        if path.exists():
            source_plan = source_management.validate_plan(load(path))
            if (source_plan["intent"] != spec or source_plan["endpoint"] != app.endpoint
                    or source_plan["producer_uuid"] != producer.producer):
                raise source_management.conflict()
        else:
            source_management.prepare(app, producer, spec, path)
            source_plan = load(path)
        policy_entries = source_policy.prepare(policies, source_plan, runtime.policy) if action == "add" else []
        batches.append({"file": filename, "sha256": digest(read_regular(path, source_management.MAX_BYTES)), "policies": policy_entries})
    # No remote mutation has happened yet. Keep the exact list bytes for crash
    # recovery; later unrelated list edits must never be overwritten.
    for name, body in (("list-before.txt", before), ("list-after.txt", after)):
        path = directory / name
        if path.exists():
            if read_regular(path, source_lists.MAX_LIST_BYTES) != body:
                raise source_management.conflict()
        else:
            source_lists.publish(path, body)
    result = {"status": "registered" if action == "add" else "removed" if removed else "noop",
              "removedCount": removed, "added": action == "add" and not matched,
              "source_targets": len(targets), "scrape_completion": "not_checked"}
    if performer is not None:
        result["performerName"] = performer["name"]
    if platform == "reddit":
        result["removedUsernames"] = sorted({v for k, v in matched}) if action != "add" else []
    else:
        result["removedIds"] = sorted({v for k, v in matched if k == "id"}) if action != "add" else []
        result["resolvedId"] = value["identity"] if action != "remove-performer" else ""
    result["message"] = "Source registration completed; backfill status is checked separately." if action == "add" else "Updated the selected tracked sources; retained media and account history."
    planned = {"request": value, "batches": batches, "ownership": guards,
               "before_sha256": digest(before), "after_sha256": digest(after), "result": result}
    save(directory / "plan.json", planned)
    return planned


def apply(app, policies, producer, runtime, value, directory, plan):
    if (not isinstance(plan, dict) or set(plan) != {"request", "batches", "ownership", "before_sha256", "after_sha256", "result"}
            or plan["request"] != value or not isinstance(plan["batches"], list) or len(plan["batches"]) > 10
            or not isinstance(plan["ownership"], list)):
        raise InvalidData("Saved n8n source plan does not match this request")
    before = read_regular(directory / "list-before.txt", source_lists.MAX_LIST_BYTES)
    after = read_regular(directory / "list-after.txt", source_lists.MAX_LIST_BYTES)
    if digest(before) != plan["before_sha256"] or digest(after) != plan["after_sha256"]:
        raise InvalidData("Saved source list bytes changed")
    current = read_regular(runtime.lists[value["platform"]], source_lists.MAX_LIST_BYTES)
    if current not in (before, after):
        raise source_management.conflict()
    source_accounts.verify(app, plan["ownership"])
    prepared = []
    for index, batch in enumerate(plan["batches"]):
        if not isinstance(batch, dict) or set(batch) != {"file", "sha256", "policies"} or batch["file"] != f"sources-{index}.json":
            raise InvalidData("Invalid saved source batch")
        source_plan = source_management.load_plan(directory / batch["file"], batch["sha256"], app, producer)
        if source_plan["intent"]["root_uuid"] != runtime.root or source_plan["intent"]["operation"] != ("ensure" if value["action"] == "add" else "disable"):
            raise InvalidData("Saved source batch changed its operation or media root")
        result = source_management.inspect(app, producer, source_plan)
        if result["needs_review"]:
            raise source_management.conflict()
        if value["action"] == "add":
            result = source_policy.inspect(policies, batch["policies"], source_plan)
            if result["needs_review"]:
                raise source_management.conflict()
        elif batch["policies"]:
            raise InvalidData("Source removal cannot change metadata policies")
        prepared.append((source_plan, batch["policies"]))
    for source_plan, entries in prepared:
        if not source_management.inspect(app, producer, source_plan, apply=True)["complete"]:
            raise source_management.conflict()
        if value["action"] == "add" and not source_policy.inspect(policies, entries, source_plan, apply=True)["complete"]:
            raise source_management.conflict()
    source_accounts.verify(app, plan["ownership"])
    source_lists.apply(runtime.lists[value["platform"]], before, after)
    result = deepcopy(plan["result"])
    result["complete"] = True
    save(directory / "receipt.json", {"request": value, "plan_sha256": digest(read_regular(directory / "plan.json", MAX_STATE_BYTES)), "result": result})
    return result


def run(app, policies, producer, runtime, value, call):
    if (app.endpoint != value["endpoint"] or policies.endpoint != app.endpoint or producer.endpoint != app.endpoint
            or producer.producer != value["producer_uuid"] or runtime.root != value["root_uuid"]):
        raise InvalidData("Source-management clients do not match the saved request")
    with source_management.management_lock(runtime.locks, runtime.root):
        directory = runtime.state / identifier(call)
        if directory.is_symlink():
            raise InvalidData("Source request directory must not be a symlink")
        directory.mkdir(mode=0o700, exist_ok=True)
        parent = os.open(runtime.state, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
        original = directory / "request.json"
        if original.exists():
            if load(original) != value:
                raise source_management.conflict()
        else:
            save(original, value)
        receipt_path = directory / "receipt.json"
        plan_path = directory / "plan.json"
        if receipt_path.exists():
            receipt = load(receipt_path)
            if (not isinstance(receipt, dict) or set(receipt) != {"request", "plan_sha256", "result"}
                    or receipt["request"] != value or receipt["plan_sha256"] != digest(read_regular(plan_path, MAX_STATE_BYTES))
                    or receipt["result"].get("complete") is not True):
                raise InvalidData("Invalid completed source-management receipt")
            # Return the original result after later owner edits, without
            # issuing new writes or claiming a current scrape has completed.
            return receipt["result"]
        plan = load(plan_path) if plan_path.exists() else prepare(app, policies, producer, runtime, value, directory, call)
        return apply(app, policies, producer, runtime, value, directory, plan)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--action", choices=("add", "remove", "remove-performer"), required=True)
    parser.add_argument("--platform", choices=("reddit", "twitter"), required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--workflow", required=True)
    parser.add_argument("--execution", required=True)
    parser.add_argument("--node", required=True)
    parser.add_argument("--item", required=True, type=int)
    parser.add_argument("--runtime", default=os.environ.get("STASH_SOURCE_MANAGEMENT_CONFIG"))
    parser.add_argument("--endpoint", default=os.environ.get("STASH_INGEST_ENDPOINT"))
    parser.add_argument("--producer", default=os.environ.get("STASH_INGEST_PRODUCER"))
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--token-env", default="STASH_INGEST_TOKEN")
    args = parser.parse_args(argv)
    try:
        if not all((args.runtime, args.endpoint, args.producer)):
            raise InvalidData("Configure the native source runtime, endpoint and producer")
        runtime = Runtime(args.runtime)
        call, value = request(args.producer, args.endpoint, runtime.root, args.action, args.platform, args.identity,
                              args.workflow, args.execution, args.node, args.item)
        app = source_management.SourceManagementClient(args.endpoint, args.api_key_env)
        policies = source_policy.SourcePolicyClient(args.endpoint, args.api_key_env)
        producer = Client(args.endpoint, args.producer, token_env=args.token_env)
        result = run(app, policies, producer, runtime, value, call)
        print(json.dumps({**result, "request_uuid": call}, sort_keys=True))
        return 0
    except Unavailable as error:
        code, status = str(error), 2 if error.status == 409 else 1
    except InvalidData as error:
        code, status = str(error), 1
    except (OSError, KeyError, TypeError, ValueError):
        code, status = "Native source-management state or configuration is unavailable", 1
    print(json.dumps({"complete": False, "needs_review": status == 2, "error": code}, sort_keys=True))
    return status


if __name__ == "__main__":
    sys.exit(main())
