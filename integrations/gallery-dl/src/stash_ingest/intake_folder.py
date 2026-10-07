"""Resume bounded folder discovery and durable native local-file intake."""

import argparse
from datetime import datetime, timezone
import json
import sqlite3
import sys
import time

from .album_client import integer
from .catalog_upload import read_regular
from .client import Unavailable
from .dedupe import exclusive_lock, private_directory
from .dedupe_host import absolute
from .encoding import InvalidData, decode, identifier
from .endpoint import origin
from .intake_client import IntakeClient, LIMIT, REJECTIONS, directory, includes, page, scan_scope
from .intake_journal import Journal

FORMAT = "stash-folder-intake-v1"


def configuration(value):
    fields = {"format", "endpoint", "root_uuid", "collections", "exclude", "state_dir", "library_lock",
              "api_key_file", "max_pending", "entries_per_run", "settle_seconds", "scan_interval_seconds"}
    if not isinstance(value, dict) or set(value) != fields or value["format"] != FORMAT:
        raise InvalidData("Invalid folder intake configuration")
    if origin(value["endpoint"]) != value["endpoint"]:
        raise InvalidData("Folder intake requires a canonical endpoint")
    identifier(value["root_uuid"])
    for key in ("state_dir", "library_lock", "api_key_file"):
        absolute(value[key])
    collections = value["collections"]
    if not isinstance(collections, list) or not 1 <= len(collections) <= 4096:
        raise InvalidData("Folder intake requires explicit reviewed collections")
    ids, prefixes = set(), set()
    for item in collections:
        if not isinstance(item, dict) or set(item) != {"uuid", "path_prefix"}:
            raise InvalidData("Invalid intake collection")
        identifier(item["uuid"])
        directory(item["path_prefix"])
        if item["uuid"] in ids or item["path_prefix"] in prefixes:
            raise InvalidData("Intake collections must have distinct identities and prefixes")
        ids.add(item["uuid"])
        prefixes.add(item["path_prefix"])
    if not isinstance(value["exclude"], list) or len(value["exclude"]) > 16384:
        raise InvalidData("Invalid intake exclusions")
    for path in value["exclude"]:
        directory(path)
    for key, low, high in (("max_pending", 1, 100), ("entries_per_run", 1, 10000),
                           ("settle_seconds", 0, 86400), ("scan_interval_seconds", 1, 86400)):
        integer(value[key], low, high)
    return value


def deliver(journal, client, request):
    result = client.receipt(request)
    if result is None:
        try:
            result = client.apply(request)
        except Unavailable as error:
            if error.code not in REJECTIONS:
                raise
            # Receipt lookup remains authoritative even for a now-missing or
            # changed file. Never replace an uncertain request with a new UUID.
            result = client.receipt(request)
            if result is None:
                journal.record(request, {"error": error.code, "review": True}, rejection=True)
                return
    journal.record(request, result)


def selected_collection(value, path):
    if any(includes(prefix, path) for prefix in value["exclude"]):
        return None
    matches = [item for item in value["collections"] if includes(item["path_prefix"], path)]
    return max(matches, key=lambda item: len(item["path_prefix"].split("/")) if item["path_prefix"] != "." else 0) if matches else None


def run(value, *, client=None, now=None):
    value = configuration(value)
    now = int(time.time()) if now is None else now
    state, _ = private_directory(value["state_dir"])
    client = client or IntakeClient(value["endpoint"], value["api_key_file"])
    # The backup barrier makes the local admission journal and native server
    # part of one coordinated capture boundary. No media bytes are changed here.
    with exclusive_lock(state / "intake.lock"), exclusive_lock(value["library_lock"]):
        journal = Journal(state, value)
        pending = journal.pending()
        for request in pending:
            deliver(journal, client, request)
        pending_count = len(journal.pending())
        if pending_count >= value["max_pending"] or not journal.begin_cycle(now):
            return {**journal.summary(), "visited": 0, "submitted": 0}
        collections = {item["uuid"]: item for item in value["collections"]}
        submitted, visited = 0, 0
        scopes, exclusions = {}, {}
        while visited < value["entries_per_run"] and pending_count < value["max_pending"]:
            row = journal.next_directory()
            if row is None:
                break
            collection = collections[row["collection"]]
            scope_key = (collection["uuid"], row["path"])
            if scope_key not in scopes:
                scopes[scope_key] = scan_scope(client.scan_scope(collection, value["root_uuid"], row["path"]),
                                              collection, value["root_uuid"], row["path"])
                reason = scopes[scope_key].get("blocked_reason")
                if reason:
                    exclusions[reason] = exclusions.get(reason, 0) + 1
            scope = scopes[scope_key]
            if scope.get("blocked_reason") == "source_folder":
                journal.skip_directory(row)
                visited += 1
                continue
            if row["page"] is None:
                try:
                    listing = client.directory(collection, value["root_uuid"], row["path"], row["after_key"], row["signature"])
                except Unavailable as error:
                    if error.code == "directory_changed" and row["after_key"]:
                        journal.reset_directory(row)
                        visited += 1
                        continue
                    if error.code == "file_not_found":
                        # An unavailable top-level scope may be an unmounted
                        # root. Never report it as a completed empty scan.
                        raise
                    raise
                journal.save_page(row, listing)
                row["position"] = 0
            else:
                listing = page(decode(row["page"], LIMIT), collection, value["root_uuid"], row["path"], row["after_key"], row["signature"])
            if type(row["position"]) is not int or not 0 <= row["position"] < max(1, len(listing["entries"])):
                raise InvalidData("Invalid saved directory position")
            if not listing["entries"]:
                journal.advance(row, listing)
                visited += 1
                continue
            entry = listing["entries"][row["position"]]
            selected = selected_collection(value, entry["relative_path"])
            child = None
            if selected is not None and selected["uuid"] == collection["uuid"]:
                if entry["kind"] == "directory":
                    child = entry["relative_path"]
                elif not scope.get("blocked_reason") and entry["size"] and now - datetime.fromisoformat(entry["modified_at"]).astimezone(timezone.utc).timestamp() >= value["settle_seconds"]:
                    input = {"scan_collection_uuid": collection["uuid"], "collection_uuid": scope["collection_uuid"],
                             "relative_path": entry["relative_path"], "media_kind": entry["kind"]}
                    try:
                        preview = client.preview(input, value["root_uuid"])
                    except Unavailable as error:
                        if error.code not in REJECTIONS:
                            raise
                    else:
                        settled = now - datetime.fromisoformat(preview["modified_at"]).astimezone(timezone.utc).timestamp() >= value["settle_seconds"]
                        if settled and not journal.seen(preview):
                            request = journal.prepare(preview)
                            deliver(journal, client, request)
                            submitted += 1
                            pending_count = len(journal.pending())
            journal.advance(row, listing, child)
            visited += 1
        journal.complete_cycle(now)
        return {**journal.summary(), "visited": visited, "submitted": submitted, "scope_exclusions": exclusions}


def main(argv=None):
    parser = argparse.ArgumentParser(description="Discover local media in reviewed collections and resume native file imports")
    parser.add_argument("--config", required=True)
    args = parser.parse_args(argv)
    try:
        result = run(decode(read_regular(args.config, 4 << 20), 4 << 20))
    except BlockingIOError:
        result = {"skipped": True, "reason": "busy"}
    except (InvalidData, Unavailable, UnicodeError) as error:
        print(str(error), file=sys.stderr)
        return 1
    except (OSError, sqlite3.Error):
        print("Folder intake failed; discovery and pending requests retained", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
