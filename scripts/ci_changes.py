#!/usr/bin/env python3
"""Select CI checks relative to a successful publish, and enforce their gate."""

import argparse
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess


FRONTEND_SUFFIXES = {
    ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".json", ".jsonc",
    ".yaml", ".yml", ".css", ".html", ".graphql", ".md", ".svg", ".png",
    ".jpg", ".jpeg", ".webp", ".gif", ".avif", ".ico", ".woff", ".woff2",
}


def frontend_path(path):
    # An allowlist: new backend/tooling paths and unexpected file types run all
    # checks. In particular, Go embed code under ui/ is not frontend-only.
    return (
        path.startswith(("ui/v3/", "ui/login/"))
        and PurePosixPath(path).suffix in FRONTEND_SUFFIXES
    )


def documentation_path(path):
    # Deployment notes often use [skip ci]. They must not force backend tests
    # on the next UI push; executable examples/manifests remain outside this.
    return path.startswith("docs/") and PurePosixPath(path).suffix == ".md"


def git(*args, cwd=None):
    return subprocess.run(
        ["git", *args], cwd=cwd, check=True, stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    ).stdout


def select_checks(event, branch, head, runs, cwd=None):
    full = {"backend": "true", "baseline": "", "reason": "No successful ancestor publish is available."}
    if event not in {"push", "pull_request"}:
        return {**full, "reason": "Manual and other events run the full suite."}
    for run in runs:
        baseline = run.get("head_sha", "")
        if (
            run.get("event") != "push"
            or run.get("conclusion") != "success"
            or run.get("head_branch") != branch
            or not re.fullmatch(r"[0-9a-f]{40}", baseline)
        ):
            continue
        try:
            git("merge-base", "--is-ancestor", baseline, head, cwd=cwd)
            # Disable rename detection: both removed and added paths matter.
            paths = git("diff", "--name-only", "--no-renames", "-z", baseline, head, cwd=cwd)
            paths = [path.decode("utf-8", errors="surrogateescape") for path in paths.split(b"\0") if path]
        except subprocess.CalledProcessError:
            continue
        if paths and all(frontend_path(path) or documentation_path(path) for path in paths):
            return {
                "backend": "false", "baseline": baseline,
                "reason": f"Only frontend/Markdown documentation changed since successful publish {baseline[:12]}.",
            }
        return {
            **full, "baseline": baseline,
            "reason": "Shared, backend, tooling, unknown, or no changed files: run the full suite.",
        }
    return full


def gate_failures(needs):
    failures = []
    for job in ("changes", "assets", "binary"):
        if needs.get(job, {}).get("result") != "success":
            failures.append(f"{job} did not succeed")
    backend = needs.get("changes", {}).get("outputs", {}).get("backend")
    if backend not in {"true", "false"}:
        failures.append("check selection is missing or invalid")
    expected = "skipped" if backend == "false" else "success"
    for job in ("backend-tests", "backend-lint", "python"):
        if needs.get(job, {}).get("result") != expected:
            failures.append(f"{job} must be {expected}")
    return failures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runs", type=Path, help="Recent successful publisher runs from the GitHub API")
    parser.add_argument("--gate", action="store_true", help="Verify the workflow's NEEDS_JSON")
    args = parser.parse_args()
    if args.gate:
        failures = gate_failures(json.loads(os.environ["NEEDS_JSON"]))
        if failures:
            raise SystemExit("\n".join(failures))
        print("All required validation and build jobs succeeded.")
        return
    if not args.runs:
        parser.error("--runs is required when not checking the gate")
    checks = select_checks(
        os.environ["GITHUB_EVENT_NAME"],
        os.environ.get("GITHUB_BASE_REF") or os.environ["GITHUB_REF_NAME"],
        os.environ["GITHUB_SHA"], json.loads(args.runs.read_text()),
    )
    print(json.dumps(checks, indent=2))
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        for key, value in checks.items():
            output.write(f"{key}={value}\n")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
        summary.write(f"{checks['reason']}\n\nUI validation, embedded-asset tests and Linux compilation always run.\n")


if __name__ == "__main__":
    main()
