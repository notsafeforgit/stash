from datetime import datetime, timedelta, timezone
import fcntl
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_archive.checkpoint_abandon import FORMAT, abandon_artwork, abandon_components, abandon_host_capture, abandon_media
from stash_archive.component_stage import ComponentStage
from stash_archive.host_boundary import HostFilesystemCapture
from stash_archive.locked_command import run_locked
from stash_archive.storage import InvalidArchive, json_bytes, publish_bytes
import test_artwork_pins
import test_zfs_media


def receipt(ready):
    return {"format": FORMAT, "version": 1, "uuid": ready["uuid"], "request_sha256": ready["request_sha256"],
            "abandoned_at": "2026-10-05T00:00:00Z"}


class ArtworkAbandonTests(unittest.TestCase):
    setUp = test_artwork_pins.ArtworkPinTests.setUp

    def test_partial_links_are_retired_and_originals_unknown_files_survive(self):
        link = os.link
        linked = []
        def partial(source, target, **kwargs):
            if kwargs.get("src_dir_fd") is not None:
                if linked:
                    raise OSError("interrupted pin capture")
                linked.append(target)
            return link(source, target, **kwargs)
        with patch("stash_archive.artwork_pins.os.link", side_effect=partial):
            with self.assertRaises(OSError):
                self.pins.capture(self.ready)
        path = self.pins.path(self.ready["uuid"], self.ready["token"])
        (path / "0/keep").write_bytes(b"unknown file")
        abandon_artwork(self.pins, self.ready, receipt(self.ready))
        abandon_artwork(self.pins, self.ready, receipt(self.ready))
        self.assertEqual([p.name for p in (path / "0").iterdir()], ["keep"])
        self.assertTrue((path / "intent.json").is_file())
        self.assertTrue((path / "abandoned.json").is_file())
        for original, body in self.originals.values():
            self.assertEqual(original.read_bytes(), body)

    def test_redirected_or_mounted_pin_is_preserved_after_fencing(self):
        self.pins.capture(self.ready)
        path = self.pins.path(self.ready["uuid"], self.ready["token"])
        with patch("stash_archive.zfs_media.mounts", return_value=[(path / "0", "bind", "foreign")]):
            with self.assertRaisesRegex(InvalidArchive, "mount"):
                abandon_artwork(self.pins, self.ready, receipt(self.ready))
        self.assertFalse((path / "abandoned.json").exists())
        checksum = next(iter(self.originals))
        retained = path / "0" / checksum
        retained.unlink()
        retained.symlink_to(self.originals[checksum][0])
        with self.assertRaisesRegex(InvalidArchive, "regular file"):
            abandon_artwork(self.pins, self.ready, receipt(self.ready))
        self.assertTrue(retained.is_symlink())
        retained.unlink()
        abandon_artwork(self.pins, self.ready, receipt(self.ready))
        for original, body in self.originals.values():
            self.assertEqual(original.read_bytes(), body)


