import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_document_import import CatalogDocumentClient, DOCUMENT_POLICY
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
from test_catalog_snapshot import CAPTURED, catalog_fixture
from test_catalog_upload import MemoryUploadClient


class CatalogDocumentTests(unittest.TestCase):
    def fixture(self, root):
        source = root / "catalog.sqlite"
        catalog_fixture(source)
        prepared = prepare(source, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        upload = MemoryUploadClient(manifest, prepared["manifest_sha256"])
        upload.next = len(manifest["chunks"])
        staged = upload.receipt()
        progress = {"snapshot_uuid": manifest["snapshot_uuid"], "manifest_sha256": upload.sha,
                    "collection_uuid": staged["collection_uuid"], "collection_revision": 1,
                    "policy": DOCUMENT_POLICY, "state": "mapped", "phase": "complete", "last_ordinal": 0,
                    "source_records": 3, "processed_records": 3, "mapped_records": 3, "review_records": 0,
                    "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        return CatalogDocumentClient("http://127.0.0.1:9999"), upload, staged, progress

    def test_resume_uses_processed_count_and_terminal_replay_does_not_write(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, staged, complete = self.fixture(root)
            running = {**complete, "state": "running", "phase": "sidecar_sources", "last_ordinal": 11,
                       "processed_records": 2, "mapped_records": 2}
            with patch.object(client, "request", side_effect=[staged, running, complete]) as request:
                self.assertEqual(complete, client.import_documents(root / "snapshot", upload.sha))
                method, suffix, body, _, _ = request.call_args.args
                self.assertEqual("POST", method)
                self.assertTrue(suffix.endswith("/document-import"))
                self.assertEqual({"expected_manifest_sha256": upload.sha, "after": 2}, decode(body))
            with patch.object(client, "request", side_effect=[staged, complete]) as request:
                self.assertEqual(complete, client.import_documents(root / "snapshot", upload.sha))
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))

    def test_binding_counters_phase_and_false_completion_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            client, upload, staged, complete = self.fixture(Path(directory))
            changes = ({"imported": True}, {"mapped_records": 1}, {"policy": "foreign"}, {"state": "review"},
                       {"source_records": 9}, {"last_ordinal": 999}, {"processed_records": True},
                       {"manifest_sha256": "0" * 64}, {"created_at": "today"}, {"snapshot_uuid": "other"},
                       {"collection_uuid": str(uuid.uuid4())}, {"collection_revision": 2}, {"collection_revision": True},
                       {"phase": "sidecar_heads"}, {"state": "running"}, {"last_ordinal": 1},
                       {"phase": "sidecar_documents", "state": "running", "last_ordinal": 1})
            for change in changes:
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**complete, **change}, upload.manifest, upload.sha, staged["collection_uuid"])
            reviewed = {**complete, "state": "review", "mapped_records": 1, "review_records": 2}
            self.assertEqual(reviewed, client.validate_progress(reviewed, upload.manifest, upload.sha, staged["collection_uuid"]))
            running = {**complete, "state": "running", "phase": "sidecar_sources", "last_ordinal": 11,
                       "processed_records": 2, "mapped_records": 2}
            for result in (running, {**running, "last_ordinal": 10}, {**complete, "created_at": "2026-01-01T00:00:00Z"}):
                with self.subTest(result=result), self.assertRaises(Unavailable):
                    client.validate_progress(result, upload.manifest, upload.sha, staged["collection_uuid"], running)
            with self.assertRaises(Unavailable):
                client.validate_progress(reviewed, upload.manifest, upload.sha, staged["collection_uuid"], complete)

    def test_incomplete_upload_and_permission_errors_never_start_import(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, staged, _ = self.fixture(root)
            incomplete = copy.deepcopy(staged)
            incomplete["state"] = "receiving"
            with patch.object(client, "request", return_value=incomplete) as request, self.assertRaises(Unavailable):
                client.import_documents(root / "snapshot", upload.sha)
            self.assertEqual(1, request.call_count)
            for code in (401, 403, 409, 503):
                with self.subTest(code=code), patch.object(client, "request", side_effect=[staged, Unavailable("rejected", code)]) as request, self.assertRaises(Unavailable):
                    client.import_documents(root / "snapshot", upload.sha)
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))


if __name__ == "__main__":
    unittest.main()
