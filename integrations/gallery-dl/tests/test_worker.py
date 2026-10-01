"""Worker ownership and completion, using the real gallery-dl download loop."""

import json
import os
from contextlib import closing
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch

from stash_ingest.client import Client, Unavailable
from stash_ingest.configuration import Configuration
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Capacity, Conflict, Outbox
from stash_ingest.runs import SourcePaused
from stash_ingest.worker import Delivery, execute
from helpers import PRODUCER, ROOT, RUN, capture, receipt
from test_configuration import profile_fixture
from test_gallery import Fixture
from test_producer import LeaseFixture, reddit_data


class WorkerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        profile, _ = profile_fixture(self.directory)
        self.profile = Configuration(profile)
        self.client = Client("http://example.invalid", PRODUCER, timeout=1)
        self.client.capabilities = Mock(return_value={"source_runs": True, "source_run_protocol": 1, "source_run_recovery_protocol": 1, "file_ingestion": True})
        self.lease = LeaseFixture()
        self.lease.client = self.client
        self.lease.run.update(policy_sha256=self.profile.policy_sha256, path_prefix="Account", fence=3)
        self.lease.run_uuid, self.lease.owner = RUN, ROOT
        self.lease.start, self.lease.close = Mock(), Mock()
        self.lease.finish = Mock(return_value={"uuid": RUN, "state": "succeeded"})
        self.client._request = Mock(return_value=dict(self.lease.run))
        self.box = Outbox(self.directory / "outbox.sqlite", self.client.endpoint, PRODUCER)
        self.addCleanup(self.box.close)
        self.delivery = Mock(failure=None)
        patches = [patch("stash_ingest.worker.RunLease.claim", return_value=self.lease),
                   patch("stash_ingest.worker.Delivery", return_value=self.delivery),
                   patch("requests.sessions.Session.request", side_effect=AssertionError("No website requests"))]
        self.claim, _, _ = [p.start() for p in patches]
        for p in patches:
            self.addCleanup(p.stop)
        self.downloaded = []

    def job(self, target, *, producer, lock_directory):
        extractor = Fixture.from_url(target)
        extractor.records = [reddit_data(), reddit_data("second")]
        extractor.visited = []
        task = NativeDownloadJob(extractor, producer=producer, lock_directory=lock_directory)

        def download(url):
            task.pathfmt.part_enable()
            with task.pathfmt.open("wb") as output:
                output.write(b"fixture media " + url.encode())
            self.downloaded.append(url)
            self.after_download()
            return True

        task.download = download
        return task

    def after_download(self):
        pass

    def run_worker(self, factory=None):
        with patch("stash_ingest.gallery.NativeDownloadJob", side_effect=factory or self.job):
            return execute(self.box, self.client, self.profile, RUN)

    def test_policy_capability_and_busy_lease_stop_before_downloads(self):
        for field, value in (("policy_sha256", "b" * 64), ("root_uuid", PRODUCER), ("operation", "enrich")):
            self.client._request.return_value = {**self.lease.run, field: value}
            with self.subTest(field=field), self.assertRaises(Conflict):
                self.run_worker()
        self.claim.assert_not_called()
        self.client._request.return_value = self.lease.run
        self.client.capabilities.return_value["file_ingestion"] = False
        with self.assertRaises(Unavailable):
            self.run_worker()
        self.claim.assert_not_called()
        self.client.capabilities.return_value["file_ingestion"] = True
        self.client.capabilities.return_value["source_run_recovery_protocol"] = 0
        with self.assertRaises(Unavailable):
            self.run_worker()
        self.claim.assert_not_called()
        self.client.capabilities.return_value["source_run_recovery_protocol"] = 1
        self.claim.return_value = None
        self.assertEqual(self.run_worker()["state"], "waiting")
        self.delivery.start.assert_not_called()
        self.assertEqual(self.downloaded, [])

    def test_success_preserves_queued_files_without_claiming_intake_completion(self):
        result = self.run_worker()
        self.assertEqual(result["state"], "source_succeeded")
        self.assertEqual(result["intake_completion"], "inspect_native_receipts")
        self.assertEqual(result["outbox"]["counts"]["pending"], 4)
        self.lease.finish.assert_called_once_with("succeeded", error_code="")
        events = [decode(row[0]) for row in self.box.db.execute("SELECT body FROM events ORDER BY seq")]
        self.assertEqual([e["kind"] for e in events], ["source.capture", "file.completed"] * 2)
        for event in events[1::2]:
            self.assertTrue((self.profile.root.path / event["relative_path"]).is_file())
        self.delivery.close.assert_called_once()
        self.lease.close.assert_called_once()

    def test_scoped_profile_refuses_a_different_root_extractor_before_download(self):
        path = self.directory / "worker.json"
        value = json.loads(path.read_text())
        value["source_category"] = "twitter"
        self.profile = Configuration.from_document(value, self.directory)
        self.lease.run["policy_sha256"] = self.profile.policy_sha256
        self.client._request.return_value = self.lease.run
        result = self.run_worker()
        self.assertEqual(result["state"], "deferred")
        self.assertEqual(self.downloaded, [])
        self.assertEqual(result["outbox"]["counts"]["pending"], 0)

    def test_current_file_survives_lost_lease_without_finishing_attempt(self):
        self.after_download = lambda: setattr(self.lease, "active", False)
        result = self.run_worker()
        self.assertEqual(result["state"], "paused")
        self.assertEqual(len(self.downloaded), 1)
        self.assertEqual(result["outbox"]["counts"]["pending"], 2)
        self.lease.finish.assert_not_called()
        self.delivery.close.assert_called_once()

    def test_changed_asset_defers_further_source_work(self):
        self.after_download = lambda: (self.directory / "converter.py").write_text("def prepare(data): return 'changed'\n")
        result = self.run_worker()
        self.assertEqual(result["state"], "deferred")
        self.assertEqual(len(self.downloaded), 1)
        self.assertEqual(result["outbox"]["counts"]["pending"], 2)
        self.lease.finish.assert_called_once_with("deferred", error_code="worker_configuration_or_source")

    def test_download_access_storage_and_interruption_failures_are_not_success(self):
        for failure, state, code in ((4, "retry", "source_download_failed"),
                                     (16, "deferred", "source_access_or_configuration"),
                                     (OSError(), "retry", "worker_storage_unavailable"),
                                     (Capacity("full"), "retry", "outbox_capacity"),
                                     (InvalidData("configuration"), "deferred", "worker_configuration_or_source"),
                                     (KeyboardInterrupt(), "retry", "worker_interrupted"),
                                     (RuntimeError("unexpected"), "retry", "worker_execution_failed")):
            with self.subTest(failure=failure):
                task = Mock()
                if isinstance(failure, BaseException):
                    task.run.side_effect = failure
                else:
                    task.run.return_value = failure
                self.lease.finish.reset_mock()
                self.lease.finish.return_value = {"state": "queued" if state == "retry" else state}
                result = self.run_worker(lambda *a, **kw: task)
                self.assertEqual(result["state"], state)
                self.lease.finish.assert_called_once_with(state, error_code=code)

    def test_lost_finish_requires_the_exact_completed_attempt(self):
        attempt = {"run_uuid": RUN, "producer_uuid": PRODUCER, "owner_uuid": self.lease.owner,
                   "fence": 3, "outcome": "succeeded", "ended_at": "2026-10-01T12:00:00Z"}
        alternatives = [attempt, {**attempt, "owner_uuid": PRODUCER}, {**attempt, "fence": 2},
                        {**attempt, "outcome": "expired"}, {**attempt, "ended_at": None}, None]
        self.lease.finish.side_effect = Unavailable("network_unavailable")
        for expected in alternatives:
            with self.subTest(attempt=expected):
                def request(method, route, body=None):
                    if route.endswith("/attempts"):
                        self.assertEqual(decode(body), {"after": 2})
                        if expected is None:
                            raise Unavailable("network_unavailable")
                        return [expected]
                    return self.lease.run
                self.client._request.side_effect = request
                result = self.run_worker(lambda *a, **kw: Mock(run=Mock(return_value=0)))
                confirmed = expected == attempt
                self.assertEqual(result["finish_recovered"], confirmed)
                self.assertEqual(result["state"], "source_succeeded" if confirmed else "completion_unconfirmed")


