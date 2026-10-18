#!/usr/bin/env python3
"""Run an exhaustive SQLite test shard and retain per-test timings and output."""

import argparse
from collections import Counter, deque
import json
import os
from pathlib import Path
import re
import shlex
import subprocess

from ci_go_packages import select_packages


TEST_NAME = re.compile(r"(?:Test|Example|Fuzz)\w*\Z")


def discover_tests(output):
    # Use the tagged Go test binary's registry, rather than parsing source or
    # maintaining test-name allowlists. Benchmarks are not part of `make it`.
    names = [line for line in output.splitlines() if TEST_NAME.fullmatch(line)]
    if not names or len(set(names)) != len(names):
        raise ValueError("Expected a nonempty registry of uniquely named Go tests")
    return sorted(names)


def shard_tests(names, shard, shards):
    if not 1 <= shard <= shards or shards < 1:
        raise ValueError("Shard must be between 1 and the total number of shards")
    if not names or len(set(names)) != len(names):
        raise ValueError("Cannot shard an empty or duplicate test registry")
    # Interleave names so related families are distributed across runners.
    # Selection changes automatically when Go discovers a new test.
    selected = sorted(names)[shard - 1::shards]
    if not selected:
        raise ValueError("Empty SQLite shard; reduce the shard count")
    return selected


def test_command(packages, flags):
    # Make expands command-line variables before the recipe reaches the shell.
    # Preserve regex end anchors through both expansion layers.
    return ["make", "--no-print-directory", "-s", "it",
            "GO_TEST_PACKAGES=" + " ".join(packages),
            "GO_TEST_FLAGS=" + shlex.join(flags).replace("$", "$$")]


def run_tests(command, path):
    recent = deque(maxlen=100)
    with path.open("w") as output:
        with subprocess.Popen(command, stdout=subprocess.PIPE,
                              stderr=subprocess.STDOUT, text=True) as process:
            for line in process.stdout:
                output.write(line)
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    print(line, end="", flush=True)
                    continue
                if event.get("Output"):
                    recent.append(event["Output"])
                if event.get("Action") == "fail":
                    print("".join(recent), end="", flush=True)
                    recent.clear()
                if not event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
                    print(f"{event['Action']}: {event['Package']} ({event.get('Elapsed', 0):.2f}s)", flush=True)
            return process.wait()


def read_results(path, package):
    completed = Counter()
    timings = {}
    package_passed = False
    with path.open() as source:
        for line in source:
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            if event.get("Package") != package:
                continue
            name, action = event.get("Test"), event.get("Action")
            if not name:
                if action == "pass":
                    package_passed = True
            elif "/" not in name and action in ("pass", "fail", "skip"):
                completed[name] += 1
                timings[name] = {"seconds": event.get("Elapsed", 0), "result": action}
    return completed, timings, package_passed


def check_coverage(selected, completed, timings, package_passed):
    errors = []
    if missing := set(selected) - completed.keys():
        errors.append("Tests did not finish: " + ", ".join(sorted(missing)))
    if unexpected := completed.keys() - set(selected):
        errors.append("Unexpected tests: " + ", ".join(sorted(unexpected)))
    if duplicates := [name for name, count in completed.items() if count != 1]:
        errors.append("Tests ran more than once: " + ", ".join(sorted(duplicates)))
    if any(result["result"] == "fail" for result in timings.values()) or not package_passed:
        errors.append("SQLite package did not pass")
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
    package = module + "/pkg/sqlite"
    listed = subprocess.check_output(test_command([package], ["-list=."]), text=True)
    names = discover_tests(listed)
    selected = shard_tests(names, args.shard, args.shards)
    report = {"package": package, "shard": args.shard, "shards": args.shards,
              "discovered_count": len(names), "selected_tests": selected}
    report_path = args.output / "results.json"
    report_path.write_text(json.dumps(report, indent=2) + "\n")
    print(f"SQLite shard {args.shard}/{args.shards}: {len(selected)} of {len(names)} tests", flush=True)
    # Full-name anchors prevent prefix matches; selecting a parent includes all
    # of its subtests and all fuzz seed cases. Preserve Go's normal test order.
    pattern = "^(" + "|".join(re.escape(name) for name in selected) + ")$"
    path = args.output / "tests.jsonl"
    status = run_tests(test_command([package], ["-json", "-count=1", "-run", pattern]), path)
    completed, timings, passed = read_results(path, package)
    errors = check_coverage(selected, completed, timings, passed)
    # Include all present/future SQLite subpackages exactly once, without
    # applying the root package's name filter to them.
    if args.shard == 1:
        packages = subprocess.check_output(
            ["make", "--no-print-directory", "-s", "list-test-packages"], text=True).split()
        children = [p for p in select_packages(packages, module, "sqlite") if p != package]
        if children:
            child_status = run_tests(test_command(children, ["-json", "-count=1"]),
                                     args.output / "subpackages.jsonl")
            if child_status:
                errors.append("SQLite subpackages failed")
        report["subpackages"] = children
    report.update(timings=timings, errors=errors, exit_code=1 if status or errors else 0)
    report_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    summary = [f"### SQLite shard {args.shard}/{args.shards}", "",
               f"Completed {len(completed)} of {len(selected)} selected tests; {len(names)} discovered in total.", "",
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
