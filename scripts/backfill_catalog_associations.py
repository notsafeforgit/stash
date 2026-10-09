#!/usr/bin/env python3
"""Explicit, resumable repair of imported catalog publishers through Stash.

Uses small admin API batches, ordinary native decisions, and existing accounts
and performers only. No source files, payload copies or secondary database.
"""

import argparse
import collections
import json
import os
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", required=True)
    parser.add_argument("--api-key-file", type=Path, required=True)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--link-owners", action="store_true")
    args = parser.parse_args()
    base = args.server.rstrip("/") + "/api/v3/archive"
    key = args.api_key_file.read_text().strip()
    scope = {"server": base, "link_owners": args.link_owners, "policy": "catalog-association-v1"}
    state = {"scope": scope, "after": "", "processed": 0, "counts": {}, "status": "running"}
    if args.state.exists():
        state = json.loads(args.state.read_text())
        if state["scope"] != scope:
            raise ValueError("Checkpoint belongs to a different server or policy")
        if state["status"] == "complete":
            print(json.dumps(state), flush=True)
            return
    args.state.parent.mkdir(parents=True, exist_ok=True)
    report = args.state.with_suffix(".review.jsonl")

    def request(path, body=None):
        encoded = None if body is None else json.dumps(body).encode()
        for attempt in range(4):
            req = urllib.request.Request(base + path, data=encoded, headers={
                "ApiKey": key, "Content-Type": "application/json",
            })
            try:
                with urllib.request.urlopen(req, timeout=90) as response:
                    return json.load(response)
            except urllib.error.HTTPError as error:
                # Keep the failing response useful without printing credentials.
                raise RuntimeError(f"HTTP {error.code}: {error.read(4096).decode()}") from None
            except (urllib.error.URLError, TimeoutError):
                if attempt == 3:
                    raise
                time.sleep(2 ** attempt)

    def save():
        temporary = args.state.with_suffix(".next")
        with temporary.open("w") as out:
            json.dump(state, out, indent=2)
            out.write("\n")
            out.flush()
            os.fsync(out.fileno())
        os.replace(temporary, args.state)

    counts = collections.Counter(state["counts"])
    last_print = 0
    while True:
        ids = request("/catalog-associations/posts?" + urllib.parse.urlencode({"after": state["after"], "limit": 25}))
        if not ids:
            state["status"] = "complete"
            save()
            print(json.dumps(state), flush=True)
            return
        results = request("/catalog-associations/backfill", {"post_uuids": ids, "link_owners": args.link_owners})
        if len(results) != len(ids):
            raise ValueError("Incomplete batch response; checkpoint not advanced")
        with report.open("a") as out:
            for result in results:
                counts["publisher:" + (result["reason"] or result["action"])] += 1
                if result.get("ownership"):
                    counts["ownership:" + result["ownership"]] += 1
                if result["action"] in ("review", "unavailable") or result.get("ownership") in ("ambiguous_performers", "no_matching_performer", "too_many_identifiers"):
                    out.write(json.dumps(result) + "\n")
            out.flush()
            os.fsync(out.fileno())
        state.update(after=ids[-1], processed=state["processed"] + len(ids), counts=dict(counts))
        save()
        if time.monotonic() - last_print >= 30:
            print(json.dumps({"processed": state["processed"], "counts": state["counts"]}), flush=True)
            last_print = time.monotonic()


if __name__ == "__main__":
    main()
