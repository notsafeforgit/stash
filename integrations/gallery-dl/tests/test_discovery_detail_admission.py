import sqlite3
from types import SimpleNamespace
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.discovery_detail_client import DiscoveryDetailClient
from stash_ingest.discovery_detail_dispatch import DiscoveryDetailDispatcher, DiscoveryDetailCollectionDispatcher
from stash_ingest.encoding import InvalidData, encode, decode
from stash_ingest.outbox import Outbox, SCHEMA
from helpers import PRODUCER, capture
from test_discovery_detail_execution import DetailFixture


class DetailAdmissionTests(DetailFixture):
    def setUp(self):
        super().setUp()
        self.transport = Mock(endpoint=self.box.endpoint, producer=PRODUCER)
        self.client = DiscoveryDetailClient(self.transport)
        work = self.job["arguments"]
        self.collection = work["collection_uuid"]
        self.cursor = {"listing_uuid": work["listing_uuid"], "source_ordinal": 2}
        self.candidate = {"cursor": self.cursor, "target_uuid": work["target_uuid"],
                          "target_revision": work["target_revision"], "candidate_sequence": work["candidate_sequence"],
                          "post_namespace": work["post_namespace"], "post_value": work["post_value"], "url": work["url"]}
        self.page = {"candidates": [self.candidate], "after": self.cursor, "has_more": True}
        self.profile = SimpleNamespace(operation="post.verify_candidate", policy_sha256="a" * 64,
                                       extractor_version=work["extractor_version"], check=Mock(), accepts=Mock(return_value=True))
        self.caps = {"discovery_detail_protocol": 1, "discovery_detail_admission_protocol": 1}

    def worker(self):
        return DiscoveryDetailDispatcher(self.box, self.transport, self.collection, self.profile)

    def test_candidate_pages_validate_order_bounds_scope_and_progress(self):
        self.transport._request.return_value = self.page
        self.assertEqual(self.client.candidates(self.collection, 1), self.page)
        self.assertTrue(self.transport._request.call_args.args[1].endswith("/candidates"))
        variants = [None, {}, dict(self.page, has_more=1), dict(self.page, after=None),
                    dict(self.page, candidates=[self.candidate, self.candidate]),
                    dict(self.page, after={**self.cursor, "source_ordinal": 1})]
        for change in ({"target_revision": True}, {"candidate_sequence": False}, {"extra": "private"},
                       {"target_uuid": "invalid"}, {"url": "https://www.reddit.com/comments/different"},
                       {"post_namespace": "native:instagram"},
                       {"cursor": {**self.cursor, "source_ordinal": 0}}):
            variants.append(dict(self.page, candidates=[{**self.candidate, **change}]))
        for bad in variants:
            self.transport._request.return_value = bad
            with self.subTest(value=bad), self.assertRaises(Unavailable):
                self.client.candidates(self.collection, 1)
        self.transport._request.return_value = self.page
        with self.assertRaises(Unavailable):
            self.client.candidates(self.collection, after=self.cursor)
        self.transport._request.return_value = {"candidates": [], "after": self.cursor, "has_more": False}
        self.assertFalse(self.client.candidates(self.collection, after=self.cursor)["has_more"])
        self.transport._request.return_value["has_more"] = True
        with self.assertRaises(Unavailable):
            self.client.candidates(self.collection, after=self.cursor)
        for cursor in ([], {}, {"listing_uuid": "invalid", "source_ordinal": 0}, {**self.cursor, "source_ordinal": True}):
            with self.assertRaises(InvalidData):
                self.client.candidates(self.collection, after=cursor)

    def test_filtered_pages_and_incompatible_urls_advance_across_restart(self):
        self.profile.accepts.return_value = False
        with patch.object(DiscoveryDetailClient, "capabilities", return_value=self.caps), \
                patch.object(DiscoveryDetailClient, "ready_jobs", return_value=[]), \
                patch.object(DiscoveryDetailClient, "candidates", return_value=self.page) as candidates, \
                patch.object(DiscoveryDetailClient, "admit") as admit:
            self.assertEqual(self.worker().once()["state"], "waiting")
            admit.assert_not_called()
            self.reopen()
            later = {**self.cursor, "source_ordinal": 7}
            candidates.return_value = {"candidates": [], "after": later, "has_more": True}
            self.assertEqual(self.worker().once()["state"], "waiting")
            self.assertEqual(candidates.call_args.kwargs["after"], self.cursor)
            self.assertEqual(self.worker().state()["target_cursor"], later)
            candidates.return_value = {"candidates": [], "after": later, "has_more": False}
            self.assertEqual(self.worker().once()["state"], "idle")
            self.reopen()
            self.assertIsNone(self.worker().state()["target_cursor"])
            self.assertEqual(self.worker().once()["state"], "backoff")

    def test_lost_admission_keeps_cursor_and_recovers_the_original_ready_job_first(self):
        with patch.object(DiscoveryDetailClient, "capabilities", return_value=self.caps), \
                patch.object(DiscoveryDetailClient, "ready_jobs", return_value=[]) as jobs, \
                patch.object(DiscoveryDetailClient, "candidates", return_value=self.page) as candidates, \
                patch.object(DiscoveryDetailClient, "admit", side_effect=Unavailable("network_error")) as admit, \
                patch("stash_ingest.discovery_detail_dispatch.execute", return_value={"state": "completed"}) as execute:
            self.assertEqual(self.worker().once()["state"], "unavailable")
            self.assertTrue(admit.call_args.kwargs["automatic"])
            self.assertEqual(self.worker().state()["target_cursor"], self.cursor)
            execute.assert_not_called()
            self.reopen()
            self.assertEqual(self.worker().once()["state"], "backoff")
            self.now += 60
            jobs.return_value = [{"sequence": 1, "uuid": self.job["uuid"]}]
            self.assertEqual(self.worker().once()["state"], "completed")
            execute.assert_called_once_with(self.box, self.transport, self.profile, self.job["uuid"])
            self.assertEqual(admit.call_count, 1)
            self.assertEqual(candidates.call_count, 1)

    def test_server_conflict_and_competing_cursor_never_execute(self):
        with patch.object(DiscoveryDetailClient, "capabilities", return_value=self.caps), \
                patch.object(DiscoveryDetailClient, "ready_jobs", return_value=[]), \
                patch.object(DiscoveryDetailClient, "candidates", return_value=self.page), \
                patch.object(DiscoveryDetailClient, "admit", side_effect=Unavailable("conflict", status=409)) as admit, \
                patch("stash_ingest.discovery_detail_dispatch.execute") as execute:
            worker = self.worker()
            self.assertEqual(worker.once()["state"], "waiting")
            execute.assert_not_called()
            self.assertEqual(admit.call_count, 1)
            with patch.object(worker, "save", return_value=False):
                self.assertEqual(worker.once()["state"], "contended")
            self.assertEqual(admit.call_count, 1)

    def test_admission_requires_capability_and_inspection_uses_its_own_route(self):
        self.transport._request.return_value = [{"uuid": self.collection}]
        self.client.ready_collections(self.profile.policy_sha256, self.profile.extractor_version)
        self.assertTrue(self.transport._request.call_args.args[1].endswith("/collections/inspect"))
        self.transport._request.return_value = self.job
        self.client.admit(self.candidate["target_uuid"], self.candidate["target_revision"], self.candidate["candidate_sequence"],
                          self.profile.policy_sha256, self.profile.extractor_version, automatic=True)
        self.assertIs(decode(self.transport._request.call_args.args[2])["automatic"], True)
        with patch.object(DiscoveryDetailClient, "capabilities", return_value={"discovery_detail_protocol": 1}), \
                patch.object(DiscoveryDetailClient, "ready_jobs", return_value=[]), \
                patch.object(DiscoveryDetailClient, "candidates") as candidates:
            self.assertEqual(self.worker().once()["error_code"], "native_discovery_detail_admission_unavailable")
            candidates.assert_not_called()

    def test_multiple_container_pages_pause_after_a_complete_inspection_pass(self):
        containers = [{"uuid": str(uuid.uuid4())} for _ in range(21)]
        containers.sort(key=lambda row: row["uuid"])
        with patch.object(DiscoveryDetailClient, "capabilities", return_value=self.caps), \
                patch.object(DiscoveryDetailClient, "ready_collections", side_effect=[containers[:20], containers[20:]]) as ready, \
                patch.object(DiscoveryDetailDispatcher, "once", return_value={"state": "idle"}):
            worker = DiscoveryDetailCollectionDispatcher(self.box, self.transport, self.profile)
            self.assertEqual(worker.once()["state"], "waiting")
            self.reopen()
            worker = DiscoveryDetailCollectionDispatcher(self.box, self.transport, self.profile)
            self.assertEqual(worker.once()["state"], "waiting")
            self.assertEqual(ready.call_args.kwargs["after"], containers[19]["uuid"])
            self.assertEqual(worker.once()["state"], "backoff")
            self.assertEqual(ready.call_count, 2)


