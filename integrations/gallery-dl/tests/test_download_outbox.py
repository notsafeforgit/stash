from copy import deepcopy
from pathlib import Path
import tempfile
import unittest
import uuid

from stash_ingest import events
from stash_ingest.client import drain_once
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.outbox import Conflict, Outbox
from helpers import PRODUCER, capture, file_event, receipt


def download(parent, state="started", **changes):
    item = {key: parent[key] for key in events.COMMON}
    item.update(kind="attachment.download", event_uuid=str(uuid.uuid4()),
                capture_event_uuid=parent["event_uuid"], owner_uuid=str(uuid.uuid4()),
                fence=1, transfer_sequence=1, state=state,
                attachment={"namespace": "native:twitter", "value": "101"})
    item.update(changes)
    return item


def download_receipt(event, parent_receipt):
    ack = receipt(event)
    del ack["job_uuid"]
    ack.update(capture_uuid=parent_receipt["capture_uuid"], post_uuid=parent_receipt["post_uuid"],
               result={"status": "recorded", "reported_state": event["state"],
                       "attachment_uuid": str(uuid.uuid4()), "media_ingested": False})
    return ack


class DownloadOutboxTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.path = Path(temp.name) / "queue.sqlite"
        self.now = [1000.0]
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())

    def open(self):
        return Outbox(self.path, "http://localhost:8009", PRODUCER, clock=lambda: self.now[0])

    def claim(self):
        return self.box.claim(str(uuid.uuid4()))

    def test_capture_and_file_dependencies_survive_release_and_restart(self):
        parent = capture()
        start = download(parent)
        file = file_event(parent)
        end = download(parent, "downloaded", file_event_uuid=file["event_uuid"], owner_uuid=start["owner_uuid"])
        for event in (parent, start, file, end):
            self.box.enqueue(encode(event))
        first, = self.claim()
        self.assertEqual(first.event_uuid, parent["event_uuid"])
        parent_receipt = receipt(parent)
        self.box.acknowledge(first, parent_receipt)
        eligible = {item.event_uuid: item for item in self.claim()}
        self.assertEqual(set(eligible), {start["event_uuid"], file["event_uuid"]})
        self.box.acknowledge(eligible[start["event_uuid"]], download_receipt(start, parent_receipt))
        self.box.acknowledge(eligible[file["event_uuid"]], receipt(file))
        self.box.close()
        self.box = self.open()
        final, = self.claim()
        self.assertEqual(final.body, encode(end))
        self.box.acknowledge(final, download_receipt(end, parent_receipt))
        self.assertEqual(self.box.status()["counts"]["acknowledged"], 4)
        self.assertEqual(self.box.status()["queued_bytes"], 0)

    def test_download_rejects_foreign_capture_and_run_dependencies(self):
        parent, other = capture(), capture()
        file = file_event(other)
        for event in (parent, other, file):
            self.box.enqueue(encode(event))
        for bad in (download(parent, "downloaded", file_event_uuid=file["event_uuid"]),
                    download(parent, run_uuid=str(uuid.uuid4())),
                    download(parent, capture_event_uuid=str(uuid.uuid4()))):
            with self.assertRaises(Conflict):
                self.box.enqueue(encode(bad))
        self.assertEqual(self.box.status()["counts"]["pending"], 3)

    def test_report_receipt_cannot_claim_import_or_another_capture(self):
        parent = capture()
        event = download(parent, "excluded", reason_code="unsupported_media")
        self.box.enqueue(encode(parent))
        self.box.enqueue(encode(event))
        parent_receipt = receipt(parent)
        self.box.acknowledge(self.claim()[0], parent_receipt)
        selected, = self.claim()
        original = download_receipt(event, parent_receipt)
        for field, value in (("reported_state", "downloaded"), ("media_ingested", True),
                             ("media_ingested", 0), ("status", "queued")):
            bad = deepcopy(original)
            bad["result"][field] = value
            with self.assertRaises(Conflict):
                self.box.acknowledge(selected, bad)
        with self.assertRaises(Conflict):
            self.box.acknowledge(selected, dict(original, capture_uuid=str(uuid.uuid4())))
        with self.assertRaises(Conflict):
            self.box.acknowledge(selected, dict(original, job_uuid=str(uuid.uuid4())))
        self.assertIsNotNone(self.box.db.execute("SELECT body FROM events WHERE event_uuid=?", (event["event_uuid"],)).fetchone()[0])
        self.box.acknowledge(selected, original)

    def test_delivery_uses_recorded_status_and_recovers_lost_response(self):
        parent = capture()
        start = download(parent)
        self.box.enqueue(encode(parent))
        self.box.enqueue(encode(start))
        parent_receipt = receipt(parent)
        self.box.acknowledge(self.claim()[0], parent_receipt)
        selected, = self.claim()
        self.now[0] += 61  # another drainer recovers the same immutable bytes
        expected = download_receipt(start, parent_receipt)

        class Client:
            endpoint = "http://localhost:8009"
            producer = PRODUCER

            def capabilities(self):
                return {"file_ingestion": False, "attachment_download_protocol": 1}

            def batch(self, items):
                self.seen = items
                return [{"index": 0, "status": 200, "receipt": expected}]

        client = Client()
        result = drain_once(self.box, client)
        self.assertEqual(result["acknowledged"], 1)
        self.assertEqual(client.seen[0].body, selected.body)
        self.assertEqual(self.box.receipt(start["event_uuid"]), expected)

    def test_version_fifteen_queue_migrates_without_losing_pending_capture(self):
        parent = capture()
        self.box.enqueue(encode(parent))
        before = tuple(self.box.db.execute("SELECT * FROM events").fetchone())
        self.box.db.execute("PRAGMA user_version=15")
        self.box.close()
        self.box = self.open()
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], 16)
        self.assertEqual(tuple(self.box.db.execute("SELECT * FROM events").fetchone()), before)

    def test_typed_bounded_reports_reject_unverifiable_states_and_secrets(self):
        parent = capture()
        for state, reason in (("started", ""), ("downloaded", ""), ("failed", "download_failed"),
                              ("excluded", "unsupported_media"), ("skipped", "archive_entry_without_file")):
            item = download(parent, state, reason_code=reason)
            if state == "downloaded":
                item["file_event_uuid"] = str(uuid.uuid4())
            self.assertEqual(events.validate(encode(item)), item)
        for changes in ({"state": "available"}, {"state": "interrupted"}, {"state": []},
                        {"fence": True}, {"transfer_sequence": 2**53}, {"root_uuid": None},
                        {"password": "secret"}, {"state": "failed", "reason_code": "raw URL or error"},
                        {"state": "downloaded"}, {"file_event_uuid": str(uuid.uuid4())},
                        {"attachment": {"namespace": "native:twitter", "value": "é"*513}},
                        {"attachment": {"namespace": "native:twitter", "value": "id\u007f"}}):
            with self.subTest(changes=changes), self.assertRaises(InvalidData):
                self.box.enqueue(encode(download(parent, **changes)))
        self.assertEqual(self.box.status()["counts"]["pending"], 0)


if __name__ == "__main__":
    unittest.main()