class MediaAbandonTests(unittest.TestCase):
    setUp = test_zfs_media.ZFSMediaTests.setUp

    def test_snapshot_created_before_failed_hold_can_be_retired(self):
        def fail_hold(*args, **kwargs):
            if args[0] == "hold":
                raise InvalidArchive("interrupted hold")
            return self.fake.run(*args, **kwargs)
        self.provider.run = fail_hold
        with self.assertRaisesRegex(InvalidArchive, "interrupted hold"):
            self.provider.capture(self.ready)
        self.assertEqual(len(self.fake.snapshots), 1)
        self.provider.run = self.fake.run
        abandon_media(self.provider, self.ready, receipt(self.ready))
        abandon_media(self.provider, self.ready, receipt(self.ready))
        self.assertFalse(self.fake.snapshots)
        self.assertEqual((self.media / "media/video.mp4").read_bytes(), b"original media")
        self.assertEqual(len([call for call in self.fake.calls if call[0] == "destroy"]), 1)

    def test_foreign_holds_properties_and_clones_block_retirement(self):
        record = self.provider.capture(self.ready)
        state = self.fake.snapshots[record["snapshot"]]
        state["holds"].add("foreign")
        with self.assertRaisesRegex(InvalidArchive, "Another owner"):
            abandon_media(self.provider, self.ready, receipt(self.ready))
        self.assertIn(record["hold"], state["holds"])
        state["holds"].remove("foreign")
        state["properties"]["guid"] = "777"
        with self.assertRaisesRegex(InvalidArchive, "replaced"):
            abandon_media(self.provider, self.ready, receipt(self.ready))
        state["properties"]["guid"] = record["snapshot_guid"]
        def block_destroy(*args, **kwargs):
            if args[0] == "destroy":
                raise InvalidArchive("snapshot has a clone")
            return self.fake.run(*args, **kwargs)
        self.provider.run = block_destroy
        with self.assertRaisesRegex(InvalidArchive, "clone"):
            abandon_media(self.provider, self.ready, receipt(self.ready))
        path = self.provider.path(self.ready["uuid"], self.ready["token"])
        self.assertTrue((path / "abandoning.json").is_file())
        self.assertFalse((path / "abandoned.json").exists())
        self.provider.run = self.fake.run
        abandon_media(self.provider, self.ready, receipt(self.ready))
        self.assertFalse(self.fake.snapshots)

    def test_uncertain_destroy_response_and_changed_unsealed_snapshot(self):
        record = self.provider.capture(self.ready)
        path = self.provider.path(self.ready["uuid"], self.ready["token"])
        (path / "manifest.json").unlink()  # Capture died before writing this record.
        def stop(*args, **kwargs):
            if args[0] == "destroy":
                raise OSError("lost worker")
            return self.fake.run(*args, **kwargs)
        self.provider.run = stop
        with self.assertRaises(OSError):
            abandon_media(self.provider, self.ready, receipt(self.ready))
        self.provider.run = self.fake.run
        state = self.fake.snapshots[record["snapshot"]]
        state["properties"]["guid"] = "777"
        with self.assertRaisesRegex(InvalidArchive, "identity changed"):
            abandon_media(self.provider, self.ready, receipt(self.ready))
        state["properties"]["guid"] = record["snapshot_guid"]
        def lost_marker(target, body):
            if target.name == "abandoned.json":
                raise OSError("lost completion marker")
            publish_bytes(target, body)
        with patch("stash_archive.checkpoint_abandon.publish_bytes", side_effect=lost_marker):
            with self.assertRaises(OSError):
                abandon_media(self.provider, self.ready, receipt(self.ready))
        self.assertFalse(self.fake.snapshots)
        abandon_media(self.provider, self.ready, receipt(self.ready))
        self.assertTrue((path / "abandoned.json").is_file())


class ComponentAbandonTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.cache = self.root / "components"
        self.cache.mkdir(mode=0o700)
        self.artwork, self.media = Mock(), Mock()
        self.artwork.cache = self.root / "artwork"
        self.media.cache = self.root / "media"
        self.artwork.cache.mkdir(mode=0o700)
        self.media.cache.mkdir(mode=0o700)
        worker = self.root / "worker"
        worker.mkdir(mode=0o700)
        self.host = HostFilesystemCapture([worker], self.artwork, self.media)
        self.host.__enter__()
        self.addCleanup(self.host.__exit__, None, None, None)
        self.client = self.host.client("https://stash.example", "fixture-key", str(uuid.uuid4()))
        self.ready = {"uuid": self.client.request_id, "token": str(uuid.uuid4()),
                      "request_sha256": self.client.request_hash(0),
                      "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
        self.profile = self.root / "profile.json"
        self.profile.write_bytes(b"original private profile")
        self.parts = [{"role": "config", "name": "profile", "path": self.profile}]

    def test_failed_staging_requires_server_fence_and_retires_only_owned_files(self):
        def failed_copy(_source, target, _reserve):
            target.write_bytes(b"partial")
            raise OSError("capture interrupted")
        with patch("stash_archive.component_stage.copy_file", side_effect=failed_copy):
            with self.assertRaises(OSError):
                self.host.prepare(self.cache, self.client, self.parts, reserve=0)
        path = self.cache / self.client.request_id
        (path / "keep").write_bytes(b"unknown")
        with patch("stash_archive.checkpoint_abandon.abandon_checkpoint", side_effect=InvalidArchive("server busy")):
            with self.assertRaisesRegex(InvalidArchive, "server busy"):
                abandon_host_capture(self.client, 0, self.cache, self.artwork, self.media)
        self.assertEqual((path / "component-00000").read_bytes(), b"partial")
        record = receipt(self.ready)
        with patch("stash_archive.checkpoint_abandon.abandon_checkpoint", return_value=record):
            self.assertEqual(abandon_host_capture(self.client, 0, self.cache, self.artwork, self.media), record)
        self.assertFalse((path / "component-00000").exists())
        self.assertTrue((path / "keep").is_file())
        self.assertEqual(self.profile.read_bytes(), b"original private profile")
        with self.assertRaises(InvalidArchive):
            ComponentStage(self.cache, self.client, self.parts, self.host.barrier, reserve=0)

    def test_challenge_is_saved_before_any_provider_effect(self):
        stage = self.host.prepare(self.cache, self.client, self.parts, reserve=0)
        def capture(ready):
            self.assertEqual((stage.path / "boundary-ready.json").read_bytes(), json_bytes(ready))
            raise OSError("capture interrupted")
        self.artwork.capture.side_effect = capture
        with self.assertRaises(OSError):
            self.host(self.ready)
        self.media.capture.assert_not_called()
        (stage.path / "checkpoint.json").write_bytes(b"sealed evidence")
        with self.assertRaisesRegex(InvalidArchive, "sealed"):
            abandon_components(self.cache, self.client, 0, receipt(self.ready))
        self.assertTrue((stage.path / "component-00000").is_file())

    def test_bad_receipt_never_authorizes_component_deletion(self):
        stage = self.host.prepare(self.cache, self.client, self.parts, reserve=0)
        record = receipt(self.ready)
        for field, value in (("request_sha256", "b" * 64), ("uuid", str(uuid.uuid4())), ("version", True), ("abandoned_at", "invalid")):
            wrong = dict(record, **{field: value})
            with self.assertRaises(InvalidArchive):
                abandon_components(self.cache, self.client, 0, wrong)
            self.assertTrue((stage.path / "component-00000").is_file())


class LockedCommandTests(unittest.TestCase):
    def test_timeout_retains_lock_until_indirect_command_exits(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            marker, release = root / "started", root / "finish"
            fd = os.open(root / "lock", os.O_CREAT | os.O_RDWR, 0o600)
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            contender = os.open(root / "lock", os.O_RDWR)
            command = [sys.executable, "-c", "import pathlib,sys,time; "
                       "pathlib.Path(sys.argv[1]).write_text('started'); "
                       "exec('while not pathlib.Path(sys.argv[2]).exists():\\n time.sleep(0.01)')", str(marker), str(release)]
            try:
                with self.assertRaises(subprocess.TimeoutExpired):
                    run_locked(command, fd, 0.3)
                os.close(fd)
                fd = None
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(contender, fcntl.LOCK_EX | fcntl.LOCK_NB)
                deadline = time.monotonic() + 5
                while not marker.exists() and time.monotonic() < deadline:
                    time.sleep(0.01)
                self.assertTrue(marker.exists())
            finally:
                release.touch()
                if fd is not None:
                    os.close(fd)
                deadline = time.monotonic() + 5
                while True:
                    try:
                        fcntl.flock(contender, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        if time.monotonic() >= deadline:
                            self.fail("command supervisor retained lock after command exit")
                        time.sleep(0.01)
                os.close(contender)
