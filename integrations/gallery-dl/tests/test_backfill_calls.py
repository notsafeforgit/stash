from concurrent.futures import ThreadPoolExecutor
from contextlib import closing
from copy import deepcopy
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest import backfills
from stash_ingest.backfill_calls import BackfillCalls, advance_once
from stash_ingest.client import Client, Unavailable
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.outbox import Capacity, Conflict, LeaseLost, Outbox
from stash_ingest.run_queue import RunQueue
from stash_ingest.source_calls import SourceCalls, resolve_once
from helpers import PRODUCER, ROOT
from test_source_calls import candidate


def specification(mode="reddit-top", account="Example"):
    platform = "twitter" if mode == "twitter" else "reddit"
    return {"version": 1, "root_uuid": ROOT, "platform": platform, "account": account,
            "component": mode, "policy_sha256": "a" * 64,
            "window": {"since": None, "until": "2026-10-01T00:00:00.000Z"},
            "targets": backfills.targets(platform, account, mode)}


def decision(mode="reddit-top", *, skip=False, native=False, key=None):
    return {"uuid": key or str(uuid.uuid4()), "component": "*" if skip else mode,
            "outcome": "skipped" if skip else "completed",
            "basis": "legacy_skip" if skip else "source_runs" if native else "legacy_completion",
            "decided_at": "2026-10-01T01:02:03.123456789Z"}


def status(spec, rows=()):
    done = {row["component"] for row in rows if row["outcome"] == "completed"}
    all_done = backfills.REQUIRED[spec["platform"]] <= done
    state = "completed" if spec["component"] in done or all_done else "skipped" if any(row["outcome"] == "skipped" for row in rows) else "needed"
    return {**backfills.subject(spec["root_uuid"], spec["platform"], spec["account"]), "component": spec["component"],
            "state": state, "account_complete": all_done, "decisions": list(rows)}


class BackfillCallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "producer.sqlite"
        self.now = 1000
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())
        self.calls = BackfillCalls(self.box)
        self.client = Client(self.box.endpoint, PRODUCER)
        self.client.capabilities = Mock(return_value={"source_backfill_protocol": 1, "collection_lookup": True,
                                                      "source_runs": True, "source_run_protocol": 1})
        self.client._request = Mock(side_effect=self.http)
        self.spec, self.rows, self.runs, self.proofs = specification(), [], {}, []
        self.call = str(uuid.uuid4())
        self.lose_completion = False

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: self.now)

    def reopen(self):
        self.box.close()
        self.box = self.open()
        self.calls = BackfillCalls(self.box)

    def record(self, call=None):
        return self.calls.record(call or self.call, "b" * 64, lambda: self.spec)

    def http(self, method, route, body=None, **kwargs):
        self.assertFalse(self.box.db.in_transaction, "HTTP must not hold the local SQLite write lock")
        if route == "/backfills/status":
            return status(self.spec, self.rows)
        if route == "/collections/lookup":
            value = decode(body)
            return {"root_uuid": ROOT, "targets": [{"target_url": url, "candidates": [candidate(url)], "has_more": False}
                                                  for url in value["targets"]]}
        if route == "/backfills/complete":
            proof = decode(body)
            self.proofs.append(body)
            value = decision(self.spec["component"], native=True, key=proof["uuid"])
            self.rows = [value]
            if self.lose_completion:
                self.lose_completion = False
                raise Unavailable("network_unavailable")
            return value
        self.assertEqual(method, "GET")
        return self.runs[route.removeprefix("/runs/")]

    def admit(self, *, complete=True):
        self.assertEqual(resolve_once(SourceCalls(self.box), self.client)["state"], "queued")
        queue = RunQueue(self.box)
        while sent := queue.claim(str(uuid.uuid4())):
            request = decode(sent.body)
            run = str(uuid.uuid4())
            receipt = {**request, "uuid": run, "root_uuid": ROOT, "state": "queued"}
            queue.admit(sent, receipt)
            self.runs[run] = {**receipt, "state": "succeeded" if complete else "queued",
                              "completed": [request["window"]] if complete else []}

    def test_offline_guard_retains_original_snapshot_and_cannot_admit_work(self):
        self.assertTrue(self.record()["backfill_pending"])
        self.client.capabilities.side_effect = Unavailable("network_unavailable")
        self.assertTrue(advance_once(self.calls, self.client)["backfill_pending"])
        self.assertEqual(SourceCalls(self.box).summary()["calls"], 0)
        self.assertEqual(RunQueue(self.box).status()["caller_tickets"], 0)
        self.reopen()
        self.calls.record(self.call, "b" * 64, Mock(side_effect=AssertionError("reread mutable input")))
        self.assertEqual(advance_once(self.calls, self.client)["state"], "idle")
        self.now += 5
        self.client.capabilities.side_effect = None
        self.assertEqual(advance_once(self.calls, self.client)["state"], "active")
        self.assertEqual(SourceCalls(self.box).summary()["calls"], 1)
        with self.assertRaises(Conflict):
            self.calls.record(self.call, "c" * 64, lambda: self.spec)

    def test_imported_acceptance_and_deliberate_skip_never_create_source_requests(self):
        for rows, wanted in (([decision("reddit-new"), decision()], "completed"), ([decision(skip=True)], "skipped")):
            self.rows, self.call = rows, str(uuid.uuid4())
            self.record()
            result = advance_once(self.calls, self.client, self.call)
            self.assertEqual(result["state"], wanted)
            self.assertEqual(result["exit_code"], 0)
            self.assertTrue(result["backfill_cached"])
            self.assertEqual(result["account_backfill_complete"], wanted == "completed")
            self.reopen()
            self.assertEqual(self.calls.result(self.call), result)
        self.assertEqual(SourceCalls(self.box).summary()["calls"], 0)
        self.assertEqual(RunQueue(self.box).status()["caller_tickets"], 0)

    def test_guard_and_source_snapshot_commit_atomically_and_expired_ownership_cannot_replace_them(self):
        self.record()
        old = self.calls.claim(str(uuid.uuid4()))
        self.now += 121
        current = self.calls.claim(str(uuid.uuid4()))
        with self.assertRaises(LeaseLost):
            self.calls.guard(old, status(self.spec))
        self.box.db.execute("""CREATE TRIGGER interrupt_guard BEFORE UPDATE OF guard ON backfill_calls
            BEGIN SELECT RAISE(ABORT,'fixture interruption'); END""")
        with self.assertRaises(sqlite3.IntegrityError):
            self.calls.guard(current, status(self.spec))
        self.assertEqual(SourceCalls(self.box).summary()["calls"], 0)
        self.box.db.execute("DROP TRIGGER interrupt_guard")
        self.calls.guard(current, status(self.spec))
        before = self.calls.result(self.call)
        with self.assertRaises(LeaseLost):
            self.calls.guard(old, status(self.spec, [decision()]))
        self.assertEqual(self.calls.result(self.call), before)

    def test_pending_original_jobs_cannot_be_completed_by_another_calls_later_history(self):
        self.record()
        advance_once(self.calls, self.client)
        self.admit(complete=False)
        self.rows = [decision("reddit-new"), decision()]
        result = advance_once(self.calls, self.client)
        self.assertTrue(result["backfill_pending"])
        self.assertFalse(result["backfill_cached"])
        self.assertFalse(self.proofs)
        for run in self.runs.values():
            run.update(state="cancelled", completed=[])
        self.now += 30
        result = advance_once(self.calls, self.client)
        self.assertTrue(result["command_failed"])
        self.assertFalse(result["backfill_pending"])
        self.assertEqual(result["stderr_tail"], "source_cancelled")
        self.calls.retry(self.call)
        self.assertTrue(advance_once(self.calls, self.client)["command_failed"])
        self.assertFalse(self.proofs)

    def test_completion_uses_all_original_requests_and_replays_exact_proof_after_lost_response(self):
        self.record()
        advance_once(self.calls, self.client)
        self.admit()
        self.lose_completion = True
        result = advance_once(self.calls, self.client)
        self.assertTrue(result["backfill_pending"])
        proof = decode(self.proofs[0])
        self.assertEqual(len(proof["requests"]), 4)
        self.assertEqual(proof["account"], "Example")
        self.assertTrue(all(value["window"] == self.spec["window"] for value in proof["requests"]))
        self.reopen()
        self.now += 100
        self.runs.clear()  # Retained proof replays without reinterpreting run state.
        result = advance_once(self.calls, self.client)
        self.assertEqual(result["state"], "completed")
        self.assertFalse(result["backfill_cached"])
        self.assertEqual(result["intake_completion"], "inspect_native_receipts")
        self.assertEqual(self.proofs, [self.proofs[0]] * 2)
        self.reopen()
        self.assertEqual(self.calls.result(self.call), result)
        self.assertEqual(advance_once(self.calls, self.client)["state"], "idle")

    def test_unrelated_or_inconsistent_history_requires_review_instead_of_admission(self):
        self.record()
        wrong = status(self.spec, [decision()])
        wrong["account"] = "different"
        self.client._request.side_effect = lambda *a, **k: wrong
        self.assertTrue(advance_once(self.calls, self.client)["command_failed"])
        self.assertEqual(SourceCalls(self.box).summary()["calls"], 0)
        for change in ({"state": "completed"}, {"account_complete": True}, {"decisions": [decision(skip=True)]}):
            with self.assertRaises(InvalidData):
                backfills.validate_status({**status(self.spec), **change}, self.spec)

    def test_concurrent_recording_and_capacity_preserve_one_original_call(self):
        def record(_):
            with closing(self.open()) as box:
                return BackfillCalls(box).record(self.call, "b" * 64, lambda: deepcopy(self.spec))
        with ThreadPoolExecutor(max_workers=4) as pool:
            result = list(pool.map(record, range(8)))
        self.assertTrue(all(item == result[0] for item in result))
        with self.assertRaises(Capacity):
            BackfillCalls(self.box, max_calls=1).record(str(uuid.uuid4()), "b" * 64, lambda: self.spec)
        self.assertEqual(self.calls.summary()["calls"], 1)


if __name__ == "__main__":
    unittest.main()
