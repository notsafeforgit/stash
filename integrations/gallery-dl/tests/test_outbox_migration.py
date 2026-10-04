from concurrent.futures import ThreadPoolExecutor
from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
import threading
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.encoding import digest, encode
from stash_ingest.outbox import Conflict, Outbox, SCHEMA
from stash_ingest.run_queue import RunQueue
from helpers import PRODUCER, capture, file_event, receipt
from test_run_queue import request, admission


def schema_twelve(db):
    db.execute("DROP TABLE discovery_collection_dispatch")
    db.execute("ALTER TABLE worker_dispatch DROP COLUMN discovery_delivery_after")
    db.execute("ALTER TABLE worker_dispatch RENAME COLUMN enrichment_delivery_after TO delivery_after")
    db.execute("PRAGMA user_version=12")


def schema_eleven(db):
    schema_twelve(db)
    db.execute("DROP TABLE discovery_dispatch")
    db.execute("PRAGMA user_version=11")


def schema_ten(db):
    schema_eleven(db)
    db.execute("DROP TABLE discovery_executions")
    db.execute("PRAGMA user_version=10")


def schema_nine(db):
    schema_ten(db)
    db.execute("DROP TABLE worker_dispatch")
    db.execute("DROP TABLE enrichment_collection_dispatch")
    db.execute("PRAGMA user_version=9")


def schema_eight(db):
    schema_nine(db)
    db.execute("DROP TABLE enrichment_dispatch")
    db.execute("PRAGMA user_version=8")


def schema_seven(db):
    schema_eight(db)
    db.execute("DROP TABLE enrichment_executions")
    db.execute("PRAGMA user_version=7")


def schema_six(db):
    schema_seven(db)
    db.execute("DROP TRIGGER native_n8n_token_unique")
    db.execute("DROP TABLE legacy_n8n_receipts")
    db.execute("DROP TABLE n8n_receipt_imports")
    db.execute("PRAGMA user_version=6")


def schema_five(db):
    schema_six(db)
    db.execute("DROP TABLE backfill_calls")
    db.execute("PRAGMA user_version=5")


def schema_four(db):
    schema_five(db)
    db.execute("DROP TABLE source_call_targets")
    db.execute("DROP TABLE source_calls")
    db.execute("PRAGMA user_version=4")


