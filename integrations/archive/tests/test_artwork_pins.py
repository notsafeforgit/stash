from datetime import datetime, timedelta, timezone
import copy
import errno
import hashlib
import os
from pathlib import Path
import shutil
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import uuid

from stash_archive.artwork_pins import ArtworkPins
from stash_archive.durability import syncfs_function
from stash_archive.bundle import export_archive
from stash_archive.storage import InvalidArchive, require_space, store_file


class ArtworkPinTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cache, self.source = self.root / "pins", self.root / "artwork"
        self.cache.mkdir(mode=0o700)
        self.source.mkdir()
        self.originals = {}
        for data in (b"original cover", b"original performer image"):
            checksum = hashlib.md5(data).hexdigest()
            path = self.source / checksum[:2] / checksum[2:4] / checksum
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            self.originals[checksum] = (path, data)
        self.ready = {"uuid": str(uuid.uuid4()), "token": str(uuid.uuid4()), "request_sha256": "a" * 64,
                      "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
        self.pins = ArtworkPins(self.cache, [self.source], reserve=0)

    def capture(self):
        record = self.pins.capture(self.ready)
        return {**{k: self.ready[k] for k in ("uuid", "token", "request_sha256")}, "details": {"artwork": record}}

    def test_filesystem_flush_precedes_manifest_without_per_inode_flushes(self):
        inodes = {path.stat().st_ino for path, _ in self.originals.values()}
        fsync, flushed_inodes, boundaries = os.fsync, [], []
        target = self.pins.path(self.ready["uuid"], self.ready["token"])

        def checked_fsync(fd):
            if os.fstat(fd).st_ino in inodes:
                flushed_inodes.append(os.fstat(fd).st_ino)
            fsync(fd)

        def boundary(fd):
            self.assertEqual(os.fstat(fd).st_dev, self.cache.stat().st_dev)
            self.assertFalse((target / "manifest.json").exists())
            self.assertTrue((target / "0/inventory.jsonl").is_file())
            for checksum, (source, data) in self.originals.items():
                retained = target / "0" / checksum
                self.assertEqual(retained.stat().st_ino, source.stat().st_ino)
                self.assertEqual(retained.read_bytes(), data)
            boundaries.append(fd)

        with patch("stash_archive.durability.syncfs_function", return_value=boundary), patch("os.fsync", side_effect=checked_fsync):
            view = self.pins.open_bound(self.capture()).verify()
        self.assertEqual(flushed_inodes, [])
        self.assertEqual(len(boundaries), 1)
        self.assertTrue((view.path / "manifest.json").is_file())
        with self.assertRaises(OSError): os.fstat(boundaries[0])

    def test_unavailable_filesystem_flush_keeps_per_file_durability(self):
        inodes = {path.stat().st_ino for path, _ in self.originals.values()}
        fsync, flushed = os.fsync, []

        def checked(fd):
            inode = os.fstat(fd).st_ino
            if inode in inodes:
                flushed.append(inode)
            fsync(fd)

        with patch("stash_archive.durability.syncfs_function", return_value=None), patch("os.fsync", side_effect=checked):
            self.pins.open_bound(self.capture()).verify()
        self.assertCountEqual(flushed, inodes)

    def test_kernel_without_writeback_error_reporting_uses_file_flushes(self):
        for release in ("4.19.0", "5.7.99", "unknown"):
            with self.subTest(release=release), patch("stash_archive.durability.sys.platform", "linux"), \
                 patch("stash_archive.durability.os.uname", return_value=SimpleNamespace(release=release)), \
                 patch("stash_archive.durability.ctypes.CDLL") as library:
                self.assertIsNone(syncfs_function())
                library.assert_not_called()

    def test_filesystem_writeback_failure_or_expired_flush_never_seals(self):
        for mode in ("writeback", "timeout"):
            with self.subTest(mode=mode):
                self.ready["token"] = str(uuid.uuid4())
                now = [time.monotonic()]

                def flush(fd):
                    if mode == "writeback":
                        raise OSError(errno.EIO, "injected filesystem writeback failure")
                    now[0] += 60

                with patch("stash_archive.durability.syncfs_function", return_value=flush), \
                     patch("stash_archive.artwork_pins.time.monotonic", side_effect=lambda: now[0]):
                    with self.assertRaises((OSError, InvalidArchive)):
                        self.capture()
                target = self.pins.path(self.ready["uuid"], self.ready["token"])
                self.assertTrue((target / "intent.json").is_file())
                self.assertFalse((target / "manifest.json").exists())

    def test_retained_inodes_survive_atomic_replacement_and_source_removal(self):
        boundary = self.capture()
        view = self.pins.open_bound(boundary).verify()
        self.assertEqual(boundary["details"]["artwork"]["roots"][0]["count"], 2)
        for checksum, (path, data) in self.originals.items():
            self.assertEqual(path.stat().st_ino, view.resolve(checksum).stat().st_ino)
            replacement = path.with_suffix(".new")
            replacement.write_bytes(b"changed later")
            os.replace(replacement, path)
            self.assertEqual(view.resolve(checksum).read_bytes(), data)
        shutil.rmtree(self.source)
        reopened = ArtworkPins(self.cache, [self.source], reserve=0).open_bound(boundary).verify()
        for checksum, (_, data) in self.originals.items():
            self.assertEqual(reopened.resolve(checksum).read_bytes(), data)
        self.assertEqual(view.record, reopened.record)

    def test_changed_binding_inventory_and_inode_are_rejected(self):
        boundary = self.capture()
        for field in ("uuid", "token", "request_sha256"):
            wrong = copy.deepcopy(boundary)
            wrong[field] = str(uuid.uuid4()) if field != "request_sha256" else "b" * 64
            with self.assertRaises(InvalidArchive): self.pins.open_bound(wrong)
        view = self.pins.open_bound(boundary)
        inventory = view.path / "0/inventory.jsonl"
        body = inventory.read_bytes()
        inventory.write_bytes(body + body.splitlines(keepends=True)[-1])
        with self.assertRaises(InvalidArchive): view.verify()
        inventory.write_bytes(body)
        checksum = next(iter(self.originals))
        retained = view.resolve(checksum)
        retained.unlink()
        retained.write_bytes(self.originals[checksum][1])
        with self.assertRaises(InvalidArchive): view.verify()

    def test_failed_capture_keeps_its_attempt_and_never_recaptures(self):
        boundary = self.capture()
        path = self.pins.open_bound(boundary).path
        with self.assertRaises(FileExistsError): self.capture()
        self.assertTrue((path / "manifest.json").is_file())
        self.ready["token"] = str(uuid.uuid4())
        link = os.link
        def failed_link(source, target, **kwargs):
            if kwargs.get("src_dir_fd") is not None:
                raise OSError("failed link")
            return link(source, target, **kwargs)
        with patch("stash_archive.artwork_pins.os.link", side_effect=failed_link):
            with self.assertRaises(OSError): self.capture()
        failed = self.pins.path(self.ready["uuid"], self.ready["token"])
        self.assertEqual(set(self.cache.iterdir()), {path, failed})
        self.assertTrue((failed / "intent.json").is_file())
        self.assertFalse((failed / "manifest.json").exists())
        self.ready["expires_at"] = "2000-01-01T00:00:00+00:00"
        with self.assertRaises(InvalidArchive): self.capture()
        self.assertEqual(set(self.cache.iterdir()), {path, failed})

    def test_symlinks_space_and_live_path_fallback_are_rejected(self):
        path, _ = next(iter(self.originals.values()))
        external = self.root / "outside"
        external.write_bytes(b"outside")
        path.unlink()
        path.symlink_to(external)
        with self.assertRaises(InvalidArchive): self.capture()
        self.assertTrue((self.pins.path(self.ready["uuid"], self.ready["token"]) / "intent.json").is_file())
        self.pins.reserve = 1 << 100
        with self.assertRaises(InvalidArchive): self.capture()
        with self.assertRaises(InvalidArchive):
            export_archive(None, self.root / "export", artwork_pins=self.pins, blob_paths=[self.source])
        self.assertFalse((self.root / "export").exists())

    def test_live_unlink_during_packing_keeps_verified_retained_bytes(self):
        view = self.pins.open_bound(self.capture()).verify()
        checksum, (source, data) = next(iter(self.originals.items()))
        retained = view.resolve(checksum)
        (self.root / "objects").mkdir()
        before = retained.stat()

        def unlink_live(*args, **kwargs):
            source.unlink(missing_ok=True)
            require_space(*args, **kwargs)

        with patch("stash_archive.storage.require_space", side_effect=unlink_live):
            artifact = store_file(self.root, retained, reserve=0, expected_md5=checksum)
        self.assertEqual(artifact["sha256"], hashlib.sha256(data).hexdigest())
        self.assertLess(retained.stat().st_nlink, before.st_nlink)
        with self.assertRaises(InvalidArchive):
            store_file(self.root, retained, reserve=0, expected_md5="f" * 32)
