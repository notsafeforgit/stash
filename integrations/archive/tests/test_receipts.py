"""Exercise the actual producer outbox against the native receipt table DDL."""

from contextlib import closing, ExitStack, redirect_stdout
import hashlib
import io
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
import uuid

from stash_ingest.encoding import encode
from stash_ingest.outbox import Outbox
from stash_ingest.retention import POLICY, retain
from stash_archive.bundle import FORMAT, connect_readonly, export_archive, snapshot_database
from stash_archive.cli import main
from stash_archive.receipts import verify_ingestion_receipts
from stash_archive.storage import InvalidArchive

ROOT = Path(os.environ.get("STASH_SOURCE_ROOT", Path(__file__).resolve().parents[3]))
PRODUCER, COLLECTION, MEDIA_ROOT, RUN, CREDENTIAL = [str(uuid.uuid4()) for _ in range(5)]
ORIGIN = "https://stash.example"


class ReceiptFixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.library = self.root / "library.sqlite"
        self.db = sqlite3.connect(self.library)
        self.addCleanup(self.db.close)
        self.db.executescript("""
            CREATE TABLE native_schema(singleton INTEGER PRIMARY KEY,lineage TEXT);
            CREATE TABLE ingest_producers(uuid TEXT PRIMARY KEY);
            CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,dirty INTEGER);
            INSERT INTO schema_migrations VALUES(1000087,0);
            CREATE TABLE blobs(checksum TEXT PRIMARY KEY,blob BLOB);
            CREATE TABLE media_roots(uuid TEXT PRIMARY KEY);
            CREATE TABLE ingest_credentials(producer_uuid TEXT,uuid TEXT,PRIMARY KEY(producer_uuid,uuid));
            CREATE TABLE source_collection_revisions(collection_uuid TEXT,revision INTEGER,PRIMARY KEY(collection_uuid,revision));
            CREATE TABLE ingest_credential_scopes(credential_uuid TEXT,collection_uuid TEXT,PRIMARY KEY(credential_uuid,collection_uuid));
            CREATE TABLE source_posts(uuid TEXT PRIMARY KEY);
            CREATE TABLE source_captures(post_uuid TEXT,uuid TEXT UNIQUE,PRIMARY KEY(post_uuid,uuid));
            CREATE TABLE archive_jobs(uuid TEXT PRIMARY KEY);
            CREATE TABLE source_attachments(uuid TEXT PRIMARY KEY);
            CREATE TABLE source_run_attempts(run_uuid TEXT,fence INTEGER,PRIMARY KEY(run_uuid,fence));
        """)
        self.db.execute("INSERT INTO native_schema VALUES(1,?)", (FORMAT,))
        self.db.execute("INSERT INTO ingest_producers VALUES(?)", (PRODUCER,))
        self.db.execute("INSERT INTO media_roots VALUES(?)", (MEDIA_ROOT,))
        self.db.execute("INSERT INTO ingest_credentials VALUES(?,?)", (PRODUCER,CREDENTIAL))
        self.db.execute("INSERT INTO source_collection_revisions VALUES(?,1)", (COLLECTION,))
        self.db.execute("INSERT INTO ingest_credential_scopes VALUES(?,?)", (CREDENTIAL,COLLECTION))
        migration = (ROOT / 'pkg/sqlite/migrations/1000087_attachment_downloads.up.sql').read_text()
        receipt_table = migration.split('CREATE TABLE ingest_receipts_next (', 1)[1].split('\n);', 1)[0]
        # Use the actual receipt DDL; the supporting domain rows provide its
        # foreign-key targets. Normal native model behavior is tested in Go.
        self.db.executescript('CREATE TABLE ingest_receipts (' + receipt_table + '\n);')
        download_table = migration.split('CREATE TABLE source_attachment_downloads (', 1)[1].split('\n);', 1)[0]
        self.db.executescript('CREATE TABLE source_attachment_downloads (' + download_table + '\n);')
        self.attachments = {}
        self.db.commit()
        self.outbox = self.root / "producer.sqlite"
        self.box = Outbox(self.outbox, ORIGIN, PRODUCER)
        self.addCleanup(self.box.close)

    def event(self):
        return {"protocol": 1, "producer_uuid": PRODUCER, "event_uuid": str(uuid.uuid4()),
                "collection_uuid": COLLECTION, "collection_revision": 1,
                "root_uuid": MEDIA_ROOT, "run_uuid": RUN, "kind": "file.completed",
                "observed_at": "2026-10-04T12:00:00Z", "relative_path": "manual/purchase.mp4",
                "size": 12345, "sha256": "a" * 64, "media_kind": "scene"}

    def accepted(self, event, *, acknowledge=True):
        self.box.enqueue(encode(event))
        delivery, = self.box.claim(str(uuid.uuid4()))
        receipt = {key: event[key] for key in ("producer_uuid", "event_uuid", "collection_uuid",
                                             "collection_revision", "root_uuid", "run_uuid", "kind")}
        receipt.update(sha256=hashlib.sha256(encode(event)).hexdigest(), credential_uuid=CREDENTIAL,
                       result={"status": "queued"},
                       committed_at="2026-10-04T12:00:00+00:00")
        if event["kind"] == "source.capture":
            receipt.update(post_uuid=str(uuid.uuid4()), capture_uuid=str(uuid.uuid4()), result={"status":"committed"})
            self.db.execute("INSERT INTO source_posts VALUES(?)", (receipt["post_uuid"],))
            self.db.execute("INSERT INTO source_captures VALUES(?,?)", (receipt["post_uuid"],receipt["capture_uuid"]))
        elif event["kind"] == "attachment.download":
            capture = self.box.receipt(event["capture_event_uuid"])
            attachment = self.attachments.setdefault(json.dumps(event["attachment"], sort_keys=True), str(uuid.uuid4()))
            self.db.execute("INSERT OR IGNORE INTO source_attachments VALUES(?)", (attachment,))
            self.db.execute("INSERT OR IGNORE INTO source_run_attempts VALUES(?,?)", (event["run_uuid"], event["fence"]))
            receipt.update(post_uuid=capture["post_uuid"], capture_uuid=capture["capture_uuid"],
                           result={"status": "recorded", "reported_state": event["state"],
                                   "attachment_uuid": attachment, "media_ingested": False})
            self.db.execute("""INSERT INTO source_attachment_downloads(producer_uuid,event_uuid,run_uuid,
                fence,owner_uuid,transfer_sequence,capture_event_uuid,attachment_uuid,state,phase,
                file_event_uuid,reason_code,observed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)""",
                tuple(event[key] for key in ("producer_uuid", "event_uuid", "run_uuid", "fence", "owner_uuid",
                                            "transfer_sequence", "capture_event_uuid")) +
                (attachment, event["state"], int(event["state"] != "started"), event.get("file_event_uuid"),
                 event.get("reason_code", ""), event["observed_at"]))
        else:
            receipt["job_uuid"] = str(uuid.uuid4())
            self.db.execute("INSERT INTO archive_jobs VALUES(?)", (receipt["job_uuid"],))
            if event.get("source"):
                capture = self.box.receipt(event["source"]["capture_event_uuid"])
                receipt.update(post_uuid=capture["post_uuid"], capture_uuid=capture["capture_uuid"])
        self.db.execute("""INSERT INTO ingest_receipts(producer_uuid,event_uuid,digest,credential_uuid,
            collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,job_uuid,result,committed_at)
            VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)""", tuple(receipt.get(k) for k in
            ("producer_uuid", "event_uuid", "sha256", "credential_uuid", "collection_uuid", "collection_revision",
             "root_uuid", "run_uuid", "kind", "post_uuid", "capture_uuid", "job_uuid")) + (json.dumps(receipt["result"]), "2026-10-04 12:00:00"))
        self.db.commit()
        if acknowledge:
            self.box.acknowledge(delivery, receipt)
        else:
            self.box.fail(delivery, "network_unavailable")
        return receipt

    def verify(self, *, library=None, outbox=None, expected_origin=ORIGIN, copies=1):
        with ExitStack() as stack:
            native = stack.enter_context(closing(connect_readonly(library or self.library)))
            native.execute("BEGIN")
            queues = []
            for _ in range(copies):
                queue = stack.enter_context(closing(connect_readonly(outbox or self.outbox)))
                queue.execute("BEGIN")
                queues.append(queue)
            return verify_ingestion_receipts(native, queues, expected_origin)


