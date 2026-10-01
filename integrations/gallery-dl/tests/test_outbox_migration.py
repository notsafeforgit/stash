from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import sqlite3
import tempfile
import unittest
import uuid

from stash_ingest.encoding import digest, encode
from stash_ingest.outbox import Conflict, Outbox, SCHEMA
from helpers import PRODUCER, capture, file_event, receipt


class OutboxMigrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "legacy.sqlite"
        self.db = sqlite3.connect(self.path, isolation_level=None)
        self.addCleanup(self.db.close)
        self.db.executescript((Path(__file__).parent / "fixtures/outbox-v1.sql").read_text())
        self.db.execute("PRAGMA foreign_keys=ON")
        self.db.execute("INSERT INTO binding VALUES(1,?,?)", ("http://fixture.invalid", PRODUCER))
        self.source, self.other = capture(), capture()
        self.file = file_event(self.source)
        for event, state in ((self.source, "acknowledged"), (self.file, "pending"), (self.other, "sending")):
            parent = event.get("source", {}).get("capture_event_uuid") if event["kind"] == "file.completed" else None
            body = encode(event)
            ack = encode(receipt(event)) if state == "acknowledged" else None
            self.db.execute("""INSERT INTO events(event_uuid,sha256,kind,collection_uuid,collection_revision,
                root_uuid,run_uuid,parent_uuid,body,receipt,state,owner,fence,lease_until,available_at,created_at,attempts)
                VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""", (event["event_uuid"], digest(body), event["kind"],
                event["collection_uuid"], event["collection_revision"], event["root_uuid"], event["run_uuid"], parent,
                None if ack else body, ack, state, str(uuid.uuid4()) if state == "sending" else None,
                7, 2000 if state == "sending" else None, 1000, 900, 3))
        self.before = self.db.execute("SELECT * FROM events ORDER BY seq").fetchall()

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: 1000)

    def test_concurrent_promotion_preserves_pending_dependencies_leases_and_receipts(self):
        def open_queue(_):
            queue = self.open()
            try:
                return queue.db.execute("PRAGMA user_version").fetchone()[0]
            finally:
                queue.close()
        with ThreadPoolExecutor(max_workers=4) as pool:
            self.assertEqual(list(pool.map(open_queue, range(4))), [SCHEMA] * 4)
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])
        self.assertEqual(self.db.execute("PRAGMA integrity_check").fetchone()[0], "ok")
        queue = self.open()
        self.addCleanup(queue.close)
        delivery, = queue.claim(str(uuid.uuid4()))
        self.assertEqual(delivery.event_uuid, self.file["event_uuid"])
        self.assertEqual(delivery.body, encode(self.file))
        self.assertIsNotNone(queue.receipt(self.source["event_uuid"]))
        self.assertEqual(queue.db.execute("SELECT count(*) FROM run_requests").fetchone()[0], 0)

    def test_wrong_binding_and_schema_collision_roll_back_the_whole_migration(self):
        with self.assertRaises(Conflict):
            Outbox(self.path, "http://elsewhere.invalid", PRODUCER)
        self.assertEqual(self.db.execute("PRAGMA user_version").fetchone()[0], 1)
        self.assertIsNone(self.db.execute("SELECT name FROM sqlite_schema WHERE name='run_intents'").fetchone())
        self.db.execute("CREATE TABLE run_requests(unrecognized TEXT)")
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        self.assertEqual(self.db.execute("PRAGMA user_version").fetchone()[0], 1)
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)
        self.assertIsNone(self.db.execute("SELECT name FROM sqlite_schema WHERE name='run_intents'").fetchone())


if __name__ == "__main__":
    unittest.main()
