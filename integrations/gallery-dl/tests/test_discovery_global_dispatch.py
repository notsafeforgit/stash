from contextlib import closing, ExitStack
import sqlite3
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.discovery_client import DiscoveryClient
from stash_ingest.discovery_collection_dispatch import DiscoveryCollectionDispatcher
from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.outbox import SCHEMA
from stash_ingest.worker_dispatch import Profiles, WorkerDispatcher
import test_discovery_dispatch as discovery_fixtures
import test_enrichment_dispatch as enrichment_fixtures
import test_worker_dispatch as worker_fixtures
from test_outbox_migration import schema_twelve


class DiscoveryCollectionDispatchTests(worker_fixtures.CollectionDispatchTests):
    def setUp(self):
        super().setUp()
        self.profile.operation = "account.list_page"
        self.capabilities = patch.object(DiscoveryClient, "capabilities", return_value={"discovery_collections_protocol": 1}).start()
        self.pages = patch.object(DiscoveryClient, "ready_collections", side_effect=lambda after, limit:
            [{"uuid": value} for value in self.ids if value > after][:limit]).start()
        self.selected = patch("stash_ingest.discovery_collection_dispatch.DiscoveryDispatcher").start()
        self.selected.return_value.once.return_value = {"state": "completed"}
        self.dispatcher = DiscoveryCollectionDispatcher