class ReceiptBoundaryTests(ReceiptFixture):
    def test_matching_acknowledged_pending_and_lost_response_are_preserved(self):
        self.accepted(self.event())
        self.accepted(self.event(), acknowledge=False)
        self.box.enqueue(encode(self.event()))
        before = self.box.db.execute("SELECT * FROM events ORDER BY seq").fetchall()
        report = self.verify()
        self.assertEqual(report["coverage"], "capture-file-download-run-and-job-receipts")
        self.assertEqual(report["producers"][0]["counts"], {"acknowledged": 1, "pending": 2, "sending": 0,
                                                         "review": 0, "accepted_unacknowledged": 1})
        self.assertEqual(report, self.verify())
        self.assertEqual(before, self.box.db.execute("SELECT * FROM events ORDER BY seq").fetchall())

    def test_old_native_snapshot_cannot_cover_new_acknowledgement(self):
        old = self.root / "old-native.sqlite"
        with closing(sqlite3.connect(old)) as target:
            self.db.backup(target)
        self.accepted(self.event())
        with self.assertRaisesRegex(InvalidArchive, "missing from the native snapshot"):
            self.verify(library=old)
        self.verify()

    def test_earlier_queue_and_later_native_snapshot_retain_replay_payload(self):
        event = self.event()
        self.box.enqueue(encode(event))
        old = self.root / "old-producer.sqlite"
        snapshot_database(self.outbox, old, "producer_outbox", reserve=0)
        self.accepted(event)
        counts = self.verify(outbox=old)["producers"][0]["counts"]
        self.assertEqual(counts["pending"], 1)
        self.assertEqual(counts["accepted_unacknowledged"], 1)
        with closing(sqlite3.connect(old)) as retained:
            self.assertEqual(retained.execute("SELECT body FROM events").fetchone()[0], encode(event))

    def test_conflicting_receipt_rejected_even_with_pending_body(self):
        event = self.event()
        self.accepted(event, acknowledge=False)
        self.db.execute("UPDATE ingest_receipts SET digest=?", ("b" * 64,))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "does not match"):
            self.verify()

    def test_altered_result_commit_time_and_credential_are_rejected(self):
        self.accepted(self.event())
        for key, value in (("credential_uuid", str(uuid.uuid4())), ("result", '{"status":"complete"}'),
                           ("committed_at", "2026-10-04 12:00:01"), ("job_uuid", str(uuid.uuid4()))):
            with self.subTest(key=key):
                self.db.execute("BEGIN")
                old = self.db.execute(f'SELECT {key} FROM ingest_receipts').fetchone()[0]
                self.db.execute(f'UPDATE ingest_receipts SET {key}=?', (value,))
                self.db.commit()
                with self.assertRaises(InvalidArchive): self.verify()
                self.db.execute(f'UPDATE ingest_receipts SET {key}=?', (old,))
                self.db.commit()
        self.assertEqual(self.verify()["producers"][0]["counts"]["acknowledged"], 1)

    def test_missing_corrupt_payload_and_foreign_or_duplicate_producer_rejected(self):
        event = self.event()
        self.box.enqueue(encode(event))
        self.box.db.execute("UPDATE events SET body=?", (b'{}',))
        with self.assertRaisesRegex(InvalidArchive, "payload or identity"): self.verify()
        self.box.db.execute("UPDATE events SET body=?", (encode(event),))
        with self.assertRaisesRegex(InvalidArchive, "another origin"): self.verify(expected_origin="https://other.example")
        with self.assertRaisesRegex(InvalidArchive, "Duplicate producer"): self.verify(copies=2)
        self.db.execute("DELETE FROM ingest_producers")
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "identity is missing"): self.verify()

    def test_future_outbox_version_and_writable_transaction_refused(self):
        self.box.db.execute("PRAGMA user_version=17")
        with self.assertRaisesRegex(InvalidArchive, "Unsupported producer"): self.verify()
        self.db.execute("BEGIN")
        try:
            with self.assertRaisesRegex(InvalidArchive, "read-only"):
                verify_ingestion_receipts(self.db, [self.box.db], ORIGIN)
        finally:
            self.db.rollback()

    def test_registered_producer_cannot_be_silently_omitted(self):
        self.db.execute("INSERT INTO ingest_producers VALUES(?)", (str(uuid.uuid4()),))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "no matching outbox"):
            self.verify()

    def test_capture_receipt_and_dependent_pending_file_keep_their_association(self):
        capture = {key:value for key,value in self.event().items()
                   if key not in ("relative_path","size","sha256","media_kind")}
        capture.update(kind="source.capture", extractor_version="fixture", retention_policy=POLICY,
                       post={"namespace":"native:twitter","value":"123"}, metadata={"title":"An album"},
                       source=retain({"category":"twitter","tweet_id":"123","content":"An album"}))
        self.accepted(capture)
        file = self.event()
        file["source"] = {"capture_event_uuid":capture["event_uuid"],
                          "attachment":{"namespace":"native:twitter","value":"456"}}
        self.box.enqueue(encode(file))
        counts = self.verify()["producers"][0]["counts"]
        self.assertEqual((counts["acknowledged"],counts["pending"]), (1,1))
        self.box.db.execute("UPDATE events SET parent_uuid=NULL WHERE event_uuid=?", (file["event_uuid"],))
        with self.assertRaisesRegex(InvalidArchive, "capture-event association"):
            self.verify()

    def test_sending_and_review_payloads_are_never_reclassified(self):
        for _ in range(3):
            self.box.enqueue(encode(self.event()))
        deliveries = self.box.claim(str(uuid.uuid4()))
        self.assertEqual(len(deliveries), 3)
        self.box.fail(deliveries[0], "network_unavailable")
        self.box.fail(deliveries[1], "conflicting_receipt", review=True)
        before = self.box.db.execute("SELECT * FROM events ORDER BY seq").fetchall()
        counts = self.verify()["producers"][0]["counts"]
        self.assertEqual(counts, {"acknowledged":0,"pending":1,"sending":1,"review":1,"accepted_unacknowledged":0})
        self.assertEqual(before, self.box.db.execute("SELECT * FROM events ORDER BY seq").fetchall())

    def test_cli_proof_is_bound_to_verified_archive_components(self):
        from test_verification import contract_validator
        self.accepted(self.event())
        self.box.enqueue(encode(self.event()))
        archive = self.root / "archive"
        manifest = export_archive(self.library, archive, components=[
            {"role":"producer_outbox", "name":"worker.sqlite", "path":self.outbox}], reserve=0)
        output = io.StringIO()
        with redirect_stdout(output):
            main(["verify", str(archive), "--producer-origin", ORIGIN,
                  "--native-validator", str(contract_validator(self.root)),
                  "--temp-parent", str(self.root), "--reserve-bytes", "0"])
        value = json.loads(output.getvalue())
        self.assertTrue(value["contents_verified"])
        proof = value["ingestion_receipts"]
        self.assertEqual(proof["archive_uuid"], manifest["uuid"])
        self.assertTrue(proof["registered_producers_complete"])
        self.assertEqual({e["role"] for e in proof["components"]}, {"library", "producer_outbox"})
        self.assertEqual(proof["producers"][0]["counts"]["pending"], 1)
        self.assertEqual(value["native_snapshot"]["sha256"],
                         next(e["sha256"] for e in proof["components"] if e["role"] == "library"))
        self.assertEqual(list(self.root.glob('stash-archive-receipts-*')), [])
        self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])


if __name__ == "__main__":
    unittest.main()
