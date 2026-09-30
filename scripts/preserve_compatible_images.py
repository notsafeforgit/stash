#!/usr/bin/env python3
"""Preserve verified image manifests without rebuilding or moving existing tags.

Run in each package's owning repository with that repository's packages:write
workflow token. Registry authentication is provided through REGISTRY_AUTH_FILE.
No credentials are accepted as command-line arguments or written to receipts.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess

DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
REPOSITORY = re.compile(r"ghcr\.io/notsafeforgit/stash(?:-s6)?\Z")
TAG = re.compile(r"(?:hwaccel-|alpine-|hwaccel-alpine-)?v2\.5-compatible-(?:final|20260930)\Z")


def run(command):
    return subprocess.run(command, capture_output=True)


def checked(command):
    result = run(command)
    if result.returncode:
        raise RuntimeError(result.stderr.decode().strip() or "Command failed")
    return result.stdout.decode().strip()


def digest(reference, missing_ok=False):
    # Read the exact manifest bytes. Image-specific inspection rejects build
    # attestations, which must also be protected from untagged-version cleanup.
    result = run(["skopeo", "inspect", "--raw", "docker://" + reference])
    if result.returncode:
        # Authentication, transport, and rate-limit failures are not absence.
        if missing_ok and b"manifest unknown" in result.stderr.lower():
            return None
        raise RuntimeError(result.stderr.decode().strip() or "Cannot inspect " + reference)
    json.loads(result.stdout)
    return "sha256:" + hashlib.sha256(result.stdout).hexdigest()


def descendants(repository, root):
    pending = [root]
    seen = set()
    while pending:
        current = pending.pop()
        if current in seen:
            continue
        if not DIGEST.fullmatch(current):
            raise ValueError("Invalid child manifest digest")
        seen.add(current)
        body = json.loads(checked(["skopeo", "inspect", "--raw", "docker://" + repository + "@" + current]))
        pending.extend(item["digest"] for item in body.get("manifests", []))
    return sorted(seen - {root})


def retain(repository, source_digest, tag):
    target = repository + ":" + tag
    existing = digest(target, missing_ok=True)
    if existing is not None and existing != source_digest:
        raise RuntimeError("Refusing to move existing release tag " + target)
    if existing is None:
        checked(["skopeo", "copy", "--all", "--preserve-digests",
                 "docker://" + repository + "@" + source_digest, "docker://" + target])
    if digest(target) != source_digest:
        raise RuntimeError("Published digest differs for " + target)
    return {"image": target, "digest": source_digest}


def preserve(manifest, repository):
    if not REPOSITORY.fullmatch(repository):
        raise ValueError("Unsupported release repository")
    images = [item for item in manifest["images"] if item["repository"] == repository]
    if not images:
        raise ValueError("Repository has no frozen images")
    # Verify every binary before changing any of this repository's release tags.
    for item in images:
        if not DIGEST.fullmatch(item["digest"]) or not item["tags"]:
            raise ValueError("Invalid frozen image")
        if any(not TAG.fullmatch(tag) for tag in item["tags"]):
            raise ValueError("Unsupported release tag")
        if item["entrypoint"] not in ("/app/stash", "/usr/bin/stash"):
            raise ValueError("Unsupported image entrypoint")
        ref = repository + "@" + item["digest"]
        if digest(ref) != item["digest"]:
            raise RuntimeError("Frozen source digest differs")
        version = checked(["docker", "run", "--rm", "--network=none", "--read-only",
                           "--entrypoint", item["entrypoint"], ref, "--version"])
        if manifest["version_revision"] not in version.split():
            # Stash embeds the revision in v0.x-N-gREV rather than a separate word.
            if not any(word.endswith("-" + manifest["version_revision"]) for word in version.split()):
                raise RuntimeError("Unexpected Stash binary in " + ref + ": " + version)

    receipts = []
    for item in images:
        # GHCR represents index children as untagged versions. Protect these too
        # so an untagged-version cleanup cannot break the preserved parent image.
        for child in descendants(repository, item["digest"]):
            receipts.append(retain(repository, child, "v2.5-compatible-content-" + child.split(":")[1]))
        for tag in item["tags"]:
            receipts.append(retain(repository, item["digest"], tag))
    return {"source_commit": manifest["source_commit"], "images": receipts}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    args = parser.parse_args()
    receipt = preserve(json.loads(args.manifest.read_text()), args.repository)
    args.receipt.write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    main()
