from contextlib import closing
import copy
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_translation_import import CatalogTranslationClient, TRANSLATION_POLICY
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
from test_catalog_snapshot import CAPTURED, catalog_fixture
from test_catalog_upload import MemoryUploadClient


class CatalogTranslationTests(unittest.TestCase):
    def fixture(self, root):
        source = root / "catalog.sqlite"
        catalog_fixture(source)
        with closing(sqlite3.connect(source)) as db, db:
            for index in range(2):
                db.execute("INSERT INTO translations VALUES(?,'reddit:post:album',NULL,NULL,'Retained result',NULL,NULL,NULL,'legacy',?)",
                           (str(index), CAPTURED))
        prepared = prepare(source, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        upload = MemoryUploadClient(manifest, prepared["manifest_sha256"])
        upload.next = len(manifest["chunks"])
        staged = upload.receipt()
        rows = [decode(line) for chunk in manifest["chunks"] for line in (root / "snapshot" / chunk["file"]).read_bytes().splitlines()]
        ordinals = [index for index, row in enumerate(rows, 1) if row["table"] == "translations"]
        progress = {"snapshot_uuid": manifest["snapshot_uuid"], "manifest_sha256": upload.sha,
                    "collection_uuid": staged["collection_uuid"], "collection_revision": 1,
                    "policy": TRANSLATION_POLICY, "state": "mapped", "last_ordinal": ordinals[-1],
                    "source_records": 2, "processed_records": 2, "mapped_records": 2, "review_records": 0,
                    "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        return CatalogTranslationClient("http://127.0.0.1:9999"), upload, staged, progress, ordinals

    def test_resume_uses_source_ordinal_and_terminal_replay_does_not_write(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, staged, complete, ordinals = self.fixture(root)
            running = {**complete, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
            self.assertGreater(ordinals[0], 1)
            with patch.object(client, "request", side_effect=[staged, running, complete]) as request:
                self.assertEqual(complete, client.import_translations(root / "snapshot", upload.sha))
                method, suffix, body, _, _ = request.call_args.args
                self.assertEqual("POST", method)
                self.assertTrue(suffix.endswith("/translation-import"))
                self.assertEqual({"expected_manifest_sha256": upload.sha, "after": ordinals[0]}, decode(body))
            with patch.object(client, "request", side_effect=[staged, complete]) as request:
                self.assertEqual(complete, client.import_translations(root / "snapshot", upload.sha))
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))
            with patch.object(client, "request", side_effect=[staged, Unavailable("missing", 404), complete]) as request:
                self.assertEqual(complete, client.import_translations(root / "snapshot", upload.sha))
                self.assertEqual(0, decode(request.call_args.args[2])["after"])

    def test_changed_binding_false_completion_and_stalled_progress_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            client, upload, staged, complete, ordinals = self.fixture(Path(directory))
            changes = ({"imported": True}, {"mapped_records": 1}, {"policy": "foreign"}, {"state": "review"},
                       {"source_records": 9}, {"last_ordinal": 999}, {"processed_records": True},
                       {"manifest_sha256": "0" * 64}, {"created_at": "today"}, {"snapshot_uuid": "other"},
                       {"collection_uuid": str(uuid.uuid4())}, {"collection_revision": 2}, {"collection_revision": True},
                       {"last_ordinal": 0}, {"processed_records": 1, "mapped_records": 1})
            for change in changes:
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**complete, **change}, upload.manifest, upload.sha, staged["collection_uuid"])
            reviewed = {**complete, "state": "review", "mapped_records": 1, "review_records": 1}
            self.assertEqual(reviewed, client.validate_progress(reviewed, upload.manifest, upload.sha, staged["collection_uuid"]))
            running = {**complete, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
            for result in (running, {**running, "last_ordinal": ordinals[0] - 1}, {**complete, "created_at": "2026-01-01T00:00:00Z"}):
                with self.subTest(result=result), self.assertRaises(Unavailable):
                    client.validate_progress(result, upload.manifest, upload.sha, staged["collection_uuid"], running)
            with self.assertRaises(Unavailable):
                client.validate_progress(reviewed, upload.manifest, upload.sha, staged["collection_uuid"], complete)

    def test_incomplete_upload_and_permission_errors_never_start_import(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, staged, _, _ = self.fixture(root)
            incomplete = copy.deepcopy(staged)
            incomplete["state"] = "receiving"
            with patch.object(client, "request", return_value=incomplete) as request, self.assertRaises(Unavailable):
                client.import_translations(root / "snapshot", upload.sha)
            self.assertEqual(1, request.call_count)
            for code in (401, 403, 409, 503):
                with self.subTest(code=code), patch.object(client, "request", side_effect=[staged, Unavailable("rejected", code)]) as request, self.assertRaises(Unavailable):
                    client.import_translations(root / "snapshot", upload.sha)
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))


if __name__ == "__main__":
    unittest.main()
