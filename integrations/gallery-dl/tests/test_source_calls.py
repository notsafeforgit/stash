from concurrent.futures import ThreadPoolExecutor
from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.caller_input import record_file_call
from stash_ingest.client import Client, Unavailable
from stash_ingest.completion import inspect_call
from stash_ingest.encoding import InvalidData, decode
from stash_ingest.outbox import Capacity, Conflict, LeaseLost, Outbox
from stash_ingest.run_queue import RunQueue
from stash_ingest.source_calls import SourceCalls, resolve_once
from helpers import PRODUCER, ROOT
from test_run_queue import window


def target(index):
    return f"https://fixture.invalid/account/{index}"


def candidate(url, state="active"):
    return {"collection_uuid": str(uuid.uuid5(uuid.NAMESPACE_URL, url)), "collection_revision": 1, "state": state}


def snapshot(count=1):
    return {"targets": [target(i) for i in range(count)], "root_uuid": ROOT, "operation": "download",
            "policy_sha256": "a" * 64, "cooldown_seconds": 30, "window": window(10, 20)}


class SourceCallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "producer.sqlite"
        self.now = 1000.0
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())
        self.calls = SourceCalls(self.box)
        self.client = Client(self.box.endpoint, PRODUCER)
        self.client.capabilities = Mock(return_value={"collection_lookup": True, "source_runs": True, "source_run_protocol": 1})
        self.client._request = Mock(side_effect=self.http)
        self.matches, self.runs = {}, {}
        self.call_uuid = str(uuid.uuid4())

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: self.now)

    def http(self, method, route, body=None, **kwargs):
        self.assertFalse(self.box.db.in_transaction, "network I/O must not hold a SQLite transaction")
        if route == "/collections/lookup":
            request = decode(body)
            return {"root_uuid": request["root_uuid"], "targets": [{"target_url": url,
                "candidates": self.matches.get(url, [candidate(url)]), "has_more": False} for url in request["targets"]]}
        self.assertEqual(method, "GET")
        return self.runs[route.removeprefix("/runs/")]

    def record(self, count=1, call_uuid=None):
        return self.calls.record(call_uuid or self.call_uuid, "b" * 64, lambda: snapshot(count))

    def test_profile_subscription_uses_one_collection_and_separate_retrieval_tickets(self):
        from stash_ingest.n8n_sources import target_urls
        from stash_ingest.profile_sources import expand_profiles
        profile = target_urls("reddit", [("handle", "Example"), ("handle", "example")])
        self.assertEqual(profile, ["https://www.reddit.com/user/example/"])
        urls = expand_profiles(profile)
        self.assertEqual(len(urls), 6)
        binding = candidate(profile[0])
        self.matches = {url: [{**binding, "retrieval_url": url}] for url in urls}
        value = {**snapshot(), "targets": profile}
        self.assertEqual(self.calls.record(self.call_uuid, "b" * 64, lambda: value)["target_count"], 6)
        self.assertEqual(resolve_once(self.calls, self.client)["counts"], {"queued": 6, "review": 0})
        rows = self.calls.page(self.call_uuid)
        self.assertEqual({row["collection_uuid"] for row in rows}, {binding["collection_uuid"]})
        templates = [decode(row[0]) for row in self.box.db.execute("SELECT template FROM run_intents")]
        self.assertEqual({row["retrieval_url"] for row in templates}, set(urls))
        self.assertEqual(len(templates), 6, "coverage for one pass must not complete another")
        second = str(uuid.uuid4())
        self.calls.record(second, "b" * 64, lambda: value)
        resolve_once(self.calls, self.client)
        self.assertEqual(RunQueue(self.box).status()["configurations"], 6, "repeated schedules coalesce")

    def test_large_offline_list_resolves_in_bounded_pages_and_overlapping_calls_coalesce(self):
        self.assertEqual(self.record(500)["target_count"], 500)
        self.client.capabilities.side_effect = Unavailable("network_unavailable")
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "pending")
        self.client._request.assert_not_called()
        self.box.close()
        self.box = self.open()
        self.calls = SourceCalls(self.box)
        self.now += 5
        self.client.capabilities.side_effect = None
        for _ in range(10):
            self.assertEqual(resolve_once(self.calls, self.client)["counts"], {"queued": 50, "review": 0})
        self.assertEqual(self.calls.summary(self.call_uuid)["counts"], {"pending": 0, "resolving": 0, "queued": 500, "review": 0})
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "idle")
        for call in self.client._request.call_args_list:
            self.assertLessEqual(len(decode(call.args[2])["targets"]), 50)
        queue = RunQueue(self.box)
        self.assertEqual(queue.status()["configurations"], 500)
        self.assertEqual(queue.status()["caller_tickets"], 500)
        second = str(uuid.uuid4())
        self.record(20, second)
        resolve_once(self.calls, self.client)
        self.assertEqual(queue.status()["configurations"], 500)
        self.assertEqual(queue.status()["caller_tickets"], 520)
        status = inspect_call(self.calls, self.client, self.call_uuid)
        self.assertEqual(status["state"], "recorded")
        self.assertEqual(status["target_count"], 500)
        self.assertEqual(len(status["issues"]), 20)
        self.assertTrue(status["issues_truncated"])

    def test_file_list_and_relative_time_are_frozen_before_any_network_access(self):
        path = Path(self.temp.name) / "sources.txt"
        path.write_text(f"# saved list\n{target(0)} annotation\n\n{target(1)}\n{target(0)}\n")
        args = SimpleNamespace(call=self.call_uuid, targets_file=str(path), profile=None, policy="a" * 64,
            root=ROOT, operation="download", cooldown=30, since=None, lookback_seconds=600, until=None)
        original = record_file_call(self.calls, args)
        self.assertEqual(original["target_count"], 2)
        self.assertEqual(original["definition"]["window"], {"since": "1970-01-01T00:06:40.000Z", "until": "1970-01-01T00:16:40.000Z"})
        self.now += 100
        path.unlink()
        self.assertEqual(record_file_call(self.calls, args), original)
        self.assertEqual([row["target_url"] for row in self.calls.page(self.call_uuid)], [target(0), target(1)])
        args.lookback_seconds = 900
        with self.assertRaises(Conflict):
            record_file_call(self.calls, args)
        self.client.capabilities.assert_not_called()

    def test_scan_request_time_is_frozen_and_cannot_mix_with_a_date_range(self):
        path = Path(self.temp.name) / 'sources.txt'
        path.write_text(target(0))
        args = SimpleNamespace(call=self.call_uuid, targets_file=str(path), profile=None, policy='a' * 64,
            root=ROOT, operation='download', cooldown=30, since=None, lookback_seconds=None, until=None,
            source_mode='traversal')
        for overrides in ({'since': '1960-01-01T00:00:00Z'}, {'lookback_seconds': 60}, {'operation': 'enrich'}):
            with self.subTest(overrides=overrides), self.assertRaises(InvalidData):
                record_file_call(self.calls, SimpleNamespace(**{**vars(args), **overrides}))
        original = record_file_call(self.calls, args)
        self.assertEqual(original['definition']['window'], {'basis': 'traversal', 'since': None, 'until': '1970-01-01T00:16:40.000Z'})
        self.now += 100
        path.unlink()
        self.assertEqual(record_file_call(self.calls, args), original)
        args.source_mode = 'published'
        with self.assertRaises(Conflict):
            record_file_call(self.calls, args)
        self.client.capabilities.assert_not_called()

    def test_concurrent_first_calls_keep_one_snapshot_and_do_not_repeat_preparation(self):
        def record(index):
            with closing(self.open()) as box:
                return SourceCalls(box).record(self.call_uuid, "b" * 64, lambda: snapshot(index + 1))
        with ThreadPoolExecutor(max_workers=4) as pool:
            results = list(pool.map(record, range(8)))
        self.assertTrue(all(result == results[0] for result in results))
        prepare = Mock(side_effect=AssertionError("recomputed a caller snapshot"))
        self.calls.record(self.call_uuid, "b" * 64, prepare)
        prepare.assert_not_called()
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM source_calls").fetchone()[0], 1)

    def test_expired_lookup_cannot_replace_a_later_binding_or_enqueue_its_ticket(self):
        self.record()
        old = self.calls.claim(str(uuid.uuid4()))
        self.now += 121
        current = self.calls.claim(str(uuid.uuid4()))
        result = {"target_url": target(0), "state": "resolved", "candidates": [candidate(target(0))], "has_more": False}
        with self.assertRaises(LeaseLost):
            self.calls.bind(old, old.targets[0], result)
        self.assertEqual(RunQueue(self.box).status()["caller_tickets"], 0)
        self.calls.bind(current, current.targets[0], result)
        before = self.calls.page(self.call_uuid)
        self.matches[target(0)] = [candidate("https://fixture.invalid/retargeted")]
        self.assertEqual(self.calls.retry(self.call_uuid), 0)
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "idle")
        self.assertEqual(self.calls.page(self.call_uuid), before)

    def test_binding_and_ticket_rollback_together_when_local_commit_fails(self):
        self.record()
        delivery = self.calls.claim(str(uuid.uuid4()))
        match = {"target_url": target(0), "state": "resolved", "candidates": [candidate(target(0))], "has_more": False}
        self.box.db.execute("""CREATE TRIGGER interrupt_binding BEFORE UPDATE ON source_call_targets WHEN NEW.state='queued'
            BEGIN SELECT RAISE(ABORT,'fixture interruption'); END""")
        with self.assertRaisesRegex(sqlite3.IntegrityError, "fixture interruption"):
            self.calls.bind(delivery, delivery.targets[0], match)
        self.assertEqual(RunQueue(self.box).status()["configurations"], 0)
        self.assertEqual(RunQueue(self.box).status()["caller_tickets"], 0)
        self.assertEqual(self.calls.page(self.call_uuid)[0]["state"], "resolving")
        self.box.db.execute("DROP TRIGGER interrupt_binding")
        self.calls.bind(delivery, delivery.targets[0], match)
        self.box.close()
        self.box = self.open()
        self.calls = SourceCalls(self.box)
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "idle")
        self.assertEqual(RunQueue(self.box).status()["caller_tickets"], 1)

    def test_missing_ambiguous_and_inactive_targets_need_review_without_rebinding_queued_targets(self):
        self.record(5)
        self.matches = {target(0): [], target(1): [candidate(target(1)), candidate(target(1) + "?other")],
                        target(2): [candidate(target(2), "disabled")], target(3): [candidate(target(3), "retired")]}
        result = resolve_once(self.calls, self.client)
        self.assertEqual(result["counts"], {"queued": 1, "review": 4})
        rows = self.calls.page(self.call_uuid)
        self.assertEqual([row["error_code"] for row in rows], ["collection_unresolved", "collection_ambiguous",
                        "collection_disabled", "collection_retired", None])
        queued = rows[-1]
        self.matches = {target(4): [candidate("https://fixture.invalid/changed")]}
        self.assertEqual(self.calls.retry(self.call_uuid), 4)
        self.assertEqual(resolve_once(self.calls, self.client)["counts"], {"queued": 4, "review": 0})
        self.assertEqual(self.calls.page(self.call_uuid)[-1], queued)

    def test_outage_backoff_survives_restart_and_scope_failure_requires_review(self):
        self.record()
        self.client.capabilities.side_effect = Unavailable("queue_full", 429, 120)
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "pending")
        self.box.close()
        self.box = self.open()
        self.calls = SourceCalls(self.box)
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "idle")
        self.now += 120
        self.client.capabilities.side_effect = Unavailable("outside_scope", 403)
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "review")
        self.now += 10000
        self.assertEqual(resolve_once(self.calls, self.client)["state"], "idle")
        self.assertEqual(RunQueue(self.box).status()["caller_tickets"], 0)

    def test_capacity_retains_old_calls_and_never_partially_records_a_list(self):
        limited = SourceCalls(self.box, max_targets=2)
        limited.record(self.call_uuid, "b" * 64, snapshot)
        with self.assertRaises(Capacity):
            limited.record(str(uuid.uuid4()), "b" * 64, lambda: snapshot(2))
        self.assertEqual(limited.summary()["calls"], 1)
        self.assertEqual(limited.summary()["counts"]["pending"], 1)

    def test_new_downloads_resolve_before_backfill_and_metadata_enrichment(self):
        enrichment = str(uuid.uuid4())
        self.calls.record(enrichment, "b" * 64, lambda: {**snapshot(), "operation": "enrich", "window": window(30, 40)})
        self.record()
        newest = str(uuid.uuid4())
        self.calls.record(newest, "b" * 64, lambda: {**snapshot(), "window": window(20, 30)})
        self.assertEqual(resolve_once(self.calls, self.client)["call_uuid"], newest)
        self.assertEqual(resolve_once(self.calls, self.client)["call_uuid"], self.call_uuid)
        self.assertEqual(resolve_once(self.calls, self.client)["call_uuid"], enrichment)

    def test_whole_call_completion_uses_original_tickets_and_never_claims_media_intake(self):
        self.record(3)
        resolve_once(self.calls, self.client)
        queue = RunQueue(self.box)

        def admit_all(cancel_one=False):
            ids = []
            while sent := queue.claim(str(uuid.uuid4())):
                request = decode(sent.body)
                run_uuid = str(uuid.uuid5(uuid.UUID(sent.request_uuid), "native"))
                receipt = {**request, "uuid": run_uuid, "root_uuid": ROOT, "state": "queued"}
                queue.admit(sent, receipt)
                self.runs[run_uuid] = {**receipt, "state": "succeeded", "completed": [request["window"]]}
                ids.append(run_uuid)
            if cancel_one:
                self.runs[ids[0]].update(state="cancelled", completed=[])
            return ids

        old_runs = admit_all(cancel_one=True)
        self.client.capabilities.reset_mock()
        original = inspect_call(self.calls, self.client, self.call_uuid)
        self.assertEqual(original["state"], "cancelled")
        self.assertEqual(original["counts"], {"source_succeeded": 2, "cancelled": 1})
        self.client.capabilities.assert_called_once()
        later = str(uuid.uuid4())
        self.record(3, later)
        resolve_once(self.calls, self.client)
        admit_all()
        result = inspect_call(self.calls, self.client, later)
        self.assertEqual(result["state"], "source_succeeded")
        self.assertEqual(result["counts"], {"source_succeeded": 3})
        self.assertEqual(result["intake_completion"], "inspect_native_receipts")
        self.assertEqual(inspect_call(self.calls, self.client, self.call_uuid)["state"], "cancelled")
        self.client.capabilities.side_effect = Unavailable("network_unavailable")
        self.assertEqual(inspect_call(self.calls, self.client, later)["state"], "unavailable")
        self.assertEqual(self.runs[old_runs[0]]["state"], "cancelled")

    def test_a_matching_receipt_and_status_cannot_change_the_callers_original_root(self):
        self.record()
        resolve_once(self.calls, self.client)
        queue = RunQueue(self.box)
        sent = queue.claim(str(uuid.uuid4()))
        request = decode(sent.body)
        run_uuid = str(uuid.uuid4())
        wrong = {**request, "uuid": run_uuid, "root_uuid": str(uuid.uuid4()), "state": "succeeded",
                 "completed": [request["window"]]}
        queue.admit(sent, wrong)
        self.runs[run_uuid] = wrong
        result = inspect_call(self.calls, self.client, self.call_uuid)
        self.assertEqual(result["state"], "review")
        self.assertEqual(result["issues"][0]["error_code"], "source_status_mismatch")


if __name__ == "__main__":
    unittest.main()
