from contextlib import closing
import hashlib
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.discovery_client import DiscoveryClient, LISTING_KEYS
from stash_ingest.discovery_dispatch import DiscoveryDispatcher
from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.encoding import native_json
from stash_ingest.outbox import Outbox, SCHEMA
from helpers import PRODUCER
from test_discovery_client import execution_fixture
from test_outbox_migration import schema_eleven


class DiscoveryDispatchTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.path = Path(temp.name) / "outbox.sqlite"
        self.now = 1000
        self.collection = str(uuid.uuid4())
        self.transport = SimpleNamespace(endpoint="http://fixture.invalid", producer=PRODUCER)
        self.profile = SimpleNamespace(policy_sha256="a" * 64, extractor_version="1.32.15-dev",
            operation="account.list_page", check=Mock())
        self.execute = patch("stash_ingest.discovery_dispatch.execute", return_value={"state": "waiting"}).start()
        self.ready = patch.object(DiscoveryClient, "ready_jobs", return_value=[]).start()
        self.listings = patch.object(DiscoveryClient, "ready_listings", return_value={"listings": [], "after": "", "has_more": False}).start()
        self.admit = patch.object(DiscoveryClient, "admit", return_value={"uuid": str(uuid.uuid4())}).start()
        self.describe = patch.object(DiscoveryClient, "describe", return_value={"job": {}}).start()
        self.caps = patch.object(DiscoveryClient, "capabilities", return_value={"discovery_readiness_protocol": 1, "discovery_dispatch_protocol": 1}).start()
        self.addCleanup(patch.stopall)

    def open(self):
        return Outbox(self.path, self.transport.endpoint, PRODUCER, clock=lambda: self.now)

    def worker(self, box, *, profile=True):
        return DiscoveryDispatcher(box, self.transport, self.collection, self.profile if profile else None)

    def candidate(self):
        return {"uuid": str(uuid.uuid4()), "definition_sha256": "b" * 64}

    def stage(self, box, policy="a" * 64):
        description, page = execution_fixture()
        listing = description["listing"]
        listing.update(collection_uuid=self.collection, policy_sha256=policy)
        listing["sha256"] = hashlib.sha256(native_json({k: listing[k] for k in LISTING_KEYS}, 32768)).hexdigest()
        description["job"]["arguments"].update(collection_uuid=self.collection, definition_sha256=listing["sha256"])
        journal = DiscoveryJournal(box)
        with journal.execution():
            value = journal.claim(journal.prepare(description), description)
            lease = {**description["job"], "state": "running", "revision": 2, "fence": 1,
                "owner_uuid": value.state["claim"]["owner_uuid"], "lease_until": "2026-10-04T14:03:00Z"}
            value = journal.reserve(journal.claimed(value, lease))
            return journal.page(value, lease, page)

    def test_empty_filtered_pages_advance_across_restart_before_idle(self):
        first, second = sorted([str(uuid.uuid4()), str(uuid.uuid4())])
        self.listings.side_effect = [
            {"listings": [], "after": first, "has_more": True},
            {"listings": [], "after": second, "has_more": False}]
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "waiting")
            self.assertEqual(self.worker(box).state()["listing_after"], first)
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "idle")
            self.assertEqual(self.worker(box).state()["listing_after"], "")
            self.assertEqual(self.worker(box).state()["available_at"], 1030)
        self.assertEqual(self.listings.call_args.kwargs["after"], first)
        self.admit.assert_not_called()

    def test_lost_admission_is_recovered_as_an_admitted_job(self):
        candidate = self.candidate()
        self.listings.return_value = {"listings": [candidate], "after": candidate["uuid"], "has_more": False}
        self.admit.side_effect = Unavailable("network_unavailable")
        with closing(self.open()) as box:
            result = self.worker(box).once()
            self.assertEqual(result["state"], "unavailable")
            self.assertEqual(self.worker(box).state()["listing_after"], candidate["uuid"])
        self.now = result["next_attempt_at"]
        job = str(uuid.uuid4())
        self.ready.return_value = [{"uuid": job, "sequence": 1}]
        self.execute.return_value = {"state": "page_delivered", "job_uuid": job}
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["job_uuid"], job)
        self.admit.assert_called_once_with(candidate["uuid"], candidate["definition_sha256"], self.profile.policy_sha256, self.profile.extractor_version)

    def test_saved_page_from_an_old_profile_precedes_all_new_source_work(self):
        with closing(self.open()) as box:
            value = self.stage(box, "b" * 64)
            self.execute.return_value = {"state": "delivery_pending"}
            result = self.worker(box).once()
            self.assertEqual(result["state"], "delivery_pending")
            self.assertEqual(result["dispatch"]["state"], "unavailable")
            self.execute.assert_called_once_with(box, self.transport, None, value.job_uuid)

            self.execute.reset_mock(side_effect=True)
            self.describe.return_value = {"job": {"execution_policy_sha256": self.profile.policy_sha256}}
            self.execute.side_effect = [{"state": "ownership_required"}, {"state": "page_delivered"}]
            self.now = result["dispatch"]["next_attempt_at"]
            self.assertEqual(self.worker(box).once()["state"], "page_delivered")
            self.assertIs(self.execute.call_args_list[1].args[2], self.profile)
            self.assertEqual(DiscoveryJournal(box).find(value.job_uuid).body, value.body)
        self.ready.assert_not_called()
        self.listings.assert_not_called()
        self.profile.check.assert_not_called()

    def test_expired_saved_page_requires_its_original_profile_for_new_ownership(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            self.execute.side_effect = [{"state": "ownership_required"}, {"state": "page_delivered"}]
            self.assertEqual(self.worker(box).once()["state"], "page_delivered")
            self.assertIsNone(self.execute.call_args_list[0].args[2])
            self.assertIs(self.execute.call_args_list[1].args[2], self.profile)
            self.execute.reset_mock(side_effect=True)
            self.profile.policy_sha256 = "c" * 64
            self.execute.return_value = {"state": "ownership_required"}
            self.assertEqual(self.worker(box).once()["state"], "profile_required")
            self.execute.assert_called_once_with(box, self.transport, None, value.job_uuid)

    def test_delivery_only_does_not_discover_or_require_a_profile(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            self.execute.return_value = {"state": "page_delivered"}
            self.assertEqual(self.worker(box, profile=False).once()["state"], "page_delivered")
            self.execute.assert_called_once_with(box, self.transport, None, value.job_uuid)
        self.caps.assert_not_called()
        self.ready.assert_not_called()
        self.listings.assert_not_called()

    def test_blocked_admitted_jobs_do_not_fill_the_queue_with_more_listings(self):
        self.ready.return_value = [{"uuid": str(uuid.uuid4()), "sequence": 1}]
        for _ in range(2):
            with closing(self.open()) as box:
                self.assertEqual(self.worker(box).once()["state"], "waiting")
                self.assertEqual(self.worker(box).state()["job_after"], 0)
        self.listings.assert_not_called()
        self.admit.assert_not_called()

    def test_process_death_wraps_the_saved_job_cursor_before_admitting(self):
        candidate = {"uuid": str(uuid.uuid4()), "sequence": 8}
        self.ready.return_value = [candidate]
        self.execute.side_effect = RuntimeError("process stopped")
        with closing(self.open()) as box:
            with self.assertRaises(RuntimeError):
                self.worker(box).once()
        self.ready.return_value = []
        self.execute.side_effect = None
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "waiting")
            self.assertEqual(self.worker(box).state()["job_after"], 0)
        self.listings.assert_not_called()
        self.ready.return_value = [candidate]
        self.execute.return_value = {"state": "page_delivered"}
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "page_delivered")

    def test_competing_cursor_cannot_execute_an_outdated_selection(self):
        self.ready.return_value = [{"uuid": str(uuid.uuid4()), "sequence": 1}]
        with closing(self.open()) as box:
            first, second = self.worker(box), self.worker(box)
            def competing(*args, **kwargs):
                second.save(second.state(), job_after=3)
                return [{"uuid": str(uuid.uuid4()), "sequence": 2}]
            self.ready.side_effect = competing
            self.assertEqual(first.once()["state"], "contended")
        self.execute.assert_not_called()
        self.listings.assert_not_called()

    def test_idle_and_server_backoff_survive_restart(self):
        self.caps.side_effect = Unavailable("queue_full", 429, 120)
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["next_attempt_at"], 1120)
        self.now = 1119
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "backoff")
        self.caps.assert_called_once()
        self.now = 1120
        self.caps.side_effect = None
        with closing(self.open()) as box:
            self.assertEqual(self.worker(box).once()["state"], "idle")
            self.assertEqual(self.worker(box).state()["available_at"], 1150)

    def test_dispatch_requires_explicit_server_support_before_discovery(self):
        for version in (None, True, 2):
            with closing(self.open()) as box:
                self.caps.return_value = {"discovery_dispatch_protocol": version}
                self.now += 1000
                result = self.worker(box).once()
                self.assertEqual(result["state"], "unavailable")
                self.assertEqual(result["error_code"], "native_discovery_dispatch_unavailable")
        self.ready.assert_not_called()
        self.listings.assert_not_called()

    def test_schema_eleven_upgrade_preserves_every_table_and_pending_page(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            schema_eleven(box.db)
            tables = [r[0] for r in box.db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")]
            original = {table: [tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '" ORDER BY rowid')] for table in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            for table, rows in original.items():
                self.assertEqual([tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '" ORDER BY rowid')], rows, table)
            self.assertEqual(DiscoveryJournal(box).find(value.job_uuid), value)
            self.assertEqual(box.db.execute("SELECT count(*) FROM discovery_dispatch").fetchone()[0], 0)

    def test_schema_collision_rolls_back_without_discarding_pending_evidence(self):
        with closing(self.open()) as box:
            value = self.stage(box)
            schema_eleven(box.db)
            box.db.execute("CREATE TABLE discovery_dispatch(original TEXT)")
            box.db.execute("INSERT INTO discovery_dispatch VALUES('retain unknown evidence')")
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        with closing(sqlite3.connect(self.path)) as db:
            self.assertEqual(db.execute("PRAGMA user_version").fetchone()[0], 11)
            self.assertEqual(db.execute("SELECT * FROM discovery_dispatch").fetchall(), [("retain unknown evidence",)])
            self.assertEqual(db.execute("SELECT body FROM discovery_executions WHERE job_uuid=?", (value.job_uuid,)).fetchone()[0], value.body)
