"""Resolve declared worker backup dependencies without running a worker.

Reports contain paths and hashes, never resolved website credentials. Enumerate
under the declared publication barriers before capture, then bind the parsed
profile/private files to the bytes actually retained by ComponentStage.
"""

import argparse
import base64
import copy
import glob
import hashlib
import os
from pathlib import Path
import re
import string
import sys

from stash_ingest.configuration import Configuration, _access_option, _pointer, merge_values
from stash_archive.bundle import NAME
from stash_archive.storage import InvalidArchive, decode_json, json_bytes, open_regular, publish_bytes, regular

FORMAT = "org.notsafeforgit.stash.worker-inventory"
SCHEMAS = {"stash-gallery-worker-v1", "stash-gallery-enrichment-v1",
           "stash-gallery-discovery-v1", "stash-gallery-discovery-detail-v1"}
LIMIT = 4 << 20
MAX_FILES = 16384


def absolute(value):
    if (not isinstance(value, str) or not value.startswith("/") or ".." in Path(value).parts
            or any(ord(c) < 32 for c in value)):
        raise InvalidArchive("Inventory paths must be explicit absolute paths")
    return Path(value)


class Inventory:
    def __init__(self):
        self.files, self.checksums, self.locks, self.archives = {}, {}, set(), {}

    def add(self, path, role):
        path = absolute(str(path))
        regular(path)
        key = str(path)
        previous = self.files.get(key)
        if previous is not None and previous["role"] != role:
            raise InvalidArchive("A worker dependency has conflicting backup roles")
        name = hashlib.sha256(os.fsencode(path)).hexdigest()[:24]
        suffix = re.sub(r"[^A-Za-z0-9._-]", "_", path.name)[:80]
        self.files[key] = {"role": role, "name": name + "-" + suffix, "path": key}
        if len(self.files) > MAX_FILES:
            raise InvalidArchive("Worker backup inventory exceeds its file limit")

    def read(self, path, role="config", expected=None):
        self.add(path, role)
        with open_regular(path) as incoming:
            body = incoming.read(LIMIT + 1)
        if len(body) > LIMIT:
            raise InvalidArchive("Worker dependency exceeds its inspection limit")
        digest = hashlib.sha256(body).hexdigest()
        if expected is not None and digest != expected:
            raise InvalidArchive("Worker helper differs from its reviewed digest")
        if str(path) in self.checksums and self.checksums[str(path)] != digest:
            raise InvalidArchive("Worker dependency changed during inventory")
        self.checksums[str(path)] = digest
        return body

    def document(self, path, role="config"):
        return decode_json(self.read(path, role))

    def worker(self, spec):
        required = {"name", "profile", "home", "working_directory", "outboxes"}
        optional = {"path_mappings", "environment", "lock_roots"}
        if (not isinstance(spec, dict) or not required <= spec.keys() or spec.keys() - required - optional
                or not isinstance(spec["name"], str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", spec["name"])
                or not isinstance(spec["outboxes"], list) or not spec["outboxes"]):
            raise InvalidArchive("Invalid worker inventory declaration")
        home, working = absolute(spec["home"]), absolute(spec["working_directory"])
        mappings = spec.get("path_mappings", [])
        if not isinstance(mappings, list) or len(mappings) > 128:
            raise InvalidArchive("Invalid worker mount mappings")
        translated = {}
        for mapping in mappings:
            if not isinstance(mapping, dict) or set(mapping) != {"from", "to"}:
                raise InvalidArchive("Worker mount mappings require from/to paths")
            source, target = absolute(mapping["from"]), absolute(mapping["to"])
            if source in translated:
                raise InvalidArchive("Duplicate worker mount mapping")
            translated[source] = target

        def path(value, base=working):
            if not isinstance(value, str) or not value or "$" in value:
                raise InvalidArchive("Unresolved worker dependency path")
            if value == "~" or value.startswith("~/"):
                value = str(home) + value[1:]
            candidate = absolute(str(Path(value) if value.startswith("/") else base / value))
            for source in sorted(translated, key=lambda p: len(p.parts), reverse=True):
                if candidate.is_relative_to(source):
                    return translated[source] / candidate.relative_to(source)
            return candidate

        # Binding paths follow Configuration's profile-directory rule; gallery
        # cookie/argument paths follow the worker's explicit cwd/home instead.
        profile_base = absolute(spec["profile"]).parent
        profile = path(spec["profile"])
        document = self.document(profile, "worker_profile")
        if (not isinstance(document, dict) or document.get("schema") not in SCHEMAS
                or not isinstance(document.get("bindings"), dict) or not isinstance(document.get("gallery"), dict)):
            raise InvalidArchive("Unknown worker profile schema")
        if len(document["bindings"]) > 64:
            raise InvalidArchive("Too many worker bindings")
        for outbox in spec["outboxes"]:
            self.add(path(outbox), "producer_outbox")
        roots = spec.get("lock_roots", [])
        if not isinstance(roots, list):
            raise InvalidArchive("Invalid worker publication roots")
        if document["schema"] == "stash-gallery-worker-v1":
            for name in ("root", "locks"):
                value = document.get(name)
                if not isinstance(value, dict) or "path" not in value or "identity" not in value:
                    raise InvalidArchive("Download profile has no directory identity")
                directory = path(value["path"])
                info = directory.stat()
                if not directory.is_dir() or [info.st_dev, info.st_ino] != value["identity"]:
                    raise InvalidArchive("Worker directory identity changed")
                if name == "locks":
                    roots = [*roots, value["path"]]
        for root in roots:
            directory = path(root)
            if not directory.is_dir() or directory.is_symlink():
                raise InvalidArchive("Worker publication root is unavailable")
            self.locks.add(str(directory))

        environment = spec.get("environment", {})
        if not isinstance(environment, dict):
            raise InvalidArchive("Invalid worker environment source declaration")

        def private(source):
            if not isinstance(source, dict) or set(source) != {"file", "pointer"}:
                raise InvalidArchive("Private worker inputs require a file and JSON pointer")
            return _pointer(self.document(path(source["file"], profile_base)), source["pointer"])

        config = Configuration.__new__(Configuration)
        config._values, config._kinds = {}, {}
        if "root" in document:
            config._values["media_root"] = document["root"]["path"]
            config._kinds["media_root"] = "path"
        for name, binding in document["bindings"].items():
            if (not isinstance(name, str) or not re.fullmatch(r"[a-z][a-z0-9_]{0,63}", name)
                    or name == "media_root" or not isinstance(binding, dict)):
                raise InvalidArchive("Invalid worker binding")
            kind = binding.get("kind")
            config._kinds[name] = kind
            if kind in {"path", "asset"} and set(binding) == ({"kind", "path", "sha256"} if kind == "asset" else {"kind", "path"}):
                value = binding["path"]
                path(value, profile_base)  # Validate even a currently unused binding.
                if kind == "asset":
                    if not isinstance(binding["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", binding["sha256"]):
                        raise InvalidArchive("Worker helper requires its reviewed SHA-256")
                    self.read(path(value, profile_base), expected=binding["sha256"])
                # Interpolated path/asset values are already resolved relative
                # to the profile by the actual worker, before gallery sees them.
                if value == "~" or value.startswith("~/"):
                    value = str(home) + value[1:]
                value = str(absolute(value) if value.startswith("/") else profile_base / value)
            elif kind == "private" and set(binding) == {"kind", "file", "pointer"}:
                value = private({k: v for k, v in binding.items() if k != "kind"})
            elif kind == "private" and set(binding) == {"kind", "sources"}:
                sources = binding["sources"]
                if not isinstance(sources, list) or not 1 <= len(sources) <= 16:
                    raise InvalidArchive("Invalid private worker layers")
                value = None
                for source in sources:
                    value = merge_values(value, private(source))
            elif kind == "private" and set(binding) == {"kind", "env"}:
                if not isinstance(binding["env"], str) or binding["env"] not in environment:
                    raise InvalidArchive("Worker environment binding needs an explicit file/pointer source")
                value = private(environment[binding["env"]])
            else:
                raise InvalidArchive("Unsupported worker binding")
            config._values[name] = value
        gallery = config._expand(copy.deepcopy(document["gallery"]))
        config._validate(gallery)

        def cookies(value):
            if isinstance(value, str):
                self.add(path(value), "config")
            elif isinstance(value, list):
                raise InvalidArchive("Browser cookie stores require an explicit portable cookie-file policy")
            elif value and not isinstance(value, dict):
                raise InvalidArchive("Unsupported cookie dependency")

        def archive(value):
            if value is None or value is False or value == "":
                return
            if isinstance(value, list) and value and all(isinstance(p, str) and p for p in value):
                value = os.path.join(*value)
            if not isinstance(value, str):
                raise InvalidArchive("Unsupported download archive path")
            target = path(value)
            parts = list(string.Formatter().parse(str(target)))
            if not any(field is not None for _, field, _, _ in parts):
                self.add(target, "download_archive")
                return
            pattern = ""
            for literal, field, _, _ in parts:
                if field is not None and not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", field):
                    raise InvalidArchive("Archive template requires explicit path review")
                pattern += glob.escape(literal) + ("*" if field is not None else "")
            # Enumerate only the explicit template. Do not recurse a media root
            # to infer archives from a post-dependent base directory.
            if "{" in str(target.parent) or not target.parent.is_dir() or target.parent.is_symlink():
                raise InvalidArchive("Archive template requires a fixed existing directory")
            files = sorted(glob.iglob(pattern))
            if len(files) > MAX_FILES:
                raise InvalidArchive("Too many matching download archives")
            for filename in files:
                self.add(Path(filename), "download_archive")
            self.archives[str(target)] = files

        def visit(node):
            if isinstance(node, dict):
                for key, value in node.items():
                    if key == "archive":
                        archive(value)
                    elif key == "cookies":
                        if node.get("cookies-select") in {"rotate", "random"} and isinstance(value, list):
                            for selected in value:
                                cookies(selected)
                        else:
                            cookies(value)
                    elif key == "cookies-from-browser" and value:
                        raise InvalidArchive("Browser cookies require a portable cookie-file policy")
                    elif key == "netrc" and value:
                        self.add(path(str(home / ".netrc") if value is True else value), "config")
                    elif key == "cmdline-args" and value:
                        for index, argument in enumerate(value):
                            if argument in {"--cookies", "--netrc-location", "--download-archive", "--config-locations"}:
                                if index + 1 == len(value):
                                    raise InvalidArchive("Missing worker path argument")
                                self.add(path(value[index + 1]), "config" if argument != "--download-archive" else "operating_state")
                            elif argument in {"--netrc-cmd", "--cookies-from-browser"}:
                                raise InvalidArchive("Dynamic worker access requires explicit portable-file review")
                    if not _access_option(key):
                        visit(value)
            elif isinstance(node, list):
                for value in node:
                    visit(value)
        visit(gallery)

    def report(self):
        return {"format": FORMAT + ".resolved", "version": 1,
                "components": sorted(self.files.values(), key=lambda c: (c["role"], c["name"])),
                "read_checksums": dict(sorted(self.checksums.items())),
                "worker_lock_roots": sorted(self.locks), "archive_sets": dict(sorted(self.archives.items()))}


def collect(filename):
    inventory = Inventory()
    document = inventory.document(absolute(str(filename)))
    if (not isinstance(document, dict) or set(document) != {"format", "version", "workers"}
            or document["format"] != FORMAT or type(document["version"]) is not int or document["version"] != 1
            or not isinstance(document["workers"], list) or not 1 <= len(document["workers"]) <= 256):
        raise InvalidArchive("Invalid worker inventory file")
    names = set()
    for worker in document["workers"]:
        inventory.worker(worker)
        if worker["name"] in names:
            raise InvalidArchive("Duplicate worker inventory name")
        names.add(worker["name"])
    return validate_report(inventory.report())


def validate_report(report):
    fields = {"format", "version", "components", "read_checksums", "worker_lock_roots", "archive_sets"}
    if (not isinstance(report, dict) or set(report) != fields or report["format"] != FORMAT + ".resolved"
            or type(report["version"]) is not int or report["version"] != 1
            or not isinstance(report["components"], list) or not 1 <= len(report["components"]) <= MAX_FILES
            or not isinstance(report["read_checksums"], dict) or not isinstance(report["archive_sets"], dict)
            or not isinstance(report["worker_lock_roots"], list)):
        raise InvalidArchive("Invalid retained worker inventory")
    paths, names = {}, set()
    for component in report["components"]:
        if (not isinstance(component, dict) or set(component) != {"role", "name", "path"}
                or component["role"] not in {"config", "worker_profile", "producer_outbox", "download_archive", "operating_state"}
                or not isinstance(component["name"], str) or not NAME.fullmatch(component["name"])
                or (component["role"], component["name"]) in names):
            raise InvalidArchive("Invalid retained worker dependency")
        key = str(absolute(component["path"]))
        if key in paths:
            raise InvalidArchive("Duplicate retained worker dependency")
        paths[key] = component["role"]
        names.add((component["role"], component["name"]))
    for path, digest in report["read_checksums"].items():
        if paths.get(path) not in {"config", "worker_profile"} or not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise InvalidArchive("Invalid retained worker dependency digest")
    for root in report["worker_lock_roots"]:
        absolute(root)
    for template, files in report["archive_sets"].items():
        absolute(template)
        if not isinstance(files, list) or any(paths.get(path) != "download_archive" for path in files):
            raise InvalidArchive("Invalid retained download archive inventory")
    return report


def components_for_capture(report, existing, held_roots):
    validate_report(report)
    if not set(report["worker_lock_roots"]) <= {str(absolute(str(root))) for root in held_roots}:
        raise InvalidArchive("Backup does not hold every inventoried worker publication barrier")
    result, selected = [], {}
    for component in [*existing, *report["components"]]:
        path = str(absolute(str(component["path"])))
        if path in selected:
            if selected[path] != component["role"]:
                raise InvalidArchive("Explicit component conflicts with a worker dependency role")
        else:
            result.append(component)
            selected[path] = component["role"]
    return result


def verify_stage(report, stage):
    validate_report(report)
    captured = {os.fsdecode(base64.b64decode(c["source_path"], validate=True)): c for c in stage.record["components"]}
    for component in report["components"]:
        found = captured.get(component["path"])
        if found is None or found["role"] != component["role"]:
            raise InvalidArchive("Captured backup omits a required worker dependency")
    for path, expected in report["read_checksums"].items():
        if captured[path]["sha256"] != expected:
            raise InvalidArchive("Worker dependency changed between inspection and capture")


def main():
    parser = argparse.ArgumentParser(description="Inspect declared worker backup dependencies without running workers")
    parser.add_argument("--inventory", required=True)
    parser.add_argument("--output", required=True, help="New private report file; contains paths/hashes, never credential values")
    args = parser.parse_args()
    try:
        report = collect(args.inventory)
        publish_bytes(Path(args.output), json_bytes(report))
    except (ValueError, OSError, KeyError, TypeError):
        print("Worker dependency inventory failed; inspect the declared profile paths, schemas and bindings", file=sys.stderr)
        return 1
    print(f"Resolved {len(report['components'])} components and {len(report['worker_lock_roots'])} publication roots")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
