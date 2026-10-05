from contextlib import closing
import base64
import copy
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import sqlite3
import threading
import unittest
from unittest.mock import patch
import uuid

from stash_archive.bundle import export_archive, import_archive, iter_artifacts, snapshot_database
from stash_archive.checkpoint_release import (RELEASE_FORMAT, archive_binding, checkpoint_digest,
                                              release_published_checkpoint)
from stash_archive.server_checkpoint import ServerCheckpoint, FORMAT, COVERAGE, ROLES, request_bytes
from stash_archive.storage import InvalidArchive, store_file
import test_bundle


class ServerCheckpointTests(unittest.TestCase):
    def setUp(self):
        test_bundle.ArchiveTests.setUp(self)
        self.db.execute("CREATE TABLE file_deletions(id TEXT PRIMARY KEY)")
        self.db.commit()
        self.mode, self.requests, self.order = "ok", [], []
        self.request_id = str(uuid.uuid4())
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def reply(self, data, content_type, digest=None):
                self.send_response(200)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(data)))
                if digest:
                    self.send_header("X-Stash-SHA256", digest)
                self.end_headers()
                self.wfile.write(data)

            def do_POST(self):
                owner.requests.append((self.command, self.path, self.headers.get("ApiKey")))
                owner.order.append("server")
                request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                if self.path.endswith("/release"):
                    reply = {"format": RELEASE_FORMAT, "version": 1, "uuid": owner.request_id,
                             "request_sha256": owner.capture_request_hash, "archive": request,
                             "released_at": "2026-10-05T00:00:00Z"}
                    if owner.mode == "wrong-release":
                        reply["archive"]["archive_manifest_sha256"] = "0" * 64
                    self.reply(json.dumps(reply).encode(), "application/json")
                    return
                if owner.mode == "redirect":
                    self.send_response(302)
                    self.send_header("Location", "/credential-leak")
                    self.end_headers()
                    return
                native = owner.root / (str(uuid.uuid4()) + ".sqlite")
                with closing(sqlite3.connect(owner.database)) as source, closing(sqlite3.connect(native)) as target:
                    source.backup(target)
                owner.objects = {"library.sqlite": native.read_bytes(), "config.yml": b"private_setting: retained\n",
                                 "runtime-overrides.yml": b"port: 8010\n", "deletions.zip": b"opaque-test-recovery-component"}
                manifest = {"format": FORMAT, "version": 1, "uuid": request["uuid"], "coverage": COVERAGE,
                            "created_at": "2026-10-05T00:00:00Z", "request_sha256": hashlib.sha256(request_bytes(request)).hexdigest(),
                            "source_database_path": base64.b64encode(bytes(owner.database)).decode(), "source_config_path": None,
                            "source_working_directory": base64.b64encode(bytes(owner.root)).decode(),
                            "committed_deletion_ids": None,
                            "components": [{"role": ROLES[name], "name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}
                                           for name, data in owner.objects.items()]}
                owner.capture_request_hash = manifest["request_sha256"]
                if owner.mode == "wrong-request":
                    manifest["request_sha256"] = "0" * 64
                elif owner.mode == "wrong-markers":
                    manifest["committed_deletion_ids"] = [str(uuid.uuid4())]
                elif owner.mode == "unknown-component":
                    manifest["components"][0]["name"] = "../outside"
                self.reply(json.dumps(manifest).encode(), "application/json")

            def do_GET(self):
                owner.requests.append((self.command, self.path, self.headers.get("ApiKey")))
                name = self.path.rsplit("/", 1)[-1]
                data = owner.objects[name]
                digest = hashlib.sha256(data).hexdigest()
                if owner.mode == "corrupt" and name == "config.yml":
                    data = bytes([data[0] ^ 1]) + data[1:]
                self.reply(data, "application/octet-stream", digest)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.close_server)
        self.client = ServerCheckpoint(f"http://127.0.0.1:{self.server.server_port}", "private-test-key", self.request_id)

    def close_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def export(self, **kwargs):
        return export_archive(None, self.output, blob_paths=[self.blobs], reserve=0, server_checkpoint=self.client, **kwargs)

    def test_server_components_round_trip_without_second_library_snapshot(self):
        with patch("stash_archive.bundle.snapshot_database", side_effect=AssertionError("must use the already captured library")):
            self.export()
        restored = self.root / "restored"
        import_archive(self.output, restored, reserve=0)
        self.assertEqual((restored / "components/config/config.yml").read_bytes(), b"private_setting: retained\n")
        manifest = json.loads((restored / "components/operating_state/server-checkpoint.json").read_bytes())
        self.assertEqual(manifest["uuid"], self.request_id)
        self.assertTrue(all(key == "private-test-key" for _, _, key in self.requests))
        self.assertEqual(sum(method == "POST" for method, _, _ in self.requests), 1)

    def test_downloads_and_outboxes_precede_server_and_receipts_precede_pack(self):
        downloads = self.root / "downloads.sqlite"
        with closing(sqlite3.connect(downloads)) as db:
            db.execute("CREATE TABLE archive(entry TEXT PRIMARY KEY)")

        def snapshot(source, destination, role, reserve):
            self.order.append(role)
            return snapshot_database(source, destination, role, reserve)

        def receipts(*args):
            self.order.append("receipts")

        def pack(*args, **kwargs):
            self.assertEqual(self.order, ["download_archive", "producer_outbox", "server", "receipts"])
            return store_file(*args, **kwargs)

        with patch("stash_archive.bundle.snapshot_database", side_effect=snapshot), \
                patch("stash_archive.receipts.verify_snapshot_receipts", side_effect=receipts), \
                patch("stash_archive.bundle.store_file", side_effect=pack):
            self.export(producer_origin="https://stash.example", components=[
                {"role": "producer_outbox", "name": "worker.sqlite", "path": self.outbox},
                {"role": "download_archive", "name": "downloads.sqlite", "path": downloads}])

    def test_transport_mismatch_never_seals_archive(self):
        for mode in ("wrong-request", "wrong-markers", "unknown-component", "corrupt", "redirect"):
            with self.subTest(mode=mode):
                self.mode = mode
                self.requests.clear()
                with self.assertRaises(InvalidArchive):
                    self.export()
                self.assertFalse(self.output.exists())
                self.assertFalse(any(path == "/credential-leak" for _, path, _ in self.requests))

    def test_frozen_component_changed_before_pack_is_rejected(self):
        def changed_capture(*args, **kwargs):
            native, metadata, components, expected = original(*args, **kwargs)
            for component in components:
                if component["name"] == "config.yml":
                    Path(component["path"]).write_bytes(b"changed after download")
            return native, metadata, components, expected

        original = self.client.capture
        with patch.object(self.client, "capture", side_effect=changed_capture):
            with self.assertRaisesRegex(InvalidArchive, "captured digest"):
                self.export()
        self.assertFalse(self.output.exists())

    def test_component_collision_and_missing_receipt_origin_fail_before_network(self):
        for component in ({"role": "config", "name": "config.yml", "path": self.config},
                          {"role": "producer_outbox", "name": "worker.sqlite", "path": self.outbox}):
            with self.assertRaises(InvalidArchive):
                self.export(components=[component])
            self.assertEqual(self.requests, [])
            self.assertFalse(self.output.exists())

    def test_release_binds_verified_enclosing_archive_and_checks_receipt(self):
        self.export()
        self.assertFalse(any(path.endswith("/release") for _, path, _ in self.requests))
        # This isolated publisher has durably saved and fully restored its local
        # archive. Remote publishers must also finish their own remote readback.
        import_archive(self.output, self.root / "release-restore", reserve=0)
        checkpoint, binding = archive_binding(self.output, self.client)
        shuffled = dict(reversed(list(checkpoint.items())))
        shuffled["components"] = [dict(reversed(list(c.items()))) for c in checkpoint["components"]]
        self.assertEqual(checkpoint_digest(checkpoint), checkpoint_digest(shuffled))
        receipt = release_published_checkpoint(self.output, self.client)
        self.assertEqual(receipt["archive"], binding)
        self.assertEqual(release_published_checkpoint(self.output, self.client), receipt)
        self.mode = "wrong-release"
        with self.assertRaisesRegex(InvalidArchive, "does not match"):
            release_published_checkpoint(self.output, self.client)

    def test_release_rejects_other_checkpoint_or_incomplete_archive_before_network(self):
        self.export()
        manifest = json.loads((self.output / "manifest.json").read_bytes())
        original = list(iter_artifacts(self.output, manifest))
        self.requests.clear()
        other = ServerCheckpoint(self.client.server, self.client.api_key, str(uuid.uuid4()))
        with self.assertRaises(InvalidArchive):
            release_published_checkpoint(self.output, other)
        for mode in ("missing-config", "different-library", "missing-checkpoint", "bad-checkpoint-digest"):
            entries = copy.deepcopy(original)
            if mode == "missing-config":
                entries = [e for e in entries if (e["role"], e["name"]) != ("config", "config.yml")]
            elif mode == "different-library":
                next(e for e in entries if e["role"] == "library")["sha256"] = "0" * 64
            elif mode == "missing-checkpoint":
                entries = [e for e in entries if e["name"] != "server-checkpoint.json"]
            else:
                next(e for e in entries if e["name"] == "server-checkpoint.json")["sha256"] = "0" * 64
            test_bundle.ArchiveTests.rewrite_inventory(self, manifest, entries)
            with self.subTest(mode=mode), self.assertRaises(InvalidArchive):
                release_published_checkpoint(self.output, self.client)
        self.assertEqual(self.requests, [])
