"""Generate independent per-site dispatch lists without changing worker policies."""

import argparse
import json
import os
from pathlib import Path
import re
import tempfile
import uuid


def split_profiles(filename):
    filename = Path(filename).resolve(strict=True)
    document = json.loads(filename.read_text())
    if (document.get("schema") != "stash-gallery-dispatch-v1"
            or not isinstance(document.get("profiles"), list)
            or not 1 <= len(document["profiles"]) <= 32):
        raise ValueError("Invalid worker dispatch list")
    identity = uuid.UUID(document["uuid"])
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
        groups.setdefault(service, []).append(entry)
    return {
        service: {"schema": document["schema"],
                  "uuid": str(uuid.uuid5(identity, "service:" + service)), "profiles": entries}
        for service, entries in sorted(groups.items())
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profiles", type=Path)
    args = parser.parse_args()
    groups = split_profiles(args.profiles)
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
