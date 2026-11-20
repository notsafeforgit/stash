#!/usr/bin/env python3
"""Run an exhaustive shard of the non-SQLite Go tests and retain timings."""

import argparse
from collections import Counter, defaultdict
import json
import os
from pathlib import Path
import re
import subprocess

from ci_go_packages import select_packages
from ci_sqlite_tests import TEST_NAME, run_tests, test_command


def discover_tests(events):
    """Map each package to its top-level tests from `go test -json -list`."""
    registry = defaultdict(list)
    for line in events.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        name = event.get("Output", "").strip()
        if event.get("Action") == "output" and event.get("Package") and TEST_NAME.fullmatch(name):
            registry[event["Package"]].append(name)
    if not registry:
        raise ValueError("Expected a nonempty registry of Go tests")
    for package, names in registry.items():
        if len(set(names)) != len(names):
            raise ValueError(f"Duplicate test names listed for {package}")
    return {package: sorted(names) for package, names in registry.items()}


def shard_names(registry, shard, shards):
    if not 1 <= shard <= shards or shards < 1:
        raise ValueError("Shard must be between 1 and the total number of shards")
    # Shard by name across every package. A name defined in several packages
    # runs in each of them, all in one shard, so the run filter is exact. New
    # tests join a shard automatically; interleaving spreads related families.
    names = sorted({name for names in registry.values() for name in names})
    selected = set(names[shard - 1::shards])
    if not selected:
        raise ValueError("Empty backend shard; reduce the shard count")
    return {package: [name for name in names if name in selected]
            for package, names in registry.items()}


def read_results(path):
    completed = Counter()
    timings = {}
    packages = {}
    with path.open() as source:
        for line in source:
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            package, name, action = event.get("Package"), event.get("Test"), event.get("Action")
            if not package or action not in ("pass", "fail", "skip"):
                continue
            if not name:
                packages[package] = action
            elif "/" not in name:
                completed[package, name] += 1
                timings[f"{package} {name}"] = {"seconds": event.get("Elapsed", 0), "result": action}
    return completed, timings, packages


def check_coverage(expected, completed, timings, packages):
    errors = []
    wanted = Counter((package, name) for package, names in expected.items() for name in names)
    if missing := wanted.keys() - completed.keys():
        errors.append("Tests did not finish: " + ", ".join(sorted(f"{p} {n}" for p, n in missing)))
    if unexpected := completed.keys() - wanted.keys():
        errors.append("Unexpected tests: " + ", ".join(sorted(f"{p} {n}" for p, n in unexpected)))
    if duplicates := [key for key, count in completed.items() if count != 1]:
        errors.append("Tests ran more than once: " + ", ".join(sorted(f"{p} {n}" for p, n in duplicates)))
    if any(result["result"] == "fail" for result in timings.values()):
        errors.append("A selected test failed")
    if failed := sorted(package for package, action in packages.items() if action == "fail"):
        errors.append("Packages failed: " + ", ".join(failed))
    if unfinished := sorted(package for package, names in expected.items() if names and packages.get(package) != "pass"):
        errors.append("Packages did not pass: " + ", ".join(unfinished))
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shard", type=int, required=True, help="One-based shard number")
    parser.add_argument("--shards", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not 1 <= args.shard <= args.shards:
        parser.error("--shard must be between 1 and --shards")
    args.output.mkdir(parents=True, exist_ok=True)
    module = subprocess.check_output(["go", "list", "-m"], text=True).strip()
    listed = subprocess.check_output(["make", "--no-print-directory", "-s", "list-test-packages"], text=True).split()
    packages = select_packages(listed, module, "other")
    listing = subprocess.run(test_command(packages, ["-json", "-list=."]), capture_output=True, text=True)
    if listing.returncode:
        print(listing.stdout[-20000:], listing.stderr[-20000:], sep="\n", flush=True)
        raise SystemExit("Listing backend tests failed")
    registry = discover_tests(listing.stdout)
    expected = shard_names(registry, args.shard, args.shards)
    selected = sorted({name for names in expected.values() for name in names})
    total = sum(len(names) for names in registry.values())
    report = {"shard": args.shard, "shards": args.shards, "packages": packages,
              "discovered_count": total, "selected_tests": expected}
    report_path = args.output / "results.json"
    report_path.write_text(json.dumps(report, indent=2) + "\n")
    count = sum(len(names) for names in expected.values())
    print(f"Backend shard {args.shard}/{args.shards}: {count} of {total} tests in {len(packages)} packages", flush=True)
    # Every package runs, so TestMain failures and packages whose tests all
    # belong to other shards still report. Full-name anchors prevent prefix
    # matches and select each parent's subtests and fuzz seed cases.
    pattern = "^(" + "|".join(re.escape(name) for name in selected) + ")$"
    path = args.output / "tests.jsonl"
    status = run_tests(test_command(packages, ["-json", "-run", pattern]), path)
    completed, timings, results = read_results(path)
    errors = check_coverage(expected, completed, timings, results)
    report.update(timings=timings, package_results=results, errors=errors, exit_code=1 if status or errors else 0)
    report_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    summary = [f"### Backend shard {args.shard}/{args.shards}", "",
               f"Completed {sum(completed.values())} of {count} selected tests; {total} discovered in total.", "",
               "| Slowest top-level test | Seconds | Result |", "| --- | ---: | --- |"]
    for name, result in sorted(timings.items(), key=lambda item: (-item[1]["seconds"], item[0]))[:20]:
        summary.append(f"| `{name}` | {result['seconds']:.2f} | {result['result']} |")
    summary.extend(["", *errors, ""])
    text = "\n".join(summary)
    (args.output / "summary.md").write_text(text)
    if summary_path := os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(summary_path, "a") as output:
            output.write(text)
    print(text, flush=True)
    if status or errors:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
