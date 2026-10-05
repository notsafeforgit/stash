from datetime import datetime, timedelta, timezone
import copy
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_archive.storage import InvalidArchive, publish_bytes
from stash_archive.zfs_media import PROPERTY, ZFSMedia, release_published_media


class FakeZFS:
    """Stateful command boundary; the separate host probe exercises real ZFS."""
    def __init__(self, root):
        self.root, self.snapshots, self.calls = root, {}, []
        self.extra_dataset = False

    def run(self, *args, **_):
        self.calls.append(args)
        action, name = args[0], args[-1]
        if action == "list":
            if "snapshot" in args:
                return "".join(k + "\n" for k in self.snapshots)
            result = f"tank/media\tfilesystem\t{self.root}\tyes\t123\n"
            return result + ("tank/media/child\tfilesystem\t/child\tyes\t456\n" if self.extra_dataset else "")
        if action == "snapshot":
            if name in self.snapshots:
                raise InvalidArchive("ZFS snapshot already exists")
            props = dict(item.split("=", 1) for item in args[2:-1:2])
            props.update(guid="18446744073709551615", createtxg="100", defer_destroy="off")
            self.snapshots[name] = {"properties": props, "holds": set()}
            target = self.root / ".zfs/snapshot" / name.split("@", 1)[1]
            target.mkdir(parents=True)
            shutil.copytree(self.root / "media", target / "media")
            return ""
        if name not in self.snapshots:
            raise InvalidArchive("ZFS snapshot unavailable")
        state = self.snapshots[name]
        if action == "get":
            return "".join(f"{name}\t{k}\t{v}\n" for k, v in state["properties"].items())
        if action == "hold": state["holds"].add(args[1])
        elif action == "holds": return "".join(f"{name}\t{h}\t100\n" for h in sorted(state["holds"]))
        elif action == "release": state["holds"].remove(args[1])
        elif action == "destroy":
            if state["holds"]: raise InvalidArchive("ZFS snapshot is still held")
            del self.snapshots[name]
            shutil.rmtree(self.root / ".zfs/snapshot" / name.split("@", 1)[1])
        else: raise AssertionError(args)
        return ""


class ZFSMediaTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cache, self.media = self.root / "cache", self.root / "dataset"
        self.cache.mkdir(mode=0o700)
        (self.media / "media").mkdir(parents=True)
        (self.media / "media/video.mp4").write_bytes(b"original media")
        self.fake = FakeZFS(self.media)
        self.provider = ZFSMedia(self.cache, "tank/media", "123", self.media, reserve=0)
        self.provider.run = self.fake.run
        self.ready = {"uuid": str(uuid.uuid4()), "token": str(uuid.uuid4()), "request_sha256": "a" * 64,
                      "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
        self.mounts = [(self.media, "zfs", "tank/media")]
        self.addCleanup(patch.stopall)
        patch("stash_archive.zfs_media.mounts", side_effect=lambda: self.mounts).start()
        original_statvfs = os.statvfs
        def readonly(path):
            info = list(original_statvfs(path))
            info[8] |= os.ST_RDONLY
            return os.statvfs_result(info)
        patch("stash_archive.zfs_media.os.statvfs", side_effect=readonly).start()

    def capture(self):
        record = self.provider.capture(self.ready)
        return {**{k: self.ready[k] for k in ("uuid", "token", "request_sha256")}, "details": {"media": record}}

    def release(self, boundary, release=None):
        receipt = release or {"archive": "durable verified fixture"}
        with patch("stash_archive.checkpoint_release.archived_boundary", return_value=boundary), \
                patch("stash_archive.checkpoint_release.release_published_checkpoint", return_value=receipt):
            return release_published_media(self.root / "bundle", None, self.provider)

    def test_original_view_reopens_without_recapture_after_live_change(self):
        boundary = self.capture()
        view = self.provider.open_bound(boundary).verify()
        (self.media / "media/video.mp4").write_bytes(b"new live media")
        self.assertEqual(view.resolve("media/video.mp4").read_bytes(), b"original media")
        reopened = ZFSMedia(self.cache, "tank/media", "123", self.media, reserve=0)
        reopened.run = self.fake.run
        self.assertEqual(reopened.open_bound(boundary).verify().resolve("media/video.mp4").read_bytes(), b"original media")
        with self.assertRaises(FileExistsError): self.capture()
        self.assertEqual(sum(args[0] == "snapshot" for args in self.fake.calls), 1)
        for path in ("../media/video.mp4", "/etc/passwd", ".zfs/snapshot"):
            with self.assertRaises(InvalidArchive): view.resolve(path)
        (view.root / "escape").symlink_to(self.media / "media/video.mp4")
        with self.assertRaises(InvalidArchive): view.resolve("escape")

    def test_topology_guid_and_mount_changes_fail_before_creation(self):
        for change in ("dataset_guid", "child", "unmounted", "nested"):
            with self.subTest(change=change):
                self.fake.extra_dataset = change == "child"
                self.provider.dataset_guid = "456" if change == "dataset_guid" else "123"
                self.mounts[:] = [] if change == "unmounted" else [(self.media, "zfs", "tank/media")]
                if change == "nested": self.mounts.append((self.media / "media", "ext4", "/dev/other"))
                with self.assertRaises(InvalidArchive): self.capture()
        self.assertFalse(any(args[0] == "snapshot" for args in self.fake.calls))
        self.assertEqual(list(self.cache.iterdir()), [])

    def test_sealed_identity_changed_snapshot_and_hold_cannot_be_replaced(self):
        boundary = self.capture()
        view = self.provider.open_bound(boundary)
        for key in ("uuid", "token", "request_sha256"):
            wrong = copy.deepcopy(boundary)
            wrong[key] = "changed"
            with self.assertRaises(InvalidArchive): self.provider.open_bound(wrong)
        props = self.fake.snapshots[view.record["snapshot"]]["properties"]
        for key, value in (("guid", "999"), ("createtxg", "101"), ("defer_destroy", "on"), (PROPERTY + "request", "b" * 64)):
            previous, props[key] = props[key], value
            with self.assertRaises(InvalidArchive): view.verify()
            props[key] = previous
        self.fake.snapshots[view.record["snapshot"]]["holds"].clear()
        with self.assertRaises(InvalidArchive): view.verify()
        self.assertEqual(sum(args[0] == "snapshot" for args in self.fake.calls), 1)

    def test_failed_hold_retains_intent_and_never_recaptures(self):
        real_run = self.fake.run
        def fail_hold(*args, **kwargs):
            if args[0] == "hold": raise InvalidArchive("permission denied")
            return real_run(*args, **kwargs)
        self.provider.run = fail_hold
        with self.assertRaisesRegex(InvalidArchive, "permission denied"): self.capture()
        path = self.provider.path(self.ready["uuid"], self.ready["token"])
        self.assertTrue((path / "intent.json").is_file())
        self.assertFalse((path / "manifest.json").exists())
        with self.assertRaises(FileExistsError): self.capture()
        self.assertEqual(len(self.fake.snapshots), 1)

    def test_release_resumes_after_destruction_but_before_completion_marker(self):
        boundary = self.capture()
        view = self.provider.open_bound(boundary)
        def interrupted(path, body):
            if path.name == "released.json": raise OSError("interrupted")
            publish_bytes(path, body)
        with patch("stash_archive.zfs_media.publish_bytes", side_effect=interrupted):
            with self.assertRaisesRegex(OSError, "interrupted"): self.release(boundary)
        self.assertEqual(self.fake.snapshots, {})
        self.assertTrue((view.path / "release.json").is_file())
        with self.assertRaises(InvalidArchive): self.provider.open_bound(boundary)
        self.assertEqual(self.release(boundary), self.release(boundary))
        self.assertTrue((view.path / "released.json").is_file())
        self.assertTrue((view.path / "manifest.json").is_file())
        with self.assertRaises(InvalidArchive): self.release(boundary, {"archive": "different"})
        destroys = [args for args in self.fake.calls if args[0] == "destroy"]
        self.assertEqual(destroys, [("destroy", view.record["snapshot"])])

    def test_foreign_hold_or_changed_guid_blocks_release_without_forcing(self):
        boundary = self.capture()
        view = self.provider.open_bound(boundary)
        state = self.fake.snapshots[view.record["snapshot"]]
        state["properties"]["guid"] = "456"
        with self.assertRaises(InvalidArchive): self.release(boundary)
        self.assertFalse((view.path / "release.json").exists())
        self.assertIn(view.record["hold"], state["holds"])
        state["properties"]["guid"] = view.record["snapshot_guid"]
        state["holds"].add("another-backup")
        with self.assertRaises(InvalidArchive): self.release(boundary)
        self.assertIn("another-backup", state["holds"])
        state["holds"].remove("another-backup")
        self.release(boundary)
        self.assertEqual(self.fake.snapshots, {})

    def test_missing_snapshot_or_failed_listing_is_not_success(self):
        boundary = self.capture()
        view = self.provider.open_bound(boundary)
        self.fake.snapshots.clear()
        with self.assertRaises(InvalidArchive): self.release(boundary)
        self.assertFalse((view.path / "released.json").exists())
        self.provider.run = lambda *_args, **_kwargs: (_ for _ in ()).throw(InvalidArchive("device unavailable"))
        with self.assertRaisesRegex(InvalidArchive, "device unavailable"): self.release(boundary)
        self.assertFalse((view.path / "released.json").exists())
