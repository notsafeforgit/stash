from concurrent.futures import ThreadPoolExecutor
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
import uuid

from stash_ingest.encoding import InvalidData, encode
from stash_ingest.outbox import Capacity, Conflict, LeaseLost, Outbox
from helpers import PRODUCER, capture, file_event, receipt


class OutboxTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.now = [1000.0]
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())

    def open(self, **kwargs):
        return Outbox(self.path, "http://localhost:8009", PRODUCER, clock=lambda: self.now[0], **kwargs)

    def claim(self):
        return self.box.claim(str(uuid.uuid4()))

    def test_receipt_dependency_reopen_replay_and_payload_release(self):
        source, file = capture(), None
        file = file_event(source)
        self.box.enqueue(encode(source))
        self.box.enqueue(encode(file))
        first, = self.claim()
        self.assertEqual(first.event_uuid, source["event_uuid"])
        self.assertEqual(self.claim(), [])
        source_receipt = receipt(source)
        self.box.acknowledge(first, source_receipt)
        self.box.close()
        self.box = self.open()
        self.assertEqual(self.box.receipt(source["event_uuid"]), source_receipt)
        self.box.enqueue(encode(source))
        second, = self.claim()
        self.assertEqual(second.event_uuid, file["event_uuid"])
        self.box.acknowledge(second, receipt(file))
        rows = self.box.db.execute("SELECT body,receipt FROM events").fetchall()
        self.assertTrue(all(row[0] is None and row[1] is not None for row in rows))
        self.assertEqual(self.box.status()["counts"]["acknowledged"], 2)
        self.assertEqual(self.box.status()["queued_bytes"], 0)
        with self.assertRaises(Conflict):
            self.box.enqueue(encode(dict(source, metadata={"title": "changed"})))

    def test_expired_delivery_is_fenced_and_receipt_mismatch_keeps_payload(self):
        source = capture()
        self.box.enqueue(encode(source))
        first, = self.claim()
        self.now[0] += 61
        second, = self.claim()
        self.assertEqual(first.body, second.body)
        self.assertGreater(second.fence, first.fence)
        with self.assertRaises(LeaseLost):
            self.box.acknowledge(first, receipt(source))
        with self.assertRaises(Conflict):
            self.box.acknowledge(second, dict(receipt(source), sha256="b" * 64))
        self.assertIsNotNone(self.box.db.execute("SELECT body FROM events").fetchone()[0])
        self.box.acknowledge(second, receipt(source))

    def test_capacity_backoff_and_explicit_review_retry(self):
        self.box.close()
        self.box = self.open(max_events=1)
        source = capture()
        self.box.enqueue(encode(source))
        with self.assertRaises(Capacity):
            self.box.enqueue(encode(capture()))
        first, = self.claim()
        self.box.fail(first, "network_unavailable", retry_after=120)
        self.now[0] += 119
        self.assertEqual(self.claim(), [])
        self.now[0] += 1
        second, = self.claim()
        self.box.fail(second, "event_rejected_409", review=True)
        self.now[0] += 10000
        self.assertEqual(self.claim(), [])
        self.box.retry(source["event_uuid"])
        third, = self.claim()
        self.assertEqual(first.body, third.body)
        self.box.acknowledge(third, receipt(source))
        self.box.enqueue(encode(capture()))

    def test_byte_limit_and_no_unretained_payload_on_disk(self):
        small = Outbox(Path(self.temp.name) / "small.sqlite", "http://localhost:8009", PRODUCER, max_bytes=1)
        self.addCleanup(small.close)
        with self.assertRaises(Capacity):
            small.enqueue(encode(capture()))
        with self.assertRaises(InvalidData):
            self.box.enqueue(encode(capture(source={"category": "twitter", "password": "not-persisted"})))
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM events").fetchone()[0], 0)

    def test_concurrent_drainers_and_enqueue_replays(self):
        source = capture()

        def worker(_):
            box = self.open()
            try:
                box.enqueue(encode(source))
                return box.claim(str(uuid.uuid4()))
            finally:
                box.close()

        with ThreadPoolExecutor(max_workers=8) as pool:
            results = list(pool.map(worker, range(16)))
        self.assertEqual(sum(len(items) for items in results), 1)
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM events").fetchone()[0], 1)

    def test_process_death_retains_exact_bytes(self):
        path = str(Path(self.temp.name) / "crash.sqlite")
        source = capture()
        script = """import os, sys, uuid
from stash_ingest.outbox import Outbox
box = Outbox(sys.argv[1], 'http://localhost:8009', sys.argv[2], clock=lambda: 1000)
box.enqueue(sys.stdin.buffer.read())
box.claim(str(uuid.uuid4()))
os._exit(19)
"""
        completed = subprocess.run([sys.executable, "-c", script, path, PRODUCER], input=encode(source), check=False, timeout=15)
        self.assertEqual(completed.returncode, 19)
        box = Outbox(path, "http://localhost:8009", PRODUCER, clock=lambda: 1061)
        self.addCleanup(box.close)
        delivery, = box.claim(str(uuid.uuid4()))
        self.assertEqual(delivery.body, encode(source))
        self.assertEqual(delivery.attempts, 2)
        self.assertEqual(box.db.execute("PRAGMA integrity_check").fetchone()[0], "ok")

    def test_binding_foreign_db_and_parent_validation(self):
        for endpoint, producer in (("http://elsewhere.test", PRODUCER), ("http://localhost:8009", str(uuid.uuid4()))):
            with self.assertRaises(Conflict):
                Outbox(self.path, endpoint, producer)
        foreign_path = Path(self.temp.name) / "archive.sqlite"
        db = sqlite3.connect(foreign_path)
        try:
            db.execute("CREATE TABLE archive(id TEXT)")
            db.commit()
        finally:
            db.close()
        with self.assertRaises(InvalidData):
            Outbox(foreign_path, "http://localhost:8009", PRODUCER)
        source = capture()
        with self.assertRaises(Conflict):
            self.box.enqueue(encode(file_event(source)))
        self.box.enqueue(encode(source))
        with self.assertRaises(Conflict):
            self.box.enqueue(encode(file_event(source, collection_revision=2)))
        link = Path(self.temp.name) / "link.sqlite"
        link.symlink_to(self.path)
        with self.assertRaises(OSError):
            Outbox(link, "http://localhost:8009", PRODUCER)
        self.assertEqual(os.stat(self.path).st_mode & 0o777, 0o600)


if __name__ == "__main__":
    unittest.main()
