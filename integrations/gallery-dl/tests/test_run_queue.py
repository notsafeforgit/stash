from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.outbox import Capacity, Conflict, LeaseLost, Outbox
from stash_ingest.run_queue import RunQueue, submit_once
from helpers import COLLECTION, PRODUCER, ROOT, RUN


def window(since, until):
    def stamp(day):
        return (datetime(2026, 1, 1, tzinfo=timezone.utc) + timedelta(days=day)).isoformat(timespec="milliseconds").replace("+00:00", "Z")
    return {"since": None if since is None else stamp(since), "until": stamp(until)}


def request(since=10, until=20, **changes):
    return {"collection_uuid": COLLECTION, "collection_revision": 1, "operation": "download",
            "policy_sha256": "a" * 64, "cooldown_seconds": 30, "window": window(since, until), **changes}


def admission(body, **changes):
    value = decode(body)
    return {**value, "uuid": RUN, "root_uuid": ROOT, "state": "queued", **changes}


class RunQueueTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.now = [1000.0]
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())
        self.queue = RunQueue(self.box)
        self.client = SimpleNamespace(endpoint=self.box.endpoint, producer=PRODUCER,
            capabilities=Mock(return_value={"source_runs": True, "source_run_protocol": 1,
                                            "source_run_submission_receipts": True}),
            _request=Mock(side_effect=lambda method, route, body: admission(body)))

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: self.now[0])

    def claim(self):
        return self.queue.claim(str(uuid.uuid4()))

    def pending(self, intent):
        return decode(self.box.db.execute("SELECT windows FROM run_intents WHERE uuid=?", (intent,)).fetchone()[0])

    def test_many_offline_timers_coalesce_before_any_request_is_frozen(self):
        intent = self.queue.enqueue(request())
        for day in range(11, 200):
            self.assertEqual(self.queue.enqueue(request(day, day + 10)), intent)
        self.assertEqual(self.pending(intent), [window(10, 209)])
        self.assertEqual(self.queue.status()["pending_windows"], 1)
        self.assertEqual(self.queue.history(intent), [])
        self.client.capabilities.side_effect = Unavailable("network_unavailable")
        failed = submit_once(self.queue, self.client)
        self.assertEqual(failed["state"], "pending")
        self.client._request.assert_not_called()
        original = self.queue.history(intent)[0]["request_uuid"]
        self.box.close()
        self.box = self.open()
        self.queue = RunQueue(self.box)
        self.assertIsNone(self.claim(), "scheduled repeats must retain backoff")
        self.now[0] += 5
        delivery = self.claim()
        self.assertEqual(delivery.request_uuid, original)
        self.assertEqual(decode(delivery.body)["window"], window(10, 209))

    def test_expansion_around_frozen_window_preserves_holes_and_immutable_retry(self):
        intent = self.queue.enqueue(request())
        frozen = self.claim()
        self.queue.fail(frozen, "network_unavailable")
        self.queue.enqueue(request(None, 25))
        self.assertEqual(self.pending(intent), [window(None, 10), window(20, 25)])
        self.assertIsNone(self.claim())
        self.now[0] += 5
        replay = self.claim()
        self.assertEqual(replay.body, frozen.body)
        self.queue.admit(replay, admission(replay.body))
        newest = self.claim()
        self.assertEqual(decode(newest.body)["window"], window(20, 25))
        self.queue.admit(newest, admission(newest.body))
        oldest = self.claim()
        self.assertEqual(decode(oldest.body)["window"], window(None, 10))
        self.queue.admit(oldest, admission(oldest.body))
        self.assertIsNone(self.claim())
        self.assertEqual(self.queue.status()["counts"]["admitted"], 3)
        self.assertTrue(all(row["state"] == "admitted" for row in self.queue.history(intent)))

    def test_disjoint_windows_keep_the_gap_and_downloads_precede_enrichment(self):
        first = self.queue.enqueue(request(1, 2))
        self.queue.enqueue(request(5, 6))
        self.queue.enqueue(request(100, 101, operation="enrich"))
        self.assertEqual(self.pending(first), [window(1, 2), window(5, 6)])
        next = self.claim()
        self.assertEqual(decode(next.body)["operation"], "download")
        self.assertEqual(decode(next.body)["window"], window(5, 6))

    def test_concurrent_enqueues_and_senders_share_one_frozen_submission(self):
        def worker(index):
            box = self.open()
            try:
                queue = RunQueue(box)
                intent = queue.enqueue(request(index, index + 20))
                return intent, queue.claim(str(uuid.uuid4()))
            finally:
                box.close()
        with ThreadPoolExecutor(max_workers=8) as pool:
            results = list(pool.map(worker, range(16)))
        self.assertEqual(len({item[0] for item in results}), 1)
        deliveries = [item[1] for item in results if item[1] is not None]
        self.assertEqual(len(deliveries), 1)
        self.assertEqual(len(self.queue.history(results[0][0])), 1)
        frozen = decode(deliveries[0].body)["window"]
        from stash_ingest.windows import union
        self.assertEqual(union([frozen], self.pending(results[0][0])), [window(0, 35)])

    def test_expired_sender_is_fenced_and_bad_receipts_never_release_bytes(self):
        self.queue.enqueue(request())
        old = self.claim()
        self.now[0] += 121
        current = self.claim()
        self.assertEqual(old.body, current.body)
        with self.assertRaises(LeaseLost):
            self.queue.admit(old, admission(old.body))
        for change in ({"request_uuid": str(uuid.uuid4())}, {"collection_revision": 2}, {"policy_sha256": "b" * 64},
                       {"state": "complete"}, {"uuid": "not-a-uuid"}, {"root_uuid": None}):
            with self.subTest(change=change), self.assertRaises(InvalidData):
                self.queue.admit(current, admission(current.body, **change))
        row = self.box.db.execute("SELECT body FROM run_requests").fetchone()
        self.assertEqual(row[0], old.body)
        self.queue.admit(current, admission(current.body))
        self.assertIsNone(self.box.db.execute("SELECT body FROM run_requests").fetchone()[0])

    def test_capacity_or_invalid_specs_roll_back_without_losing_old_windows(self):
        limited = RunQueue(self.box, max_groups=1, max_windows=2)
        intent = limited.enqueue(request(1, 2))
        limited.enqueue(request(5, 6))
        with self.assertRaises(Capacity):
            limited.enqueue(request(8, 9))
        with self.assertRaises(Capacity):
            limited.enqueue(request(collection_uuid=str(uuid.uuid4())))
        before = self.pending(intent)
        for value in (request(window=window(5, 5)), request(window={"since": None, "until": "2026-01-01T00:00:00.000001Z"}),
                      request(secret="must-not-persist"), request(operation=[]), request(collection_revision=True),
                      request(policy_sha256="invalid")):
            with self.assertRaises(InvalidData):
                limited.enqueue(value)
        self.assertEqual(self.pending(intent), before)
        self.assertEqual(limited.status()["configurations"], 1)

    def test_review_survives_scheduling_until_explicit_retry_and_admission_is_not_completion(self):
        intent = self.queue.enqueue(request())
        self.client._request.side_effect = Unavailable("outside_scope", 403)
        failed = submit_once(self.queue, self.client)
        self.assertEqual(failed["state"], "review")
        self.queue.enqueue(request(None, 40))
        self.now[0] += 10000
        self.assertIsNone(self.claim())
        self.queue.retry(failed["request_uuid"])
        self.client._request.side_effect = lambda method, route, body: admission(body)
        result = submit_once(self.queue, self.client)
        self.assertEqual(result["state"], "admitted")
        self.assertEqual(result["request_uuid"], failed["request_uuid"])
        self.assertEqual(self.queue.history(intent)[0]["run_uuid"], RUN)
        self.assertNotIn("completed", self.queue.status()["counts"])

    def test_receipt_capability_is_required_and_auth_rotation_retains_the_same_request(self):
        intent = self.queue.enqueue(request())
        self.client.capabilities.return_value = {"source_runs": True, "source_run_protocol": 1}
        first = submit_once(self.queue, self.client)
        self.assertEqual(first["error_code"], "incompatible_source_runs")
        self.client._request.assert_not_called()
        self.client.capabilities.return_value["source_run_submission_receipts"] = True
        self.now[0] += 5
        self.client._request.side_effect = Unavailable("stash_token_rejected", 401)
        second = submit_once(self.queue, self.client)
        self.assertEqual(second["state"], "pending")
        self.now[0] += 10
        self.client._request.side_effect = lambda method, route, body: admission(body)
        final = submit_once(self.queue, self.client)
        self.assertEqual(final["state"], "admitted")
        self.assertEqual(first["request_uuid"], final["request_uuid"])
        self.assertEqual(len(self.queue.history(intent)), 1)

    def test_queue_age_survives_coalescing_and_resets_for_a_new_schedule(self):
        self.queue.enqueue(request())
        self.now[0] += 10
        self.queue.enqueue(request(11, 21))
        self.assertEqual(self.queue.status()["oldest_queued_seconds"], 10)
        delivery = self.claim()
        self.assertEqual(self.queue.status()["oldest_queued_seconds"], 10)
        self.queue.admit(delivery, admission(delivery.body))
        self.assertIsNone(self.queue.status()["oldest_queued_seconds"])
        self.now[0] += 300
        self.queue.enqueue(request(11, 21))
        self.assertEqual(self.queue.status()["oldest_queued_seconds"], 0)

    def test_caller_ticket_replay_after_admission_does_not_schedule_another_scan(self):
        ticket = str(uuid.uuid4())
        intent = self.queue.enqueue(request(), ticket_uuid=ticket)
        sent = submit_once(self.queue, self.client)
        self.assertEqual(sent["state"], "admitted")
        self.box.close()
        self.box = self.open()
        self.queue = RunQueue(self.box)
        self.assertEqual(self.queue.enqueue(request(), ticket_uuid=ticket), intent)
        self.assertIsNone(self.claim())
        with self.assertRaises(Conflict):
            self.queue.enqueue(request(10, 21), ticket_uuid=ticket)
        saved = self.queue.ticket(ticket)
        history = self.queue.history(saved["intent_uuid"], after=saved["first_sequence"] - 1)
        self.assertEqual(history[0]["request_uuid"], sent["request_uuid"])
        self.queue.enqueue(request(), ticket_uuid=str(uuid.uuid4()))
        next = self.claim()
        self.assertIsNotNone(next, "a distinct scheduled execution may request a fresh scan")
        self.assertNotEqual(next.request_uuid, sent["request_uuid"])

    def test_ticket_capacity_and_corrupt_frozen_bytes_stop_without_submission(self):
        limited = RunQueue(self.box, max_tickets=1)
        ticket = str(uuid.uuid4())
        intent = limited.enqueue(request(), ticket_uuid=ticket)
        self.assertEqual(limited.enqueue(request(), ticket_uuid=ticket), intent)
        with self.assertRaises(Capacity):
            limited.enqueue(request(None, 40), ticket_uuid=str(uuid.uuid4()))
        self.assertEqual(self.pending(intent), [window(10, 20)])
        frozen = self.claim()
        self.queue.fail(frozen, "network_unavailable")
        self.box.db.execute("UPDATE run_requests SET body=?", (b"damaged",))
        self.now[0] += 5
        result = submit_once(self.queue, self.client)
        self.assertEqual(result["error_code"], "request_integrity_failed")
        self.assertEqual(result["state"], "review")
        self.client.capabilities.assert_not_called()
        self.client._request.assert_not_called()
        self.assertEqual(self.box.db.execute("SELECT body FROM run_requests").fetchone()[0], b"damaged")

    def test_process_death_keeps_frozen_request_and_pending_expansion(self):
        script = """import json, os, sys, uuid
from stash_ingest.outbox import Outbox
from stash_ingest.run_queue import RunQueue
box = Outbox(sys.argv[1], 'http://fixture.invalid', sys.argv[2], clock=lambda: 1000)
queue = RunQueue(box)
value = json.load(sys.stdin)
queue.enqueue(value)
queue.claim(str(uuid.uuid4()))
value['window']['since'] = None
queue.enqueue(value)
os._exit(19)
"""
        process = subprocess.run([sys.executable, "-c", script, str(self.path), PRODUCER], input=encode(request()), timeout=15)
        self.assertEqual(process.returncode, 19)
        row = self.box.db.execute("SELECT body,intent_uuid FROM run_requests").fetchone()
        self.now[0] += 121
        recovered = self.claim()
        self.assertEqual(recovered.body, row[0])
        self.assertEqual(self.pending(row[1]), [window(None, 10)])
        self.assertEqual(self.box.db.execute("PRAGMA integrity_check").fetchone()[0], "ok")

    def test_cli_records_a_request_without_network_or_media_claims(self):
        command = [sys.executable, "-m", "stash_ingest.cli", "--outbox", str(self.path),
                   "--endpoint", self.box.endpoint, "--producer", PRODUCER]
        result = subprocess.run([*command, "queue-run", "--collection", COLLECTION, "--revision", "1",
                                "--policy", "a" * 64, "--until", "2026-10-01T00:00:00Z"],
                                capture_output=True, check=True, timeout=15)
        output = json.loads(result.stdout)
        self.assertEqual(output["state"], "recorded")
        self.assertEqual(output["requests"]["pending_windows"], 1)
        self.assertEqual(self.box.status()["counts"]["pending"], 0)
        environment = {**os.environ, "STASH_INGEST_TOKEN": ""}
        failed = subprocess.run([*command, "submit-runs"], env=environment, capture_output=True, timeout=15)
        self.assertEqual(failed.returncode, 2)
        self.assertEqual(json.loads(failed.stdout)["submission"]["error_code"], "stash_token_missing")


if __name__ == "__main__":
    unittest.main()
