"""Explicit Linux/ZFS rehearsal using a unique, quota-limited disposable dataset.

Not part of automatic tests. Requires existing sudo permission for ZFS and one
chown of the new mountpoint; changes no delegation, service or live media dataset.
Retains small evidence files in --output. Cleanup never uses recursive destruction.
"""

import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
from unittest.mock import patch
import uuid

from stash_archive.storage import InvalidArchive, json_bytes, publish_bytes
from stash_archive.zfs_media import PROPERTY, ZFSMedia


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--pool", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z][A-Za-z0-9_.-]*", args.pool):
        raise ValueError("Invalid test pool")
    output = args.output.absolute()
    output.mkdir(mode=0o700)
    mountpoint, cache = output / "media", output / "cache"
    mountpoint.mkdir(mode=0o700)
    cache.mkdir(mode=0o700)
    dataset = args.pool + "/stash-native-backup-probe-" + uuid.uuid4().hex
    command = ("/usr/bin/sudo", "-n", "/usr/sbin/zfs")
    def zfs(*values):
        return subprocess.check_output([*command, *values], text=True, timeout=30).strip()
    guid, ready, record = None, None, None
    started = time.monotonic()
    report = {"dataset": dataset, "production_media_unchanged": True}
    try:
        zfs("create", "-o", "mountpoint=" + str(mountpoint), "-o", "quota=64M", dataset)
        guid = zfs("get", "-H", "-p", "-o", "value", "guid", dataset)
        subprocess.run(["/usr/bin/sudo", "-n", "/usr/bin/chown", f"{os.getuid()}:{os.getgid()}", str(mountpoint)],
                       check=True, timeout=30)
        (mountpoint / "original.mp4").write_bytes(b"retained media fixture\n")
        provider = ZFSMedia(cache, dataset, guid, mountpoint, command=command, reserve=50 << 30)
        ready = {"uuid": str(uuid.uuid4()), "token": str(uuid.uuid4()), "request_sha256": "a" * 64,
                 "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
        captured = time.monotonic()
        record = provider.capture(ready)
        report["capture_seconds"] = time.monotonic() - captured
        boundary = {**{k: ready[k] for k in ("uuid", "token", "request_sha256")}, "details": {"media": record}}
        publish_bytes(output / "boundary.json", json_bytes(boundary))
        (mountpoint / "original.mp4").write_bytes(b"later live edit\n")
        reopened = ZFSMedia(cache, dataset, guid, mountpoint, command=command, reserve=50 << 30)
        view = reopened.open_bound(boundary).verify()
        original = view.resolve("original.mp4").read_bytes()
        assert original == b"retained media fixture\n"
        try:
            view.resolve("original.mp4").write_bytes(b"must be read only")
        except OSError as error:
            import errno
            assert error.errno == errno.EROFS
        else:
            raise AssertionError("Snapshot allowed mutation")
        try:
            reopened.capture(ready)
        except FileExistsError:
            pass
        else:
            raise AssertionError("Existing challenge was recaptured")
        # The owned fixture contains this one file. Flush and check the local
        # retained copy before exercising the publication-aware release path.
        publish_bytes(output / "restored-media.mp4", original)
        assert (output / "restored-media.mp4").read_bytes() == b"retained media fixture\n"
        report.update(snapshot_guid=record["snapshot_guid"], dataset_guid=guid,
                      restored_sha256=hashlib.sha256(original).hexdigest(), read_only=True,
                      original_bytes_after_live_edit=True, replay_did_not_recapture=True)
        # Native HTTP/portable-archive bindings have their own real integration
        # fixture. This probe isolates actual ZFS capture, holds and destruction.
        from stash_archive.zfs_media import release_published_media
        with patch("stash_archive.checkpoint_release.archived_boundary", return_value=boundary), \
                patch("stash_archive.checkpoint_release.release_published_checkpoint", return_value={"probe": "verified-local-fixture"}):
            release_published_media(output, None, reopened)
            release_published_media(output, None, reopened)
        assert record["snapshot"] not in zfs("list", "-H", "-p", "-d", "1", "-t", "snapshot", "-o", "name", dataset).splitlines()
        report["released_and_retry_passed"] = True
    finally:
        if guid is not None:
            assert zfs("get", "-H", "-p", "-o", "value", "guid", dataset) == guid
            remaining = zfs("list", "-H", "-p", "-d", "1", "-t", "snapshot", "-o", "name", dataset).splitlines()
            for snapshot in remaining:
                expected = dataset + "@stash-native-" + ready["uuid"] + "-" + ready["token"] if ready else None
                if snapshot != expected:
                    raise InvalidArchive("Unexpected snapshot on owned probe dataset; leaving it for inspection")
                assert zfs("get", "-H", "-o", "value", PROPERTY + "token", snapshot) == ready["token"]
                holds = zfs("holds", "-H", snapshot).splitlines()
                for line in holds:
                    if line.split("\t")[1] == "stash-native-" + ready["token"]:
                        zfs("release", "stash-native-" + ready["token"], snapshot)
                zfs("destroy", snapshot)
            zfs("destroy", dataset)
            report["owned_dataset_removed"] = True
        report["seconds"] = time.monotonic() - started
        publish_bytes(output / "result.json", json_bytes(report))
    print(json.dumps(report))


if __name__ == "__main__":
    main()
