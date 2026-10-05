"""Host-owned retained media views; never invoke ZFS from the Stash server.

One explicitly inventoried Linux dataset is supported. Child datasets and other
nested mounts fail closed rather than silently dropping media from the view.
The caller holds producer and backup/dedupe barriers during capture. Publication
owns release; neither capture nor export deletes a retained snapshot on failure.
"""

import base64
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import re
import subprocess
import time

from .artwork_pins import directory
from .filesystem_boundary import canonical_uuid, timestamp
from .storage import (HEX, InvalidArchive, decode_json, json_bytes, open_regular,
                      publish_bytes, require_space, sync_directory)

FORMAT = "org.notsafeforgit.stash.zfs-media"
PROPERTY = "org.notsafeforgit.stash:"
DATASET = re.compile(r"[A-Za-z][A-Za-z0-9_.:-]*(?:/[A-Za-z0-9_.:-]+)*\Z")
MAX_RECORD = 8192


def number(value):
    return isinstance(value, str) and value.isascii() and value.isdecimal() and str(int(value)) == value and 0 < int(value) < 1 << 64


def mounts():
    """Read the actual mount namespace, including mounts unknown to ZFS."""
    def unescape(value):
        return re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), value)
    result = []
    with open("/proc/self/mountinfo", encoding="utf-8", errors="surrogateescape") as incoming:
        for line in incoming:
            before, after = line.rstrip("\n").split(" - ", 1)
            fields, kind = before.split(), after.split()
            result.append((Path(unescape(fields[4])), kind[0], unescape(kind[1])))
    return result


