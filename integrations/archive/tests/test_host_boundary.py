from datetime import datetime, timedelta, timezone
import fcntl
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_archive.filesystem_boundary import FORMAT, read_checkpoint_response
from stash_archive.host_boundary import HostFilesystemCapture
from stash_archive.storage import InvalidArchive
from stash_ingest.publication_lock import ACTIVE
from test_filesystem_boundary import line, response


class HostBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.artwork, self.media = Mock(), Mock()
        self.artwork.capture.return_value = {"pinned": True}
        self.media.capture.return_value = {"retained": True}

    def locked(self):
        fd = os.open(self.root / ACTIVE, os.O_RDWR)
        try:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                return False
            except BlockingIOError:
                return True
        finally:
            os.close(fd)

    def test_fresh_capture_releases_workers_before_ack_and_database_copy(self):
        with HostFilesystemCapture([self.root], self.artwork, self.media) as host:
            client = host.client("https://stash.example", "fixture", str(uuid.uuid4()))
            ready = {"uuid": client.request_id, "token": str(uuid.uuid4()), "request_sha256": "a" * 64,
                     "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
            def retain(_):
                self.assertTrue(self.locked())
                return {"retained": True}
            self.media.capture.side_effect = retain
            def acknowledge(_, body):
                self.assertFalse(self.locked())
                confirmation = json.loads(body)
                receipt = {"format": FORMAT, "version": 1, "uuid": ready["uuid"], "token": ready["token"],
                           "request_sha256": ready["request_sha256"], "confirmed_at": datetime.now(timezone.utc).isoformat(),
                           "details": confirmation["details"]}
                return response("application/json", line(receipt))
            with patch.object(client, "open", side_effect=acknowledge):
                body = line({"event": "boundary_ready", "ready": ready}) + line({"event": "sealed", "checkpoint": {}})
                read_checkpoint_response(client, response("application/x-ndjson", body), ready["request_sha256"])
            self.assertFalse(self.locked())
            self.assertEqual(len(client.boundary_receipt["details"]["producer_barriers"]), 1)
        self.assertFalse(self.locked())

    def test_sealed_replay_releases_before_component_download_without_recapture(self):
        with HostFilesystemCapture([self.root], self.artwork, self.media) as host:
            client = host.client("https://stash.example", "fixture", str(uuid.uuid4()))
            self.assertTrue(self.locked())
            read_checkpoint_response(client, response("application/json", b"{}"), "a" * 64)
            self.assertFalse(self.locked())
            self.artwork.capture.assert_not_called()
            self.media.capture.assert_not_called()
            with self.assertRaises(InvalidArchive): host({})

    def test_failure_and_context_exit_release_workers(self):
        with self.assertRaisesRegex(RuntimeError, "capture failed"):
            with HostFilesystemCapture([self.root], self.artwork, self.media) as host:
                self.media.capture.side_effect = RuntimeError("capture failed")
                host({})
        self.assertFalse(self.locked())
        with self.assertRaises(InvalidArchive): host.client("https://stash.example", "fixture", str(uuid.uuid4()))

    def test_transport_failure_releases_before_leaving_enclosing_context(self):
        with HostFilesystemCapture([self.root], self.artwork, self.media) as host:
            client = host.client("https://stash.example", "fixture", str(uuid.uuid4()))
            with patch.object(client, "open", side_effect=InvalidArchive("transport failed")):
                with self.assertRaisesRegex(InvalidArchive, "transport failed"):
                    client.capture(self.root / "stage", reserve=0)
            self.assertFalse(self.locked())
            self.assertFalse((self.root / "stage").exists())

    def test_sealed_worker_inventory_is_checked_before_reusing_retained_views(self):
        with HostFilesystemCapture([self.root], self.artwork, self.media) as host:
            boundary = {"details": host({})}
            host.validate(boundary)
            self.media.open_bound.assert_called_once_with(boundary)
            boundary["details"]["producer_barriers"][0]["inode"] += 1
            with self.assertRaises(InvalidArchive): host.validate(boundary)
            self.assertEqual(self.media.open_bound.call_count, 1)

    def test_existing_destination_releases_barrier_and_preserves_existing_files(self):
        stage = self.root / "stage"
        stage.mkdir()
        (stage / "existing").write_bytes(b"keep")
        with HostFilesystemCapture([self.root], self.artwork, self.media) as host:
            client = host.client("https://stash.example", "fixture", str(uuid.uuid4()))
            with patch.object(client, "open") as transport:
                with self.assertRaises(FileExistsError): client.capture(stage, reserve=0)
            self.assertFalse(self.locked())
            transport.assert_not_called()
            self.assertEqual((stage / "existing").read_bytes(), b"keep")
