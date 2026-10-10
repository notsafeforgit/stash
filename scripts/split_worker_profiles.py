"""Partition site workers and initial/incremental queues without changing policies."""

import argparse
import json
import os
from pathlib import Path
import re
import tempfile
import uuid


def split_profiles(filename, *, lane="all", initial_profiles=()):
    filename = Path(filename).resolve(strict=True)
    document = json.loads(filename.read_text())
    if (document.get("schema") != "stash-gallery-dispatch-v1"
            or not isinstance(document.get("profiles"), list)
            or not 1 <= len(document["profiles"]) <= 32):
        raise ValueError("Invalid worker dispatch list")
    identity = uuid.UUID(document["uuid"])
    if lane not in {"all", "initial", "incremental"}:
        raise ValueError("Invalid worker queue")
    initial_profiles = set(initial_profiles)
    if lane != "all" and not initial_profiles:
        raise ValueError("Select the initial full-history profile IDs explicitly")
    groups, seen = {}, set()
    for entry in document["profiles"]:
        if (set(entry) != {"id", "operation", "profile"} or entry["id"] in seen
                or entry["operation"] not in {"download", "post.enrich", "account.list_page", "post.verify_candidate"}):
            raise ValueError("Invalid worker dispatch entry")
        seen.add(entry["id"])
        profile = json.loads((filename.parent / entry["profile"]).read_text())
        service = profile.get("source_category") or "manual"
        if not isinstance(service, str) or not re.fullmatch(r"[a-z][a-z0-9_-]{0,63}", service):
            raise ValueError("Invalid worker source category")
        initial = entry["id"] in initial_profiles
        if initial and (entry["operation"] != "download"
                        or profile.get("gallery", {}).get("skip") is not True):
            raise ValueError("Initial profiles must download full history with skip=true")
        # Keep metadata dispatch/recovery in both existing worker sets. Only
        # download profiles change queues; admitted metadata must keep draining.
        if (lane == "initial" and entry["operation"] == "download" and not initial) or (lane == "incremental" and initial):
            continue
        groups.setdefault(service, []).append(entry)
    if not initial_profiles <= seen:
        raise ValueError("Unknown initial profile ID")
    return {
        service: {"schema": document["schema"],
                  "uuid": str(uuid.uuid5(identity, "service:" + service + (":" + lane if lane != "all" else ""))),
                  "profiles": entries}
        for service, entries in sorted(groups.items())
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profiles", type=Path)
    parser.add_argument("--lane", choices=("all", "initial", "incremental"), default="all")
    parser.add_argument("--initial-profile", action="append", default=[], help="Full-history download entry ID (repeatable)")
    args = parser.parse_args()
    groups = split_profiles(args.profiles, lane=args.lane, initial_profiles=args.initial_profile)
    # Keep these lists beside the original so relative paths also work through
    # container mounts. Only dispatch identities change; profiles, credentials,
    # producer outboxes and saved source requests are untouched.
    for service, document in groups.items():
        output = args.profiles.resolve().with_name("worker-dispatch-" + service + ".json")
        with tempfile.NamedTemporaryFile(mode="w", dir=output.parent, prefix=".worker-dispatch-", delete=False) as stream:
            temporary = Path(stream.name)
            try:
                json.dump(document, stream, indent=2)
                stream.write("\n")
                stream.flush()
                os.fsync(stream.fileno())
                os.replace(temporary, output)
                directory = os.open(output.parent, os.O_RDONLY | os.O_DIRECTORY)
                try:
                    os.fsync(directory)
                finally:
                    os.close(directory)
            finally:
                temporary.unlink(missing_ok=True)
        print(json.dumps({"service": service, "profiles": len(document["profiles"]), "file": str(output)}))


if __name__ == "__main__":
    main()