class ZFSMedia:
    def __init__(self, cache, dataset, dataset_guid, mountpoint, *, command=("/usr/sbin/zfs",), reserve=50 << 30, lock_fd=None):
        if (not isinstance(dataset, str) or not DATASET.fullmatch(dataset) or len(dataset) > 150
                or any(part in (".", "..") for part in dataset.split("/")) or not number(dataset_guid)):
            raise InvalidArchive("A media snapshot requires an explicit dataset and GUID")
        self.cache = Path(cache).resolve(strict=True)
        self.cache_identity = directory(self.cache, private=True)
        self.mountpoint = Path(mountpoint).absolute()
        if self.mountpoint.resolve(strict=True) != self.mountpoint:
            raise InvalidArchive("Media mountpoint must not contain symlinks")
        self.dataset, self.dataset_guid = dataset, dataset_guid
        self.command, self.reserve = tuple(command), reserve
        self.lock_fd = lock_fd
        if lock_fd is not None:
            from .locked_command import validate_lock
            validate_lock(lock_fd)
        if (not self.command or not all(isinstance(v, str) and v and "\0" not in v for v in self.command)
                or not Path(self.command[0]).is_absolute()):
            raise InvalidArchive("ZFS requires an explicitly configured absolute executable")

    def run(self, *args, deadline=None):
        remaining = 30 if deadline is None else deadline - time.monotonic()
        if remaining <= 0:
            raise InvalidArchive("ZFS capture exceeded the checkpoint deadline")
        try:
            if self.lock_fd is not None:
                from .locked_command import run_locked
                result = run_locked([*self.command, *args], self.lock_fd, min(remaining, 120))
            else:
                result = subprocess.run([*self.command, *args], check=False, capture_output=True,
                                        encoding="utf-8", errors="surrogateescape", timeout=min(remaining, 120),
                                        env={**os.environ, "LC_ALL": "C"}, stdin=subprocess.DEVNULL)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise InvalidArchive(f"ZFS {args[0]} did not complete") from error
        if result.returncode:
            raise InvalidArchive(f"ZFS {args[0]} failed (exit {result.returncode})")
        return result.stdout

    def topology(self, deadline=None):
        output = self.run("list", "-H", "-p", "-r", "-t", "filesystem,volume", "-o",
                          "name,type,mountpoint,mounted,guid", self.dataset, deadline=deadline)
        expected = [self.dataset, "filesystem", str(self.mountpoint), "yes", self.dataset_guid]
        if [line.split("\t") for line in output.splitlines()] != [expected]:
            raise InvalidArchive("Media dataset identity, mountpoint or child dataset inventory changed")
        actual = mounts()
        if [entry for entry in actual if entry[0] == self.mountpoint] != [(self.mountpoint, "zfs", self.dataset)]:
            raise InvalidArchive("Media dataset is not mounted at its declared physical root")
        for path, kind, source in actual:
            if path != self.mountpoint and path.is_relative_to(self.mountpoint):
                # ZFS automatically mounts retained views under this reserved
                # directory. They are not part of the live media namespace.
                relative = path.relative_to(self.mountpoint).parts
                if (len(relative) == 3 and relative[:2] == (".zfs", "snapshot") and kind == "zfs"
                        and source == self.dataset + "@" + relative[2]):
                    continue
                raise InvalidArchive("Media contains an uninventoried nested mount")
        if self.mountpoint.resolve(strict=True) != self.mountpoint:
            raise InvalidArchive("Media mountpoint changed")

    def path(self, checkpoint, token):
        if directory(self.cache, private=True) != self.cache_identity:
            raise InvalidArchive("ZFS snapshot cache was replaced")
        if not canonical_uuid(checkpoint) or not canonical_uuid(token):
            raise InvalidArchive("Invalid media checkpoint identity")
        return self.cache / (checkpoint + "-" + token)

    def properties(self, snapshot, deadline=None):
        keys = ["guid", "createtxg", "defer_destroy", *[PROPERTY + k for k in ("checkpoint", "token", "request")]]
        output = self.run("get", "-H", "-p", "-o", "name,property,value", ",".join(keys), snapshot, deadline=deadline)
        result = {}
        for line in output.splitlines():
            row = line.split("\t")
            if len(row) != 3 or row[0] != snapshot or row[1] not in keys or row[1] in result:
                raise InvalidArchive("Invalid media snapshot properties")
            result[row[1]] = row[2]
        if (set(result) != set(keys) or not all(number(result[k]) for k in ("guid", "createtxg"))
                or result["defer_destroy"] != "off"):
            raise InvalidArchive("Media snapshot properties are missing or scheduled for destruction")
        return result

    def holds(self, snapshot, deadline=None):
        result = set()
        for line in self.run("holds", "-H", "-p", snapshot, deadline=deadline).splitlines():
            row = line.split("\t")
            if len(row) != 3 or row[0] != snapshot or row[1] in result or not number(row[2]):
                raise InvalidArchive("Invalid media snapshot hold inventory")
            result.add(row[1])
        return result

    def capture(self, ready):
        target = self.path(ready["uuid"], ready["token"])
        if not isinstance(ready["request_sha256"], str) or not HEX.fullmatch(ready["request_sha256"]):
            raise InvalidArchive("Invalid media checkpoint request digest")
        deadline = time.monotonic() + (timestamp(ready["expires_at"]) - datetime.now(timezone.utc)).total_seconds()
        self.topology(deadline)
        require_space(self.cache, 0, self.reserve)
        snapshot = self.dataset + "@stash-native-" + ready["uuid"] + "-" + ready["token"]
        record = {"format": FORMAT, "version": 1, **{k: ready[k] for k in ("uuid", "token", "request_sha256")},
                  "dataset": self.dataset, "dataset_guid": self.dataset_guid,
                  "mountpoint": base64.b64encode(os.fsencode(self.mountpoint)).decode("ascii"),
                  "snapshot": snapshot, "hold": "stash-native-" + ready["token"]}
        # Persist intent before external effects. Even uncertain/failed commands
        # retain this identity: retry may inspect it, never recapture newer media.
        target.mkdir(mode=0o700)
        publish_bytes(target / "intent.json", json_bytes(record))
        sync_directory(self.cache)
        options = []
        for key, value in (("checkpoint", ready["uuid"]), ("token", ready["token"]), ("request", ready["request_sha256"])):
            options.extend(("-o", PROPERTY + key + "=" + value))
        self.run("snapshot", *options, snapshot, deadline=deadline)
        self.run("hold", record["hold"], snapshot, deadline=deadline)
        properties = self.properties(snapshot, deadline)
        record.update(snapshot_guid=properties["guid"], createtxg=properties["createtxg"])
        view = MediaView(self, target, record)
        view.verify(deadline=deadline)
        publish_bytes(target / "manifest.json", json_bytes(record))
        if time.monotonic() >= deadline:
            raise InvalidArchive("ZFS capture exceeded the checkpoint deadline")
        return record

    def open_bound(self, boundary, *, _released=False):
        record = boundary.get("details", {}).get("media") if isinstance(boundary, dict) else None
        fields = {"format", "version", "uuid", "token", "request_sha256", "dataset", "dataset_guid",
                  "mountpoint", "snapshot", "hold", "snapshot_guid", "createtxg"}
        if (not isinstance(record, dict) or set(record) != fields or record["format"] != FORMAT
                or type(record["version"]) is not int or record["version"] != 1
                or any(record[k] != boundary.get(k) for k in ("uuid", "token", "request_sha256"))
                or not isinstance(record["request_sha256"], str) or not HEX.fullmatch(record["request_sha256"])
                or not all(number(record[k]) for k in ("snapshot_guid", "dataset_guid", "createtxg"))
                or record["dataset"] != self.dataset or record["dataset_guid"] != self.dataset_guid
                or record["mountpoint"] != base64.b64encode(os.fsencode(self.mountpoint)).decode("ascii")
                or record["snapshot"] != self.dataset + "@stash-native-" + str(record["uuid"]) + "-" + str(record["token"])
                or record["hold"] != "stash-native-" + str(record["token"])):
            raise InvalidArchive("Media view differs from its sealed checkpoint binding")
        path = self.path(record["uuid"], record["token"])
        directory(path, private=True)
        if any((path / name).exists() or (path / name).is_symlink() for name in ("abandoning.json", "abandoned.json")):
            raise InvalidArchive("Media snapshot was abandoned without publication")
        with open_regular(path / "manifest.json") as incoming:
            body = incoming.read(MAX_RECORD + 1)
        if len(body) > MAX_RECORD or decode_json(body) != record:
            raise InvalidArchive("Retained media manifest differs from its sealed checkpoint")
        if not _released and any((path / name).exists() or (path / name).is_symlink() for name in ("release.json", "released.json")):
            raise InvalidArchive("Media snapshot has been released for publication")
        return MediaView(self, path, record)


