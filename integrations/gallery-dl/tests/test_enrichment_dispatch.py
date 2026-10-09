from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
from stash_ingest.enrichment_client import EnrichmentClient
from stash_ingest.enrichment_dispatch import EnrichmentDispatcher, PAGE_SIZE
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.outbox import Outbox, SCHEMA
from helpers import PRODUCER
from test_enrichment_execution import execution_fixture
from test_outbox_migration import schema_eight


class EnrichmentDispatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.collection = str(uuid.uuid4())
        self.now = 1000
        self.transport = SimpleNamespace(endpoint="http://fixture.invalid", producer=PRODUCER)
        self.profile = SimpleNamespace(policy_sha256="a" * 64, extractor_version="1.32.15-dev", check=Mock(), accepts=Mock(return_value=True))
        self.executor = patch("stash_ingest.enrichment_dispatch.execute", return_value={"state": "waiting"}).start()
        self.addCleanup(patch.stopall)
        self.native_ready = patch.object(EnrichmentClient, "ready_jobs", return_value=[]).start()
        self.targets = patch.object(EnrichmentClient, "ready", return_value=[]).start()
        self.admit = patch.object(EnrichmentClient, "admit", return_value={"uuid": str(uuid.uuid4())}).start()
        self.describe = patch.object(EnrichmentClient, "describe", return_value={"job": {}}).start()
        self.capabilities = patch.object(EnrichmentClient, "capabilities", return_value={"enrichment_dispatch_protocol": 1}).start()

    def open(self):
        return Outbox(self.path, self.transport.endpoint, PRODUCER, clock=lambda: self.now)

    def worker(self, box, profile=True):
        return EnrichmentDispatcher(box, self.transport, self.collection, self.profile if profile else None)

    def target(self, priority=20):
        return {"uuid": str(uuid.uuid4()), "revision": 1, "state": "pending", "collection_uuid": self.collection,
                "url": "https://www.reddit.com/comments/abc123", "priority": priority, "not_before": "2026-10-03T00:00:00.000000001Z"}

    def stage(self, box, policy="a" * 64):
        journal = EnrichmentJournal(box)
        fixture = execution_fixture()
        fixture["target"]["collection_uuid"] = self.collection
        fixture["job"]["arguments"].update(collection_uuid=self.collection, policy_sha256=policy)
        source = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
        with journal.execution():
            value = journal.claim(journal.prepare(fixture), fixture["job"])
            lease = {**fixture["job"], "state": "running", "revision": 2, "fence": 1, "owner_uuid": value.state["claim"]["owner_uuid"]}
            value = journal.reserve(journal.claimed(value, lease))
            return journal.checkpoint(value, lease, 0, decode(source.read_bytes())["complete"])

    def test_lost_admission_response_is_rediscovered_after_restart(self):
        target = self.target()
        self.targets.return_value = [target]
        self.admit.side_effect = Unavailable("network_unavailable")
        with closing(self.open()) as box:
            result = self.worker(box).once()
            self.assertEqual(result["state"], "unavailable")
            self.assertEqual(self.worker(box).state()["target_cursor"]["uuid"], target["uuid"])
        self.now = result["next_attempt_at"]
        job = str(uuid.uuid4())
        self.native_ready.return_value = [{"sequence": 1, "uuid": job}]
        self.executor.return_value = {"state": "completed", "job_uuid": job}
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["job_uuid"], job)
        self.admit.assert_called_once()
        self.executor.assert_called_once()

    def test_process_death_after_selection_preserves_cursor_and_wraps_before_new_admission(self):
        first = {"sequence": 1, "uuid": str(uuid.uuid4())}
        self.native_ready.return_value = [first]
        self.executor.side_effect = RuntimeError("process disappeared")
        with closing(self.open()) as box:
            with self.assertRaises(RuntimeError):
                self.worker(box).once()
        self.native_ready.return_value = []
        self.executor.side_effect = None
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "waiting")
            self.assertEqual(self.worker(box).state()["job_after"], 0)
        self.targets.assert_not_called()
        self.native_ready.return_value = [first]
        self.executor.return_value = {"state": "completed"}
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "completed")

    def test_pending_old_profile_delivery_precedes_all_new_source_work(self):
        with closing(self.open()) as box:
            value = self.stage(box, "b" * 64)
            self.executor.return_value = {"state": "delivery_pending"}
            result = self.worker(box).once()
            self.assertEqual(result["state"], "delivery_pending")
            self.assertEqual(result["dispatch"]["state"], "unavailable")
            self.executor.assert_called_once_with(box, self.transport, None, value.job_uuid)

            self.executor.reset_mock(side_effect=True)
            self.describe.return_value = {"job": {"execution_policy_sha256": self.profile.policy_sha256}}
            self.executor.side_effect = [{"state": "ownership_required"}, {"state": "completed"}]
            self.now = result["dispatch"]["next_attempt_at"]
            self.assertEqual(self.worker(box).once()["state"], "completed")
            self.assertIs(self.executor.call_args_list[1].args[2], self.profile)
            self.assertEqual(EnrichmentJournal(box).find(value.job_uuid).body, value.body)
        self.native_ready.assert_not_called()
        self.targets.assert_not_called()

    def test_blocked_admitted_work_does_not_admit_more_targets(self):
        self.native_ready.return_value = [{"sequence": 1, "uuid": str(uuid.uuid4())}]
        self.targets.return_value = [self.target()]
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "waiting")
            self.assertEqual(self.worker(box).state()["job_after"], 0)
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "waiting")
        self.targets.assert_not_called()
        self.admit.assert_not_called()

    def test_expired_delivery_uses_matching_profile_for_reclaim_but_never_a_different_profile(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            self.executor.side_effect = [{"state": "ownership_required"}, {"state": "completed"}]
            self.assertEqual(self.worker(box).once()["state"], "completed")
            self.assertIsNone(self.executor.call_args_list[0].args[2])
            self.assertIs(self.executor.call_args_list[1].args[2], self.profile)
            self.executor.reset_mock(side_effect=True)
            self.profile.policy_sha256 = "b" * 64
            self.executor.return_value = {"state": "ownership_required"}
            self.assertEqual(self.worker(box).once()["state"], "profile_required")
            self.executor.assert_called_once_with(box, self.transport, None, value.job_uuid)

    def test_delivery_only_dispatch_never_loads_profile_or_discovers_sources(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            self.executor.return_value = {"state": "completed"}
            self.assertEqual(self.worker(box, profile=False).once()["state"], "completed")
            self.executor.assert_called_once_with(box, self.transport, None, value.job_uuid)
        self.native_ready.assert_not_called()
        self.capabilities.assert_not_called()
        self.profile.check.assert_not_called()

    def test_unsupported_front_page_does_not_starve_later_targets(self):
        first = sorted([self.target() for _ in range(PAGE_SIZE)], key=lambda t: t["uuid"])
        second = [self.target(priority=10)]
        self.targets.side_effect = [first, second]
        self.profile.accepts.side_effect = [False] * PAGE_SIZE + [True]
        self.executor.return_value = {"state": "completed"}
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "waiting")
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "completed")
        self.assertEqual(self.targets.call_args.kwargs["after"], EnrichmentClient.target_cursor(first[-1]))
        self.admit.assert_called_once_with(second[0]["uuid"], 1, self.profile.policy_sha256, self.profile.extractor_version)

    def test_retry_after_and_idle_delays_survive_restart_without_claiming_completion(self):
        self.capabilities.side_effect = Unavailable("queue_full", 429, 120)
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["next_attempt_at"], 1120)
        self.now = 1119
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "backoff")
        self.capabilities.assert_called_once()
        self.now = 1120
        self.capabilities.side_effect = None
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "idle")
            self.assertEqual(self.worker(box).state()["available_at"], 1150)
        self.executor.assert_not_called()

    def test_competing_cursor_update_does_not_execute_stale_selection(self):
        with closing(self.open()) as box, closing(self.open()) as peer:
            one, two = self.worker(box), self.worker(peer)

            def discover(*args, **kwargs):
                self.assertTrue(two.save(two.state(), job_after=5))
                return [{"sequence": 1, "uuid": str(uuid.uuid4())}]

            self.native_ready.side_effect = discover
            self.assertEqual(one.once()["state"], "contended")
        self.executor.assert_not_called()

    def test_schema_eight_migration_preserves_staged_evidence_and_all_existing_tables(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            schema_eight(box.db)
            tables = [row[0] for row in box.db.execute("SELECT name FROM sqlite_schema WHERE type='table'")]
            before = {table: [tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '"')] for table in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            for table, rows in before.items():
                self.assertEqual([tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '"')], rows)
            self.assertEqual(EnrichmentJournal(box).find(value.job_uuid).body, value.body)
            schema_eight(box.db)
            box.db.execute("CREATE TABLE enrichment_dispatch(original BLOB)")
            box.db.execute("INSERT INTO enrichment_dispatch VALUES(?)", (b"retained",))
            with self.assertRaises(sqlite3.OperationalError):
                self.open()
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], 8)
            self.assertEqual(box.db.execute("SELECT original FROM enrichment_dispatch").fetchone()[0], b"retained")