class DeliveryTests(unittest.TestCase):
    def test_thread_owns_connection_and_outage_leaves_durable_retry(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "outbox.sqlite"
            client = Client("http://example.invalid", PRODUCER, timeout=1)
            with closing(Outbox(path, client.endpoint, PRODUCER)) as box:
                event = capture()
                box.enqueue(encode(event))
                done = threading.Event()
                failures = []

                def offline(thread_box, thread_client):
                    try:
                        self.assertIsNot(thread_box, box)
                        deliveries = thread_box.claim(ROOT)
                        self.assertEqual(len(deliveries), 1)
                        thread_box.fail(deliveries[0], "network_unavailable")
                    except Exception as exc:
                        failures.append(exc)
                    finally:
                        done.set()
                    return {"acknowledged": 0}

                with patch("stash_ingest.worker.drain_once", side_effect=offline):
                    delivery = Delivery(box, client)
                    delivery.start()
                    self.assertTrue(done.wait(5))
                    delivery.close()
                    delivery.check()
                    self.assertFalse(delivery.thread.is_alive())
                self.assertEqual(failures, [])
                self.assertEqual(box.status()["counts"]["pending"], 1)
            with closing(Outbox(path, client.endpoint, PRODUCER, clock=lambda: 10**12)) as restarted:
                queued = restarted.claim(ROOT)
                self.assertEqual(len(queued), 1)
                self.assertEqual(queued[0].body, encode(event))
                restarted.acknowledge(queued[0], receipt(event))
                self.assertEqual(restarted.status()["counts"]["acknowledged"], 1)

    def test_crashed_drainer_stops_further_source_work(self):
        with tempfile.TemporaryDirectory() as directory:
            client = Client("http://example.invalid", PRODUCER, timeout=1)
            with closing(Outbox(Path(directory) / "outbox.sqlite", client.endpoint, PRODUCER)) as box:
                with patch("stash_ingest.worker.drain_once", side_effect=RuntimeError("private response")):
                    delivery = Delivery(box, client)
                    delivery.start()
                    delivery.thread.join(5)
                    delivery.close()
                    with self.assertRaises(InvalidData) as failure:
                        delivery.check()
                    self.assertNotIn("private response", str(failure.exception))

    def test_worker_logs_and_child_output_do_not_corrupt_json_stdout(self):
        script = """import json, os, subprocess, sys
from stash_ingest.cli import worker_output
with worker_output():
    print('python log')
    os.write(1, b'descriptor log\\n')
    subprocess.run([sys.executable, '-c', "print('child log')"], check=True)
print(json.dumps({'state': 'fixture'}))
"""
        result = subprocess.run([sys.executable, "-c", script], capture_output=True, text=True, check=True, env=os.environ)
        self.assertEqual(json.loads(result.stdout), {"state": "fixture"})
        self.assertTrue(all(text in result.stderr for text in ("python log", "descriptor log", "child log")))


if __name__ == "__main__":
    unittest.main()