class MediaView:
    def __init__(self, provider, path, record):
        self.provider, self.path, self.record = provider, path, record
        self.root = provider.mountpoint / ".zfs/snapshot" / record["snapshot"].split("@", 1)[1]

    def verify(self, *, deadline=None, releasing=False):
        p, r = self.provider, self.record
        p.topology(deadline)
        properties = p.properties(r["snapshot"], deadline)
        if (properties["guid"] != r["snapshot_guid"] or properties["createtxg"] != r["createtxg"]
                or any(properties[PROPERTY + k] != r[field] for k, field in
                       (("checkpoint", "uuid"), ("token", "token"), ("request", "request_sha256")))):
            raise InvalidArchive("Media snapshot identity changed")
        if not releasing and r["hold"] not in p.holds(r["snapshot"], deadline):
            raise InvalidArchive("Required media snapshot hold is missing")
        if not releasing:
            directory(self.root)
            if self.root.resolve(strict=True) != self.root or not os.statvfs(self.root).f_flag & os.ST_RDONLY:
                raise InvalidArchive("Retained media view is not a read-only snapshot")
        return self

    def resolve(self, relative):
        relative = Path(relative)
        if relative.is_absolute() or ".." in relative.parts or relative.parts[:1] == (".zfs",):
            raise InvalidArchive("Media path must remain within its snapshot")
        result = (self.root / relative).resolve(strict=True)
        if not result.is_relative_to(self.root) or result.stat().st_dev != self.root.stat().st_dev:
            raise InvalidArchive("Media path escapes its retained snapshot")
        return result


def release_published_media(source, client, provider):
    """Release only after the host verifies durable archive AND media publication.

    No remote verification happens here. Small local and server records remain
    permanently, allowing retry after hold removal or snapshot destruction.
    """
    from .checkpoint_release import archived_boundary, release_published_checkpoint
    boundary = archived_boundary(source, client)
    view = provider.open_bound(boundary, _released=True)
    release = release_published_checkpoint(source, client)
    body = json_bytes({"format": FORMAT + "-release", "version": 1,
                       "media_sha256": hashlib.sha256(json_bytes(view.record)).hexdigest(), "server_release": release})
    intent, complete = view.path / "release.json", view.path / "released.json"

    def matching(path):
        try:
            with open_regular(path) as incoming:
                stored = incoming.read(len(body) + 1)
        except FileNotFoundError:
            return False
        if stored != body:
            raise InvalidArchive("Media snapshot release binding changed")
        return True

    if matching(complete):
        return release
    started = matching(intent)
    # Listing a valid dataset distinguishes a missing snapshot from a failed
    # permission/device query. Never treat arbitrary command failure as absence.
    provider.topology()
    existing = provider.run("list", "-H", "-p", "-d", "1", "-t", "snapshot", "-o", "name", provider.dataset).splitlines()
    if view.record["snapshot"] not in existing:
        if not started:
            raise InvalidArchive("Unreleased media snapshot is missing")
    else:
        view.verify(releasing=started)
        if not started:
            publish_bytes(intent, body)
        if view.record["hold"] in provider.holds(view.record["snapshot"]):
            provider.run("release", view.record["hold"], view.record["snapshot"])
        # Never recursive, forced or deferred: foreign holds/clones must block.
        view.verify(releasing=True)
        provider.run("destroy", view.record["snapshot"])
    publish_bytes(complete, body)
    return release
