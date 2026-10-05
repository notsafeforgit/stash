from contextlib import closing
from datetime import datetime, timedelta, timezone
import hashlib
import json
import sqlite3
from unittest.mock import Mock, patch
import uuid

from stash_archive.bundle import export_archive, import_archive, snapshot_database
from stash_archive.host_boundary import HostFilesystemCapture
from stash_archive.server_checkpoint import FORMAT, COVERAGE, ROLES
from stash_archive.storage import InvalidArchive, json_bytes
from stash_ingest.encoding import encode
from test_receipts import ReceiptFixture, ORIGIN


class ComponentStageTests(ReceiptFixture):
    def setUp(self):
        super().setUp()
        self.db.execute("CREATE TABLE file_deletions(id TEXT PRIMARY KEY)")
        self.db.commit()
        self.worker = self.root / "locks"
        self.cache = self.root / "stages"
        self.worker.mkdir()
        self.cache.mkdir(mode=0o700)
        self.downloads, self.profile = self.root / "archive.sqlite", self.root / "profile.json"
        self.profile.write_bytes(b'{"profile":"original","private":"retained"}\n')
        with closing(sqlite3.connect(self.downloads)) as db:
            db.execute("CREATE TABLE archive(entry TEXT PRIMARY KEY)")
            db.execute("INSERT INTO archive VALUES('original')")
            db.commit()
        self.components = [{"role": "producer_outbox", "name": "queue.sqlite", "path": self.outbox},
                           {"role": "download_archive", "name": "downloads.sqlite", "path": self.downloads},
                           {"role": "worker_profile", "name": "profile.json", "path": self.profile}]
        self.request_id = str(uuid.uuid4())
        self.artwork, self.media = Mock(), Mock()
        self.artwork.capture.return_value, self.media.capture.return_value = {"pinned": True}, {"retained": True}
        self.native, self.captured, self.server_record, self.receipt = self.root / "fixed-native.sqlite", None, None, None
        self.creates = 0

    def host(self):
        return HostFilesystemCapture([self.worker], self.artwork, self.media)

    def client(self, host):
        client = host.client(ORIGIN, "fixture-key", self.request_id)
        # This fixture uses real SQLite snapshots, producer queues and receipt
        # checks. Actual transport/GET-only behavior is covered in HTTP tests.
        def seal(*, reserve):
            if client.existing_only:
                if self.server_record is None:
                    client.release_boundary()
                    raise InvalidArchive("Native checkpoint request failed (HTTP 404)")
            else:
                assert host.barrier.acquired
                assert self.server_record is None
                self.creates += 1
                ready = {"uuid": client.request_id, "token": str(uuid.uuid4()), "request_sha256": client.request_hash(reserve),
                         "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
                self.receipt = {**{k: ready[k] for k in ("uuid", "token", "request_sha256")}, "details": host(ready)}
                metadata = snapshot_database(self.library, self.native, "library", reserve)
                payloads = {"library.sqlite": self.native.read_bytes(), "config.yml": b"retained native config",
                            "runtime-overrides.yml": b"{}", "deletions.zip": b"opaque recovery fixture",
                            "filesystem-boundary.json": json_bytes(self.receipt)}
                self.server_record = {"format": FORMAT, "version": 1, "uuid": client.request_id, "coverage": COVERAGE,
                                      "created_at": "2026-10-05T00:00:00Z", "request_sha256": client.request_hash(reserve),
                                      "source_database_path": None, "source_config_path": None, "source_working_directory": None,
                                      "committed_deletion_ids": [], "components": []}
                components, expected = [], {}
                for name, data in payloads.items():
                    role = ROLES[name]
                    self.server_record["components"].append({"role": role, "name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()})
                    if role != "library":
                        path = self.root / ("fixed-" + name)
                        path.write_bytes(data)
                        components.append({"role": role, "name": name, "path": path})
                    expected[(role, "library" if role == "library" else name)] = (hashlib.sha256(data).hexdigest(), len(data))
                checkpoint = self.root / "fixed-server-checkpoint.json"
                checkpoint.write_bytes(json_bytes(self.server_record))
                components.append({"role": "operating_state", "name": "server-checkpoint.json", "path": checkpoint})
                expected[("operating_state", "server-checkpoint.json")] = (hashlib.sha256(checkpoint.read_bytes()).hexdigest(), checkpoint.stat().st_size)
                self.captured = self.native, metadata, components, expected
            client.release_boundary()
            client.boundary_receipt = self.receipt
            client.boundary_validate(self.receipt)
            return json_bytes(self.server_record)
        client.seal = seal
        def capture(_destination, *, reserve):
            assert client.existing_only
            seal(reserve=reserve)
            return self.captured
        client.capture = capture
        return client

    def test_retry_reuses_exact_queues_and_config_after_new_live_events(self):
        original = self.event()
        self.box.enqueue(encode(original))
        order = []
        def snapshot(source, target, role, reserve):
            order.append(role)
            return snapshot_database(source, target, role, reserve)
        with self.host() as host:
            client = self.client(host)
            with patch("stash_archive.component_stage.snapshot_database", side_effect=snapshot):
                stage = host.prepare(self.cache, client, self.components, reserve=0)
            stage.seal()
            self.assertFalse(host.barrier.acquired)
        self.assertEqual(order, ["download_archive", "producer_outbox"])
        self.box.enqueue(encode(self.event()))
        self.profile.write_bytes(b"later profile")
        with closing(sqlite3.connect(self.downloads)) as db:
            db.execute("INSERT INTO archive VALUES('later')")
            db.commit()
        with self.host() as host:
            client = self.client(host)
            with patch("stash_archive.component_stage.snapshot_database", side_effect=AssertionError("must not recapture")):
                stage = host.prepare(self.cache, client, self.components, reserve=0)
            self.assertTrue(client.existing_only)
            stage.seal()
            with patch("stash_archive.bundle.snapshot_database", side_effect=AssertionError("must pack retained snapshots")):
                export_archive(None, self.root / "bundle", reserve=0, server_checkpoint=client,
                               producer_origin=ORIGIN, component_stage=stage)
        import_archive(self.root / "bundle", self.root / "restored", reserve=0)
        with closing(sqlite3.connect(self.root / "restored/components/producer_outbox/queue.sqlite")) as db:
            self.assertEqual(db.execute("SELECT event_uuid,body FROM events").fetchall(), [(original["event_uuid"], encode(original))])
        with closing(sqlite3.connect(self.root / "restored/components/download_archive/downloads.sqlite")) as db:
            self.assertEqual(db.execute("SELECT entry FROM archive").fetchall(), [("original",)])
        self.assertEqual((self.root / "restored/components/worker_profile/profile.json").read_bytes(), b'{"profile":"original","private":"retained"}\n')
        self.assertEqual(self.creates, 1)

    def test_reopen_after_prepare_without_seal_cannot_start_new_capture(self):
        with self.host() as host:
            host.prepare(self.cache, self.client(host), self.components, reserve=0)
        with self.host() as host:
            stage = host.prepare(self.cache, self.client(host), self.components, reserve=0)
            with self.assertRaisesRegex(InvalidArchive, "HTTP 404"): stage.seal()
        self.assertEqual(self.creates, 0)

    def test_lost_sealed_response_reuses_original_without_new_capture(self):
        with self.host() as host:
            client = self.client(host)
            stage = host.prepare(self.cache, client, self.components, reserve=0)
            actual = client.seal
            def lost(*, reserve):
                actual(reserve=reserve)
                raise OSError("lost response")
            client.seal = lost
            with self.assertRaisesRegex(OSError, "lost response"): stage.seal()
            self.assertTrue(client.existing_only)
        self.assertFalse((stage.path / "checkpoint.json").exists())
        with self.host() as host:
            stage = host.prepare(self.cache, self.client(host), self.components, reserve=0)
            stage.seal()
            self.assertTrue((stage.path / "checkpoint.json").is_file())
        self.assertEqual(self.creates, 1)

    def test_changed_inventory_bytes_or_server_request_are_rejected(self):
        with self.host() as host:
            stage = host.prepare(self.cache, self.client(host), self.components, reserve=0)
            stage.seal()
        for changed in (self.components[:-1], [*self.components, {"role": "config", "name": "extra", "path": self.profile}]):
            with self.host() as host:
                with self.assertRaisesRegex(InvalidArchive, "inventory or server request"):
                    host.prepare(self.cache, self.client(host), changed, reserve=0)
        with self.host() as host:
            client = self.client(host)
            client.boundary_timeout = 60
            with self.assertRaisesRegex(InvalidArchive, "inventory or server request"):
                host.prepare(self.cache, client, self.components, reserve=0)
        stage.component_path(0).write_bytes(b"corrupted")
        with self.host() as host:
            with self.assertRaisesRegex(InvalidArchive, "component changed"):
                host.prepare(self.cache, self.client(host), self.components, reserve=0)

    def test_incomplete_stage_and_reserved_components_never_replace_old_files(self):
        self.profile.unlink()
        with self.host() as host:
            with self.assertRaises(FileNotFoundError): host.prepare(self.cache, self.client(host), self.components, reserve=0)
        self.profile.write_bytes(b"retry after failure")
        with self.host() as host:
            with self.assertRaisesRegex(InvalidArchive, "Incomplete external"):
                host.prepare(self.cache, self.client(host), self.components, reserve=0)
        self.assertEqual(self.creates, 0)
        self.request_id = str(uuid.uuid4())
        with self.host() as host:
            with self.assertRaisesRegex(InvalidArchive, "duplicate staged"):
                host.prepare(self.cache, self.client(host), [{"role": "config", "name": "config.yml", "path": self.profile}], reserve=0)
        self.assertFalse((self.cache / self.request_id).exists())

    def test_released_barrier_cannot_start_fresh_server_capture(self):
        with self.host() as host:
            stage = host.prepare(self.cache, self.client(host), self.components, reserve=0)
            host.barrier.release()
            with self.assertRaisesRegex(InvalidArchive, "original staging barrier"): stage.seal()
        self.assertEqual(self.creates, 0)

    def test_mutation_during_pack_cannot_publish_complete_archive(self):
        with self.host() as host:
            client = self.client(host)
            stage = host.prepare(self.cache, client, self.components, reserve=0)
            stage.seal()
            from stash_archive.bundle import store_file
            def mutate(destination, source, **kwargs):
                if source == stage.component_path(2):
                    source.write_bytes(b"changed after inventory validation")
                return store_file(destination, source, **kwargs)
            with patch("stash_archive.bundle.store_file", side_effect=mutate):
                with self.assertRaisesRegex(InvalidArchive, "captured digest"):
                    export_archive(None, self.root / "bad-bundle", reserve=0, server_checkpoint=client,
                                   producer_origin=ORIGIN, component_stage=stage)
        self.assertFalse((self.root / "bad-bundle").exists())