class DetailAdmissionMigrationTests(DetailFixture):
    def test_schema_fourteen_promotion_keeps_all_tables_and_pending_detail_bytes(self):
        with self.journal.execution():
            pending = self.stage()
        self.box.enqueue(encode(capture()))
        collection = self.job["arguments"]["collection_uuid"]
        self.box.db.execute("""INSERT INTO discovery_detail_dispatch(collection_uuid,policy_sha256,
            delivery_after,local_after,job_after,revision,failures,available_at,error_code)
            VALUES(?,?,?,?,?,?,?,?,?)""", (collection, "a" * 64, self.job["uuid"], self.job["uuid"], 123, 7, 3, 5000, "network_error"))
        self.box.db.execute("INSERT INTO worker_dispatch(uuid,after_entry,discovery_detail_delivery_after) VALUES(?,?,?)",
                            (str(uuid.uuid4()), "details", self.job["uuid"]))
        self.box.db.execute("ALTER TABLE discovery_detail_dispatch DROP COLUMN target_cursor")
        self.box.db.execute("PRAGMA user_version=14")
        tables = [r[0] for r in self.box.db.execute("SELECT name FROM sqlite_schema WHERE type='table'")]
        columns = {name: ','.join(r[1] for r in self.box.db.execute('PRAGMA table_info("'+name+'")')) for name in tables}
        def rows():
            return {name: [tuple(r) for r in self.box.db.execute('SELECT '+columns[name]+' FROM "'+name+'" ORDER BY rowid')] for name in tables}
        before = rows()
        self.reopen()
        self.assertEqual(before, rows())
        self.assertEqual(self.journal.find(pending.job_uuid), pending)
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
        self.assertIsNone(self.box.db.execute("SELECT target_cursor FROM discovery_detail_dispatch").fetchone()[0])
        self.assertEqual(self.box.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_conflicting_column_rolls_back_without_discarding_pending_evidence(self):
        with self.journal.execution():
            pending = self.stage()
        self.box.db.execute("PRAGMA user_version=14")
        with self.assertRaises(sqlite3.OperationalError):
            Outbox(self.path, self.box.endpoint, PRODUCER)
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], 14)
        self.assertEqual(self.journal.find(pending.job_uuid), pending)
