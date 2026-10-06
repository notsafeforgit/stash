import uuid

from stash_ingest.encoding import encode
from stash_ingest.retention import POLICY, retain
from stash_archive.storage import InvalidArchive
from test_receipts import ReceiptFixture


class DownloadReceiptTests(ReceiptFixture):
    def captured(self):
        capture = {key: value for key, value in self.event().items()
                   if key not in ("relative_path", "size", "sha256", "media_kind")}
        capture.update(kind="source.capture", extractor_version="fixture", retention_policy=POLICY,
                       post={"namespace": "native:twitter", "value": "123"}, metadata={"title": "An album"},
                       source=retain({"category": "twitter", "tweet_id": "123", "content": "An album"}))
        self.accepted(capture)
        return capture

    def download(self, capture, state="started", **extra):
        event = {key: value for key, value in capture.items()
                 if key not in ("post", "metadata", "source", "extractor_version", "retention_policy")}
        event.update(event_uuid=str(uuid.uuid4()), kind="attachment.download", owner_uuid=str(uuid.uuid4()),
                     fence=1, transfer_sequence=1, capture_event_uuid=capture["event_uuid"],
                     attachment={"namespace": "native:twitter", "value": "456"}, state=state, **extra)
        return event

    def downloaded(self, *, acknowledge=True):
        capture = self.captured()
        started = self.download(capture)
        self.accepted(started)
        file = self.event()
        file["source"] = {"capture_event_uuid": capture["event_uuid"], "attachment": started["attachment"]}
        self.accepted(file)
        end = {**started, "event_uuid": str(uuid.uuid4()), "state": "downloaded", "file_event_uuid": file["event_uuid"]}
        self.accepted(end, acknowledge=acknowledge)
        return capture, file, end

    def test_acknowledged_downloads_retain_capture_file_and_report_without_payloads(self):
        capture, file, end = self.downloaded()
        before = self.box.db.execute("SELECT * FROM events ORDER BY seq").fetchall()
        proof = self.verify()
        self.assertEqual(proof["producers"][0]["counts"]["acknowledged"], 4)
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM events WHERE body IS NOT NULL").fetchone()[0], 0)
        self.assertEqual(before, self.box.db.execute("SELECT * FROM events ORDER BY seq").fetchall())
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])
        self.db.execute("DELETE FROM source_attachment_downloads WHERE event_uuid=?", (end["event_uuid"],))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "missing its report"):
            self.verify()

    def test_lost_response_keeps_exact_terminal_bytes_and_native_history(self):
        _, _, end = self.downloaded(acknowledge=False)
        proof = self.verify()["producers"][0]["counts"]
        self.assertEqual((proof["acknowledged"], proof["pending"], proof["accepted_unacknowledged"]), (3, 1, 1))
        self.assertEqual(self.box.db.execute("SELECT body FROM events WHERE event_uuid=?", (end["event_uuid"],)).fetchone()[0], encode(end))
        self.db.execute("UPDATE source_attachment_downloads SET fence=2 WHERE event_uuid=?", (end["event_uuid"],))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "differs from native history"):
            self.verify()

    def test_pending_report_can_precede_native_acceptance_but_cannot_lose_its_parent(self):
        capture = self.captured()
        event = self.download(capture, "skipped", reason_code="archive_entry_without_file")
        self.box.enqueue(encode(event))
        self.assertEqual(self.verify()["producers"][0]["counts"]["pending"], 1)
        self.box.db.execute("UPDATE events SET parent_uuid=NULL WHERE event_uuid=?", (event["event_uuid"],))
        with self.assertRaisesRegex(InvalidArchive, "capture-event association"):
            self.verify()

    def test_acknowledged_report_cannot_substitute_another_file_or_capture(self):
        _, _, end = self.downloaded()
        other_capture = self.captured()
        other_file = self.event()
        other_file["source"] = {"capture_event_uuid": other_capture["event_uuid"], "attachment": end["attachment"]}
        self.accepted(other_file)
        self.box.db.execute("UPDATE events SET parent_uuid=? WHERE event_uuid=?", (other_file["event_uuid"], end["event_uuid"]))
        with self.assertRaisesRegex(InvalidArchive, "queued parent"):
            self.verify()

    def test_all_nonfile_outcomes_survive_acknowledgement_and_snapshot_checks(self):
        for state, reason in (("failed", "download_failed"), ("excluded", "unsupported_media"),
                              ("skipped", "archive_entry_without_file")):
            with self.subTest(state=state):
                capture = self.captured()
                event = self.download(capture, state, reason_code=reason)
                self.accepted(event)
                self.verify()
        self.assertEqual(self.verify()["producers"][0]["counts"]["acknowledged"], 6)
        self.assertEqual(self.db.execute("SELECT count(*) FROM archive_jobs").fetchone()[0], 0)
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])
