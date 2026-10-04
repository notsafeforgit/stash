from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.collection_dispatch import CollectionDispatcher
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.enrichment_client import EnrichmentClient
from stash_ingest.enrichment_dispatch import EnrichmentDispatcher
from stash_ingest.outbox import Outbox, SCHEMA as OUTBOX_SCHEMA
from stash_ingest.worker_dispatch import Profiles, WorkerDispatcher, SCHEMA
from helpers import PRODUCER
import test_enrichment_dispatch as dispatch_fixtures
from test_outbox_migration import schema_nine


class WorkerDispatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.path = self.directory / "outbox.sqlite"
        self.transport = SimpleNamespace(endpoint="http://fixture.invalid", producer=PRODUCER)
        self.now = 1000
        self.document = {"schema": SCHEMA, "uuid": str(uuid.uuid4()), "profiles": [
            {"id": "download", "operation": "download", "profile": "download.json"},
            {"id": "metadata", "operation": "post.enrich", "profile": "metadata.json"}]}
        self.profile_path = self.directory / "profiles.json"
        self.profile_path.write_bytes(encode(self.document))
        self.profiles = Profiles(self.profile_path)
        self.collection = str(uuid.uuid4())

    def open(self):
        return Outbox(self.path, self.transport.endpoint, PRODUCER, clock=lambda: self.now)

    def test_profile_list_is_bounded_unique_and_paths_are_local(self):
        self.assertEqual(self.profiles.entries[0]["profile"], self.directory / "download.json")
        self.assertFalse((self.directory / "download.json").exists(), "loading the list does not load website settings")
        for changes in ({"schema": "unknown"}, {"uuid": "bad"}, {"profiles": []},
                        {"profiles": self.document["profiles"] * 2}, {"profiles": [self.document["profiles"][0]] * 33},
                        {"extra": "unsupported"}):
            self.profile_path.write_bytes(encode({**self.document, **changes}))
            with self.subTest(changes=changes), self.assertRaises(InvalidData):
                Profiles(self.profile_path)

    def test_continuously_busy_download_does_not_starve_metadata_after_restart(self):
        with (patch("stash_ingest.worker_dispatch.Configuration") as download_profile,
              patch("stash_ingest.worker_dispatch.EnrichmentConfiguration") as metadata_profile,
              patch("stash_ingest.worker_dispatch.Dispatcher") as download,
              patch("stash_ingest.worker_dispatch.CollectionDispatcher") as metadata):
            download.return_value.once.return_value = {"state": "yielded"}
            metadata.return_value.once.return_value = {"state": "completed"}
            with closing(self.open()) as box:
                self.assertEqual(WorkerDispatcher(box, self.transport, self.profiles).once()["profile_id"], "download")
            metadata_profile.assert_not_called()
            with closing(self.open()) as box:
                self.assertEqual(WorkerDispatcher(box, self.transport, self.profiles).once()["profile_id"], "metadata")
            with closing(self.open()) as box:
                self.assertEqual(WorkerDispatcher(box, self.transport, self.profiles).once()["profile_id"], "download")
            self.assertEqual(download_profile.call_count, 2)
            self.assertEqual(metadata_profile.call_count, 1)

    def test_process_death_after_selection_still_advances_to_peer(self):
        with (patch("stash_ingest.worker_dispatch.Configuration"),
              patch("stash_ingest.worker_dispatch.EnrichmentConfiguration"),
              patch("stash_ingest.worker_dispatch.Dispatcher") as download,
              patch("stash_ingest.worker_dispatch.CollectionDispatcher") as metadata):
            download.return_value.once.side_effect = RuntimeError("process disappeared")
            with closing(self.open()) as box, self.assertRaises(RuntimeError):
                WorkerDispatcher(box, self.transport, self.profiles).once()
            metadata.return_value.once.return_value = {"state": "completed"}
            with closing(self.open()) as box:
                self.assertEqual(WorkerDispatcher(box, self.transport, self.profiles).once()["profile_id"], "metadata")

    def test_broken_profile_is_reported_without_paths_or_access_values_and_peers_run(self):
        with (patch("stash_ingest.worker_dispatch.Configuration", side_effect=InvalidData("fixture-private-access /private/path")),
              patch("stash_ingest.worker_dispatch.EnrichmentConfiguration"),
              patch("stash_ingest.worker_dispatch.CollectionDispatcher") as metadata):
            metadata.return_value.once.return_value = {"state": "completed"}
            with closing(self.open()) as box:
                result = WorkerDispatcher(box, self.transport, self.profiles).once()
                self.assertEqual(result["profile_id"], "metadata")
                self.assertEqual(result["profiles"], [{"profile_id": "download", "state": "unavailable", "error_code": "worker_profile_unavailable"}])
                self.assertNotIn("fixture-private-access", encode(result).decode())
                self.assertNotIn("/private/path", "\n".join(box.db.iterdump()))

    def test_saved_delivery_runs_without_loading_original_profile(self):
        with closing(self.open()) as box:
            value = dispatch_fixtures.EnrichmentDispatchTests.stage(self, box, policy="b" * 64)
            with (patch("stash_ingest.worker_dispatch.deliver_enrichment", return_value={"state": "ownership_required"}) as delivery,
                  patch("stash_ingest.worker_dispatch.Configuration", side_effect=OSError("missing")),
                  patch("stash_ingest.worker_dispatch.EnrichmentConfiguration", side_effect=OSError("missing"))):
                result = WorkerDispatcher(box, self.transport, self.profiles).once()
                delivery.assert_called_once_with(box, self.transport, None, value.job_uuid)
                self.assertEqual(result["state"], "waiting")
                self.assertEqual(box.db.execute("SELECT body FROM enrichment_executions WHERE job_uuid=?", (value.job_uuid,)).fetchone()[0], value.body)

    def test_competing_rotation_does_not_execute_an_outdated_profile(self):
        with closing(self.open()) as box, closing(self.open()) as peer:
            dispatch_fixtures.EnrichmentDispatchTests.stage(self, box)
            one, two = WorkerDispatcher(box, self.transport, self.profiles), WorkerDispatcher(peer, self.transport, self.profiles)
            def delivered(*args):
                self.assertTrue(two.save(two.state(), after_entry="metadata"))
                return {"state": "completed"}
            with (patch("stash_ingest.worker_dispatch.deliver_enrichment", side_effect=delivered),
                  patch("stash_ingest.worker_dispatch.Configuration") as profile):
                self.assertEqual(one.once()["state"], "contended")
                profile.assert_not_called()

    def test_schema_nine_promotion_preserves_existing_bodies_and_cursors(self):
        with closing(self.open()) as box:
            staged = dispatch_fixtures.EnrichmentDispatchTests.stage(self, box)
            EnrichmentDispatcher(box, self.transport, self.collection)
            schema_nine(box.db)
            tables = [row[0] for row in box.db.execute("SELECT name FROM sqlite_schema WHERE type='table'")]
            before = {table: [tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '"')] for table in tables}
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], OUTBOX_SCHEMA)
            for table, rows in before.items():
                self.assertEqual([tuple(r) for r in box.db.execute('SELECT * FROM "' + table + '"')], rows)
            self.assertEqual(box.db.execute("SELECT body FROM enrichment_executions WHERE job_uuid=?", (staged.job_uuid,)).fetchone()[0], staged.body)
            schema_nine(box.db)
            box.db.execute("CREATE TABLE worker_dispatch(original BLOB)")
            box.db.execute("INSERT INTO worker_dispatch VALUES(?)", (b"preserved",))
            with self.assertRaises(sqlite3.OperationalError):
                self.open()
            self.assertEqual(box.db.execute("PRAGMA user_version").fetchone()[0], 9)
            self.assertEqual(box.db.execute("SELECT original FROM worker_dispatch").fetchone()[0], b"preserved")
            self.assertIsNone(box.db.execute("SELECT name FROM sqlite_schema WHERE name='enrichment_collection_dispatch'").fetchone())


class CollectionDispatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.now = 1000
        self.transport = SimpleNamespace(endpoint="http://fixture.invalid", producer=PRODUCER)
        self.profile = SimpleNamespace(policy_sha256="a" * 64, extractor_version="1.32.15-dev", check=Mock())
        self.ids = sorted(str(uuid.uuid4()) for _ in range(3))
        self.capabilities = patch.object(EnrichmentClient, "capabilities", return_value={"enrichment_collections_protocol": 1}).start()
        self.pages = patch.object(EnrichmentClient, "ready_collections", side_effect=lambda *a, after, limit:
            [{"uuid": value} for value in self.ids if value > after][:limit]).start()
        self.selected = patch("stash_ingest.collection_dispatch.EnrichmentDispatcher").start()
        self.selected.return_value.once.return_value = {"state": "completed"}
        self.dispatcher = CollectionDispatcher
        self.addCleanup(patch.stopall)

    def open(self):
        return Outbox(self.path, self.transport.endpoint, PRODUCER, clock=lambda: self.now)

    def test_collection_rotation_survives_restarts_and_wraps_before_idle(self):
        for value in self.ids:
            with closing(self.open()) as box:
                self.assertEqual(self.dispatcher(box, self.transport, self.profile).once()["collection_uuid"], value)
        with closing(self.open()) as box:
            self.assertEqual(self.dispatcher(box, self.transport, self.profile).once()["state"], "waiting")
        with closing(self.open()) as box:
            self.assertEqual(self.dispatcher(box, self.transport, self.profile).once()["collection_uuid"], self.ids[0])

    def test_blocked_collection_does_not_prevent_a_peer_from_running(self):
        self.selected.return_value.once.side_effect = [{"state": "waiting"}, {"state": "completed"}]
        with closing(self.open()) as box:
            self.assertEqual(self.dispatcher(box, self.transport, self.profile).once()["collection_uuid"], self.ids[1])

    def test_discovery_outage_keeps_cursor_and_backoff_across_restart(self):
        self.pages.side_effect = Unavailable("native_unavailable", 503, 120)
        with closing(self.open()) as box:
            worker = self.dispatcher(box, self.transport, self.profile)
            worker.save(worker.state(), after_collection=self.ids[1])
            self.assertEqual(worker.once()["next_attempt_at"], 1120)
        self.now = 1119
        with closing(self.open()) as box:
            worker = self.dispatcher(box, self.transport, self.profile)
            self.assertEqual(worker.once()["state"], "backoff")
            self.assertEqual(worker.state()["after_collection"], self.ids[1])
        self.selected.assert_not_called()

    def test_competing_cursor_cannot_execute_an_outdated_selection(self):
        with closing(self.open()) as box, closing(self.open()) as peer:
            one, two = self.dispatcher(box, self.transport, self.profile), self.dispatcher(peer, self.transport, self.profile)
            def discover(*args, **kwargs):
                self.assertTrue(two.save(two.state(), after_collection=self.ids[1]))
                return [{"uuid": self.ids[0]}]
            self.pages.side_effect = discover
            self.assertEqual(one.once()["state"], "contended")
        self.selected.assert_not_called()


class CollectionClientTests(unittest.TestCase):
    def test_rejects_duplicate_reversed_foreign_shapes_and_invalid_cursors(self):
        ids = sorted(str(uuid.uuid4()) for _ in range(2))
        transport = SimpleNamespace(_request=Mock())
        client = EnrichmentClient(transport)
        good = [{"uuid": value} for value in ids]
        for page in ([good[0], good[0]], list(reversed(good)), [{"uuid": "bad"}], [{**good[0], "url": "private"}], {}, good * 11):
            transport._request.return_value = page
            with self.subTest(page=page), self.assertRaises(Unavailable):
                client.ready_collections("a" * 64, "fixture")
        transport._request.return_value = good
        with self.assertRaises(Unavailable):
            client.ready_collections("a" * 64, "fixture", after=ids[0])
        transport._request.return_value = good
        self.assertEqual(client.ready_collections("a" * 64, "fixture"), good)


if __name__ == "__main__":
    unittest.main()
