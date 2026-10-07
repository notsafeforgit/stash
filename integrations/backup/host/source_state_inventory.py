"""Capture source-management plans and subscriptions under worker barriers."""

import hashlib
import os
from pathlib import Path
import stat

from stash_archive.storage import InvalidArchive, open_regular
from stash_ingest.encoding import identifier
from stash_ingest.n8n_sources import MAX_STATE_BYTES
from stash_ingest.source_lists import MAX_LIST_BYTES
from stash_ingest.source_policy import definition

MAX_ENTRIES = 16384


def canonical(value):
    if (not isinstance(value, str) or not value.startswith("/") or str(Path(value)) != value
            or ".." in Path(value).parts or any(ord(char) < 32 for char in value)):
        raise InvalidArchive("Source-management paths must be canonical absolute paths")
    return Path(value)


def directory(path):
    path = canonical(str(path))
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or path.resolve(strict=True) != path:
        raise InvalidArchive("Source-management state requires a real directory")
    return info


def signature(info, *, is_directory=False):
    return [info.st_dev, info.st_ino, *([] if is_directory else [info.st_size]),
            info.st_mtime_ns, info.st_ctime_ns]


def snapshot(root):
    root = canonical(str(root))
    directory(root)
    result = {"directories": {}, "files": {}}
    pending = [root]
    while pending:
        path = pending.pop()
        info = directory(path)
        result["directories"][str(path)] = signature(info, is_directory=True)
        if len(result["directories"]) + len(result["files"]) > MAX_ENTRIES:
            raise InvalidArchive("Source-management state exceeds its inventory limit")
        with os.scandir(path) as children:
            for child in children:
                target = canonical(str(Path(child.path)))
                info = child.stat(follow_symlinks=False)
                if stat.S_ISDIR(info.st_mode):
                    pending.append(target)
                elif stat.S_ISREG(info.st_mode):
                    result["files"][str(target)] = signature(info)
                else:
                    raise InvalidArchive("Source-management state cannot contain links or special files")
                if len(pending) + len(result["directories"]) + len(result["files"]) > MAX_ENTRIES:
                    raise InvalidArchive("Source-management state exceeds its inventory limit")
    return result


def fingerprint(inventory, path, role, limit):
    inventory.add(path, role)
    with open_regular(path) as source:
        before = os.fstat(source.fileno())
        if before.st_size > limit:
            raise InvalidArchive("Source-management dependency exceeds its runtime size limit")
        digest = hashlib.sha256()
        size = 0
        while body := source.read(1 << 20):
            size += len(body)
            if size > limit:
                raise InvalidArchive("Source-management dependency exceeds its runtime size limit")
            digest.update(body)
        digest = digest.hexdigest()
        after = os.fstat(source.fileno())
    if signature(before) != signature(after) or signature(after) != signature(path.lstat()):
        raise InvalidArchive("Source-management dependency changed during inventory")
    previous = inventory.checksums.setdefault(str(path), digest)
    if previous != digest:
        raise InvalidArchive("Source-management dependency changed during inventory")


def collect(inventory, filename, resolve, expected_root):
    value = inventory.document(resolve(filename))
    if (not isinstance(value, dict) or set(value) != {"version", "root_uuid", "locks", "state", "lists", "new_source_policy"}
            or type(value["version"]) is not int or value["version"] != 1
            or not isinstance(value["lists"], dict) or set(value["lists"]) != {"reddit", "twitter"}):
        raise InvalidArchive("Invalid source-management runtime in worker inventory")
    if identifier(value["root_uuid"]) != expected_root:
        raise InvalidArchive("Source-management runtime identifies a different worker root")
    policy = definition(value["new_source_policy"])
    if policy["enabled"] is not True or policy["apply_to_scans"] is not False:
        raise InvalidArchive("Source-management defaults must apply to enabled source intake")

    def path(item):
        canonical(item)
        return resolve(item)

    locks, state = path(value["locks"]), path(value["state"])
    directory(locks)
    inventory.locks.add(str(locks))
    lists = [path(filename) for filename in value["lists"].values()]
    if len(set(lists)) != 2:
        raise InvalidArchive("Source services require different subscription lists")
    for filename in lists:
        fingerprint(inventory, filename, "config", MAX_LIST_BYTES)
    record = snapshot(state)
    count = sum(len(tree["directories"]) + len(tree["files"])
                for name, tree in inventory.state_trees.items() if name != str(state))
    if count + len(record["directories"]) + len(record["files"]) > MAX_ENTRIES:
        raise InvalidArchive("Source-management state exceeds its inventory limit")
    for filename in record["files"]:
        fingerprint(inventory, Path(filename), "operating_state", MAX_STATE_BYTES)
    if snapshot(state) != record:
        raise InvalidArchive("Source-management state changed during inventory")
    previous = inventory.state_trees.setdefault(str(state), record)
    if previous != record:
        raise InvalidArchive("Source-management state changed during inventory")


def validate(trees, roles, checksums):
    if not isinstance(trees, dict) or not 1 <= len(trees) <= 256:
        raise InvalidArchive("Invalid retained source-management tree inventory")
    count = 0
    for root_name, record in trees.items():
        root = canonical(root_name)
        if (not isinstance(record, dict) or set(record) != {"directories", "files"}
                or not isinstance(record["directories"], dict) or not isinstance(record["files"], dict)
                or root_name not in record["directories"]
                or len(record["directories"]) + len(record["files"]) > MAX_ENTRIES):
            raise InvalidArchive("Invalid retained source-management tree inventory")
        count += len(record["directories"]) + len(record["files"])
        if count > MAX_ENTRIES:
            raise InvalidArchive("Retained source-management state exceeds its inventory limit")
        if set(record["directories"]) & set(record["files"]):
            raise InvalidArchive("Conflicting source-management state paths")
        for kind, length in (("directories", 4), ("files", 5)):
            for name, identity in record[kind].items():
                path = canonical(name)
                if (not path.is_relative_to(root) or (path != root and str(path.parent) not in record["directories"])
                        or not isinstance(identity, list) or len(identity) != length
                        or any(type(value) is not int for value in identity)
                        or any(value < 0 for value in identity[:-2])):
                    raise InvalidArchive("Invalid retained source-management state identity")
                if kind == "files" and (roles.get(name) != "operating_state" or name not in checksums):
                    raise InvalidArchive("Retained source-management state lacks captured bytes")


def verify_current(trees):
    for root, expected in trees.items():
        if snapshot(root) != expected:
            raise InvalidArchive("Source-management state changed between inspection and capture")