class GlobalDiscoveryDispatchTests(unittest.TestCase):
    open = worker_fixtures.WorkerDispatchTests.open

    def setUp(self):
        worker_fixtures.WorkerDispatchTests.setUp(self)
        self.document["profiles"].append({"id": "discovery", "operation": "account.list_page", "profile": "discovery.json"})
        self.profile_path.write_bytes(encode(self.document))
        self.profiles = Profiles(self.profile_path)

    def patched_profiles(self, stack):
        result = {}
        for name, state in (("Dispatcher", "source_succeeded"), ("CollectionDispatcher", "completed"),
                            ("DiscoveryCollectionDispatcher", "page_delivered")):
            value = stack.enter_context(patch("stash_ingest.worker_dispatch." + name))
            value.return_value.once.return_value = {"state": state}
            result[name] = value
        for name in ("Configuration", "EnrichmentConfiguration", "DiscoveryConfiguration"):
            stack.enter_context(patch("stash_ingest.worker_dispatch." + name))
        return result

    def test_all_three_busy_operations_rotate_across_process_reopens(self):
        with ExitStack() as stack:
            mocked = self.patched_profiles(stack)
            for expected in ("download", "metadata", "discovery") * 2:
                with closing(self.open()) as box:
                    result = WorkerDispatcher(box, self.transport, self.profiles).once()
                    self.assertEqual(result["profile_id"], expected)
            for value in mocked.values():
                self.assertEqual(value.return_value.once.call_count, 2)

    def test_both_saved_deliveries_run_without_their_missing_profiles(self):
        with closing(self.open()) as box:
            enriched = enrichment_fixtures.EnrichmentDispatchTests.stage(self, box)
            discovered = discovery_fixtures.DiscoveryDispatchTests.stage(self, box)
            with ExitStack() as stack:
                for name in ("Configuration", "EnrichmentConfiguration", "DiscoveryConfiguration"):
                    stack.enter_context(patch("stash_ingest.worker_dispatch." + name, side_effect=OSError("private fixture path")))
                enrich = stack.enter_context(patch("stash_ingest.worker_dispatch.deliver_enrichment", return_value={"state": "completed"}))
                discover = stack.enter_context(patch("stash_ingest.worker_dispatch.deliver_discovery", return_value={"state": "page_delivered"}))
                worker = WorkerDispatcher(box, self.transport, self.profiles)
                result = worker.once()
                enrich.assert_called_once_with(box, self.transport, None, enriched.job_uuid)
                discover.assert_called_once_with(box, self.transport, None, discovered.job_uuid)
                self.assertEqual(result["state"], "waiting")
                self.assertNotIn("private fixture path", encode(result).decode())
                state = worker.state()
                self.assertEqual(state["enrichment_delivery_after"], enriched.job_uuid)
                self.assertEqual(state["discovery_delivery_after"], discovered.job_uuid)
                self.assertEqual(DiscoveryJournal(box).find(discovered.job_uuid).body, discovered.body)
        with closing(self.open()) as box:
            self.assertEqual(WorkerDispatcher(box, self.transport, self.profiles).state(), state)

    def test_process_death_after_discovery_selection_advances_to_download(self):
        with ExitStack() as stack:
            mocked = self.patched_profiles(stack)
            mocked["DiscoveryCollectionDispatcher"].return_value.once.side_effect = RuntimeError("process stopped")
            with closing(self.open()) as box:
                worker = WorkerDispatcher(box, self.transport, self.profiles)
                worker.save(worker.state(), after_entry="metadata")
                with self.assertRaises(RuntimeError):
                    worker.once()
            with closing(self.open()) as box:
                self.assertEqual(WorkerDispatcher(box, self.transport, self.profiles).once()["profile_id"], "download")

    def test_competing_rotation_keeps_delivered_receipt_but_prevents_new_selection(self):
        with closing(self.open()) as box, closing(self.open()) as peer:
            discovery_fixtures.DiscoveryDispatchTests.stage(self, box)
            first, second = WorkerDispatcher(box, self.transport, self.profiles), WorkerDispatcher(peer, self.transport, self.profiles)
            def delivered(*args):
                second.save(second.state(), after_entry="metadata")
                return {"state": "page_delivered", "receipt": "original"}
            with (patch("stash_ingest.worker_dispatch.deliver_discovery", side_effect=delivered),
                  patch("stash_ingest.worker_dispatch.Configuration") as profile):
                result = first.once()
                self.assertEqual(result["state"], "contended")
                self.assertEqual(result["discovery_delivery"]["receipt"], "original")
                profile.assert_not_called()

    def test_schema_twelve_preserves_both_journals_and_original_rotation(self):
        with closing(self.open()) as box:
            discovered = discovery_fixtures.DiscoveryDispatchTests.stage(self, box)
            enrichment_fixtures.EnrichmentDispatchTests.stage(self, box)
            worker = WorkerDispatcher(box, self.transport, self.profiles)
            cursor = str(uuid.uuid4())
            worker.save(worker.state(), after_entry="metadata", enrichment_delivery_after=cursor)
            schema_twelve(box.db)
            tables = [r[0] for r in box.db.execute("SELECT name FROM sqlite_schema WHERE type='table' AND name!='worker_dispatch'")]
            original = {table: [tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '"')] for table in tables}
            old = dict(box.db.execute("SELECT * FROM worker_dispatch").fetchone())
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
            for table, rows in original.items():
                self.assertEqual([tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '"')], rows, table)
            current = WorkerDispatcher(box, self.transport, self.profiles).state()
            expected = {**old, "enrichment_delivery_after": old["delivery_after"], "discovery_delivery_after": "",
                        "discovery_detail_delivery_after": ""}
            del expected["delivery_after"]
            self.assertEqual(current, expected)
            self.assertEqual(DiscoveryJournal(box).find(discovered.job_uuid), discovered)
            self.assertEqual(box.db.execute("SELECT count(*) FROM discovery_collection_dispatch").fetchone()[0], 0)

    def test_failed_upgrade_rolls_back_renamed_columns_and_preserves_unknown_data(self):
        with closing(self.open()) as box:
            discovery_fixtures.DiscoveryDispatchTests.stage(self, box)
            worker = WorkerDispatcher(box, self.transport, self.profiles)
            worker.save(worker.state(), enrichment_delivery_after=str(uuid.uuid4()))
            schema_twelve(box.db)
            box.db.execute("ALTER TABLE worker_dispatch ADD COLUMN discovery_delivery_after TEXT NOT NULL DEFAULT 'retain unknown evidence'")
            original = dict(box.db.execute("SELECT * FROM worker_dispatch").fetchone())
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        with closing(sqlite3.connect(self.path)) as db:
            db.row_factory = sqlite3.Row
            self.assertEqual(db.execute("PRAGMA user_version").fetchone()[0], 12)
            self.assertEqual(dict(db.execute("SELECT * FROM worker_dispatch").fetchone()), original)
            self.assertIsNone(db.execute("SELECT name FROM sqlite_schema WHERE name='discovery_collection_dispatch'").fetchone())


class DiscoveryCollectionClientTests(unittest.TestCase):
    def test_scoped_collection_pages_reject_duplicates_reversal_and_unbounded_results(self):
        ids = sorted(str(uuid.uuid4()) for _ in range(2))
        transport = SimpleNamespace(_request=Mock())
        client = DiscoveryClient(transport)
        good = [{"uuid": value} for value in ids]
        for value in ([good[0], good[0]], list(reversed(good)), [{"uuid": "invalid"}], [{**good[0], "title": "not a summary"}], {}, good * 11):
            transport._request.return_value = value
            with self.assertRaises(Unavailable):
                client.ready_collections()
        transport._request.return_value = good
        self.assertEqual(client.ready_collections(), good)
        with self.assertRaises(Unavailable):
            client.ready_collections(after=ids[0])
        for cursor in (None, False, "invalid"):
            with self.assertRaises(InvalidData):
                client.ready_collections(after=cursor)