def schema_three(db):
    """Remove exactly schema 4's additions to construct a populated v3 input."""
    schema_four(db)
    db.execute("DROP TABLE run_ticket_requests")
    db.execute("DROP INDEX unassigned_run_tickets")
    db.execute("ALTER TABLE run_intent_tickets DROP COLUMN unassigned")
    db.execute("ALTER TABLE run_intent_tickets DROP COLUMN unassigned_count")
    db.execute("PRAGMA user_version=3")


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

    def test_initial_wal_contention_waits_without_replacing_existing_events(self):
        # A journal-mode change can report BUSY without honoring busy_timeout.
        # Exercise that with a real reader lock and a zero-timeout connection.
        connect = sqlite3.connect
        retried = threading.Event()
        attempts = 0

        def trace(statement):
            nonlocal attempts
            if statement == "PRAGMA journal_mode=WAL":
                attempts += 1
                if attempts > 1:
                    retried.set()

        def without_busy_wait(*args, **kwargs):
            kwargs["timeout"] = 0
            db = connect(*args, **kwargs)
            db.set_trace_callback(trace)
            return db

        def open_queue():
            with closing(self.open()) as box:
                return box.db.execute("PRAGMA user_version").fetchone()[0]

        self.db.execute("BEGIN")
        self.db.execute("SELECT count(*) FROM events").fetchone()
        with patch("stash_ingest.outbox.sqlite3.connect", side_effect=without_busy_wait), ThreadPoolExecutor(max_workers=1) as pool:
            opened = pool.submit(open_queue)
            try:
                self.assertTrue(retried.wait(3), "journal mode was not retried while the database was busy")
                self.assertFalse(opened.done())
            finally:
                self.db.execute("COMMIT")
            self.assertEqual(opened.result(timeout=3), SCHEMA)

    def test_schema_publication_between_preflight_reads_uses_one_snapshot(self):
        empty = Path(self.temp.name) / "concurrent-empty.sqlite"
        connect = sqlite3.connect
        with closing(connect(empty, isolation_level=None)) as raw:
            raw.execute("PRAGMA journal_mode=WAL")
        reader_thread = threading.get_ident()
        observed = False
        with ThreadPoolExecutor(max_workers=1) as pool:
            def publish():
                with closing(Outbox(empty, "http://fixture.invalid", PRODUCER)) as writer:
                    writer.enqueue(encode(capture()))

            class Reader(sqlite3.Connection):
                def execute(connection, sql, *args, **kwargs):
                    nonlocal observed
                    cursor = super().execute(sql, *args, **kwargs)
                    if sql == "PRAGMA user_version" and not observed:
                        observed = True
                        pool.submit(publish).result(timeout=5)
                    return cursor

            def intercepted(*args, **kwargs):
                if threading.get_ident() == reader_thread:
                    kwargs["factory"] = Reader
                return connect(*args, **kwargs)

            with patch("stash_ingest.outbox.sqlite3.connect", side_effect=intercepted), \
                    closing(Outbox(empty, "http://fixture.invalid", PRODUCER)) as reader:
                self.assertTrue(observed)
                self.assertEqual(reader.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
                self.assertEqual(reader.db.execute("SELECT count(*) FROM events").fetchone()[0], 1)
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])

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

    def test_v2_promotion_preserves_frozen_requests_tickets_and_admissions(self):
        tables = ("binding", "events", "run_intents", "run_requests", "run_intent_tickets")
        with closing(self.open()) as box:
            queue = RunQueue(box)
            queue.enqueue(request(), ticket_uuid=str(uuid.uuid4()))
            sent = queue.claim(str(uuid.uuid4()))
            queue.admit(sent, admission(sent.body))
            queue.enqueue(request(30, 40), ticket_uuid=str(uuid.uuid4()))
            queue.claim(str(uuid.uuid4()))
            queue.enqueue(request(50, 60, policy_sha256="b" * 64), ticket_uuid=str(uuid.uuid4()))
            # These are the exact schema-2 tables; only the new cursor table is
            # removed to retain real populated admission and lease state.
            schema_three(box.db)
            box.db.execute("DROP TABLE dispatch_cursors")
            box.db.execute("PRAGMA user_version=2")
        columns = {table: ','.join(row[1] for row in self.db.execute(f"PRAGMA table_info({table})")) for table in tables}
        before = {table: self.db.execute(f"SELECT {columns[table]} FROM {table}").fetchall() for table in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            self.assertEqual(box.db.execute("SELECT count(*) FROM dispatch_cursors").fetchone()[0], 0)
        self.assertEqual({table: self.db.execute(f"SELECT {columns[table]} FROM {table}").fetchall() for table in tables}, before)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_v2_cursor_collision_keeps_old_version_and_operational_state(self):
        with closing(self.open()) as box:
            schema_three(box.db)
            box.db.execute("DROP TABLE dispatch_cursors")
            box.db.execute("PRAGMA user_version=2")
            box.db.execute("CREATE TABLE dispatch_cursors(unrecognized TEXT)")
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        self.assertEqual(self.db.execute("PRAGMA user_version").fetchone()[0], 2)
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)

    def test_v4_promotion_preserves_ticket_assignments_dispatch_and_operational_rows(self):
        with closing(self.open()) as box:
            queue = RunQueue(box)
            queue.enqueue(request(), ticket_uuid=str(uuid.uuid4()))
            sent = queue.claim(str(uuid.uuid4()))
            queue.admit(sent, admission(sent.body))
            queue.enqueue(request(30, 40), ticket_uuid=str(uuid.uuid4()))
            queue.claim(str(uuid.uuid4()))
            box.db.execute("INSERT INTO dispatch_cursors(root_uuid,policy_sha256,after_sequence,failures,available_at) VALUES(?,?,50,3,2000)",
                           (str(uuid.uuid4()), "a" * 64))
            schema_four(box.db)
        tables = [row[0] for row in self.db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")]
        before = {name: self.db.execute('SELECT * FROM "' + name + '" ORDER BY rowid').fetchall() for name in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            self.assertEqual(box.db.execute("SELECT count(*) FROM source_calls").fetchone()[0], 0)
            self.assertEqual(box.db.execute("SELECT count(*) FROM source_call_targets").fetchone()[0], 0)
        after = {name: self.db.execute('SELECT * FROM "' + name + '" ORDER BY rowid').fetchall() for name in tables}
        self.assertEqual(after, before)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_v4_unknown_caller_table_rolls_back_without_recreating_the_outbox(self):
        with closing(self.open()) as box:
            schema_four(box.db)
            box.db.execute("CREATE TABLE source_calls(unrecognized TEXT)")
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        self.assertEqual(self.db.execute("PRAGMA user_version").fetchone()[0], 4)
        self.assertIsNone(self.db.execute("SELECT name FROM sqlite_schema WHERE name='source_call_targets'").fetchone())
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)

    def test_v5_promotion_preserves_bound_and_pending_calls_with_original_submissions(self):
        from stash_ingest.source_calls import SourceCalls, resolve_once
        from test_source_calls import snapshot, candidate
        from unittest.mock import Mock
        with closing(self.open()) as box:
            calls = SourceCalls(box)
            calls.record(str(uuid.uuid4()), "a" * 64, snapshot)
            client = Mock(endpoint=box.endpoint, producer=box.producer)
            client.capabilities.return_value = {"collection_lookup": True}
            from stash_ingest.encoding import decode
            client._request.side_effect = lambda method, route, body, **kwargs: {
                "root_uuid": snapshot()["root_uuid"], "targets": [{"target_url": url,
                    "candidates": [candidate(url)], "has_more": False} for url in decode(body)["targets"]]}
            self.assertEqual(resolve_once(calls, client)["state"], "queued")
            queue = RunQueue(box)
            sent = queue.claim(str(uuid.uuid4()))
            queue.admit(sent, admission(sent.body))
            calls.record(str(uuid.uuid4()), "b" * 64, snapshot)
            schema_five(box.db)
        tables = [row[0] for row in self.db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")]
        before = {name: self.db.execute('SELECT * FROM "' + name + '" ORDER BY rowid').fetchall() for name in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            self.assertEqual(box.db.execute("SELECT count(*) FROM backfill_calls").fetchone()[0], 0)
        after = {name: self.db.execute('SELECT * FROM "' + name + '" ORDER BY rowid').fetchall() for name in tables}
        self.assertEqual(after, before)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_v5_unknown_backfill_table_rolls_back_instead_of_replacing_history(self):
        with closing(self.open()) as box:
            schema_five(box.db)
            box.db.execute("CREATE TABLE backfill_calls(unrecognized TEXT)")
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        self.assertEqual(self.db.execute("PRAGMA user_version").fetchone()[0], 5)
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)

    def test_v6_promotion_preserves_backfill_snapshots_fences_and_finished_receipts(self):
        from stash_ingest.backfill_calls import BackfillCalls, advance_once
        from stash_ingest.client import Client
        from test_backfill_calls import specification, status, decision
        with closing(self.open()) as box:
            calls = BackfillCalls(box)
            spec = specification()
            done, pending = str(uuid.uuid4()), str(uuid.uuid4())
            calls.record(done, "a" * 64, lambda: spec)
            client = Client(box.endpoint, PRODUCER)
            with patch.object(client, "capabilities", return_value={"source_backfill_protocol": 1}), \
                    patch.object(client, "_request", return_value=status(spec, [decision(spec["component"])])):
                advance_once(calls, client, done)
            self.assertEqual(calls.result(done)["state"], "completed")
            calls.record(pending, "b" * 64, lambda: spec)
            calls.claim(str(uuid.uuid4()), pending)
            schema_six(box.db)
        tables = [row[0] for row in self.db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")]
        before = {name: self.db.execute('SELECT * FROM "' + name + '" ORDER BY rowid').fetchall() for name in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            self.assertEqual(box.db.execute("SELECT count(*) FROM legacy_n8n_receipts").fetchone()[0], 0)
            self.assertEqual(box.db.execute("SELECT count(*) FROM n8n_receipt_imports").fetchone()[0], 0)
        after = {name: self.db.execute('SELECT * FROM "' + name + '" ORDER BY rowid').fetchall() for name in tables}
        self.assertEqual(after, before)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_v6_unknown_receipt_table_rolls_back_all_new_objects(self):
        with closing(self.open()) as box:
            schema_six(box.db)
            box.db.execute("CREATE TABLE legacy_n8n_receipts(unrecognized TEXT)")
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        self.assertEqual(self.db.execute("PRAGMA user_version").fetchone()[0], 6)
        self.assertIsNone(self.db.execute("SELECT name FROM sqlite_schema WHERE name='n8n_receipt_imports'").fetchone())
        self.assertEqual(self.db.execute("SELECT * FROM events ORDER BY seq").fetchall(), self.before)


if __name__ == "__main__":
    unittest.main()
