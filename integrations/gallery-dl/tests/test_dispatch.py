"""Restart and contention behavior of the bounded source dispatcher."""

from contextlib import closing
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Client, Unavailable
from stash_ingest.dispatch import Dispatcher, dispatch_once
from stash_ingest.encoding import decode
from stash_ingest.outbox import Conflict, Outbox
from helpers import PRODUCER, ROOT


def candidates(start, count):
    return [{"sequence": index, "uuid": str(uuid.uuid5(uuid.NAMESPACE_URL, f"dispatch-fixture/{index}"))}
            for index in range(start, start + count)]


class DispatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "producer.sqlite"
        self.now = 1000
        self.client = Client("http://fixture.invalid", PRODUCER)
        self.client.capabilities = Mock(return_value={"source_runs": True, "source_run_protocol": 1,
                                                       "source_run_dispatch": True, "file_ingestion": True})
        self.client._request = Mock(return_value=[])
        self.profile = SimpleNamespace(root_uuid=ROOT, policy_sha256="a" * 64, check=Mock())
        self.executor = patch("stash_ingest.dispatch.execute", return_value={"state": "waiting"}).start()
        self.addCleanup(patch.stopall)

    def open(self):
        return Outbox(self.path, self.client.endpoint, PRODUCER, clock=lambda: self.now)

    def test_busy_page_cannot_starve_later_work_across_process_restarts(self):
        with closing(self.open()) as box:
            worker = Dispatcher(box, self.client, self.profile)
            self.client._request.return_value = candidates(1, 50)
            self.assertEqual(worker.once()["after_sequence"], 50)
            self.assertEqual(self.executor.call_count, 50)
        with closing(self.open()) as box:
            worker = Dispatcher(box, self.client, self.profile)
            self.client._request.return_value = candidates(51, 1)
            self.executor.return_value = {"state": "source_succeeded", "run_state": "queued"}
            self.assertEqual(worker.once()["run_state"], "queued")
            self.assertEqual(decode(self.client._request.call_args.args[2])["after"], 50)
            self.assertEqual(worker.state()["after_sequence"], 51)
            self.client._request.return_value = []
            self.assertEqual(worker.once()["state"], "idle")
            self.assertEqual(worker.state()["after_sequence"], 0)
            # A previously busy run remains eligible on the next traversal.
            self.client._request.return_value = candidates(1, 1)
            self.assertEqual(worker.once()["state"], "source_succeeded")
            self.assertEqual(decode(self.client._request.call_args.args[2])["after"], 0)

    def test_crash_after_selection_keeps_cursor_without_asserting_completion(self):
        self.client._request.return_value = candidates(1, 2)
        self.executor.side_effect = RuntimeError("process disappeared")
        with closing(self.open()) as box:
            with self.assertRaises(RuntimeError):
                Dispatcher(box, self.client, self.profile).once()
        with closing(self.open()) as box:
            worker = Dispatcher(box, self.client, self.profile)
            self.assertEqual(worker.state()["after_sequence"], 1)
            self.assertEqual(box.db.execute("SELECT count(*) FROM run_requests").fetchone()[0], 0)
            self.assertEqual(box.status()["counts"]["acknowledged"], 0)

    def test_outage_backoff_survives_restart_and_honors_retry_after(self):
        self.client.capabilities.side_effect = Unavailable("queue_full", 429, 120)
        with closing(self.open()) as box:
            result = Dispatcher(box, self.client, self.profile).once()
            self.assertEqual(result["next_attempt_at"], 1120)
        self.now = 1119
        with closing(self.open()) as box:
            self.assertEqual(Dispatcher(box, self.client, self.profile).once()["state"], "backoff")
        self.client.capabilities.assert_called_once()
        self.now = 1120
        with closing(self.open()) as box:
            result = Dispatcher(box, self.client, self.profile).once()
            self.assertEqual(result["state"], "unavailable")
            self.assertEqual(result["next_attempt_at"], 1240)
        self.assertEqual(self.client.capabilities.call_count, 2)
        self.executor.assert_not_called()

    def test_malformed_or_unsupported_discovery_never_starts_any_candidate(self):
        good = candidates(1, 2)
        pages = [{}, good[::-1], [good[0], {**good[1], "uuid": good[0]["uuid"]}],
                 [{**good[0], "sequence": True}], [{**good[0], "secret": "unexpected"}], candidates(1, 51)]
        with closing(self.open()) as box:
            worker = Dispatcher(box, self.client, self.profile)
            for page in pages:
                self.client._request.return_value = page
                result = worker.once()
                self.assertEqual(result["state"], "unavailable")
                self.assertEqual(worker.state()["after_sequence"], 0)
                self.now = result["next_attempt_at"]
            self.client.capabilities.return_value["source_run_dispatch"] = False
            result = worker.once()
            self.assertEqual(result["error_code"], "native_dispatch_unavailable")
        self.executor.assert_not_called()

    def test_competing_cursor_update_prevents_duplicate_local_selection(self):
        with closing(self.open()) as box, closing(self.open()) as other:
            worker, competing = (Dispatcher(db, self.client, self.profile) for db in (box, other))

            def reply(*_):
                self.assertTrue(competing.save(competing.state(), 1))
                return candidates(1, 1)

            self.client._request.side_effect = reply
            self.assertEqual(worker.once()["state"], "contended")
            self.assertEqual(worker.state()["after_sequence"], 1)
        self.executor.assert_not_called()

    def test_delivery_and_admission_continue_during_discovery_backoff(self):
        self.client.capabilities.side_effect = Unavailable("network_unavailable")
        with closing(self.open()) as box:
            Dispatcher(box, self.client, self.profile).once()
            with patch("stash_ingest.dispatch.drain_once", return_value={"acknowledged": 1}) as delivery, \
                    patch("stash_ingest.dispatch.submit_once", return_value={"state": "admitted"}) as admission:
                result = dispatch_once(box, self.client, self.profile)
            delivery.assert_called_once()
            admission.assert_called_once()
            self.assertEqual(result["state"], "backoff")
            self.assertEqual(result["delivery"], {"acknowledged": 1})
            self.assertEqual(result["intake_completion"], "inspect_native_receipts")

    def test_policies_have_separate_cursors_and_wrong_producer_cannot_dispatch(self):
        with closing(self.open()) as box:
            worker = Dispatcher(box, self.client, self.profile)
            self.assertTrue(worker.save(worker.state(), 50))
            profile = SimpleNamespace(root_uuid=ROOT, policy_sha256="b" * 64)
            self.assertEqual(Dispatcher(box, self.client, profile).state()["after_sequence"], 0)
            self.assertEqual(worker.state()["after_sequence"], 50)
            self.client.producer = str(uuid.uuid4())
            with self.assertRaises(Conflict):
                dispatch_once(box, self.client, self.profile)
        self.client.capabilities.assert_not_called()


if __name__ == "__main__":
    unittest.main()
