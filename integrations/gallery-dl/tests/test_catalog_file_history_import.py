from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_file_history_import import CatalogFileHistoryClient, FILE_HISTORY_POLICY
from stash_ingest.catalog_media_import import MEDIA_POLICY
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
from test_catalog_snapshot import CAPTURED, catalog_fixture
from test_catalog_upload import MemoryUploadClient


class CatalogFileHistoryTests(unittest.TestCase):
    def fixture(self, root, count=2):
        source = root / "catalog.sqlite"
        catalog_fixture(source)
        with closing(sqlite3.connect(source)) as db, db:
            db.execute("INSERT INTO assets(asset_id,created_at) VALUES('path:fixture',?)", (CAPTURED,))
            db.execute("INSERT INTO files(relpath,asset_id,state,first_observed,role) VALUES('video.mp4','path:fixture','missing',?,'local')", (CAPTURED,))
            db.execute("CREATE TABLE metadata_edits(edit_id TEXT PRIMARY KEY,relpath TEXT NOT NULL,fields_json TEXT NOT NULL,created_at TEXT NOT NULL)")
            for index in range(count):
                db.execute("INSERT INTO metadata_edits VALUES(?,'video.mp4',?,?)", (str(index), '{"title":null}', CAPTURED))
        prepared = prepare(source, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        upload = MemoryUploadClient(manifest, prepared["manifest_sha256"])
        upload.next = len(manifest["chunks"])
        staged = upload.receipt()
        rows = [decode(line) for chunk in manifest["chunks"] for line in (root / "snapshot" / chunk["file"]).read_bytes().splitlines()]
        ordinals = [index for index, row in enumerate(rows, 1) if row["table"] == "metadata_edits"]
        scope = {"snapshot_uuid": manifest["snapshot_uuid"], "manifest_sha256": upload.sha,
                 "collection_uuid": staged["collection_uuid"], "collection_revision": 1,
                 "root_uuid": str(uuid.uuid4()), "root_revision": 2, "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        progress = {**scope, "policy": FILE_HISTORY_POLICY, "state": "mapped", "last_ordinal": ordinals[-1] if count else 0,
                    "source_records": count, "processed_records": count, "mapped_records": count, "review_records": 0}
        media = {**scope, "policy": MEDIA_POLICY, "state": "mapped", "phase": "complete", "library_root_path": "/media",
                 "last_ordinal": 0, "source_records": 2, "processed_records": 2, "mapped_records": 1, "review_records": 0,
                 "unavailable_records": 1, "matched_files": 0, "media_associations": 0}
        del media["collection_uuid"]
        return CatalogFileHistoryClient("http://127.0.0.1:9999"), upload, staged, media, progress, ordinals

    def test_resume_uses_frozen_media_binding_and_terminal_replay_does_not_write(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, staged, media, complete, ordinals = self.fixture(root)
            running = {**complete, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
            with patch.object(client, "request", side_effect=[staged, media, running, complete]) as request:
                self.assertEqual(complete, client.import_history(root / "snapshot", upload.sha))
                method, suffix, body, _, _ = request.call_args.args
                self.assertEqual("POST", method)
                self.assertTrue(suffix.endswith("/file-history-import"))
                self.assertEqual({"expected_manifest_sha256": upload.sha, "after": ordinals[0]}, decode(body))
            with patch.object(client, "request", side_effect=[staged, media, complete]) as request:
                self.assertEqual(complete, client.import_history(root / "snapshot", upload.sha))
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))
            with patch.object(client, "request", side_effect=[staged, media, Unavailable("missing", 404), complete]) as request:
                self.assertEqual(complete, client.import_history(root / "snapshot", upload.sha))
                self.assertEqual(0, decode(request.call_args.args[2])["after"])

    def test_rebinding_false_completion_and_stalled_progress_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            client, upload, staged, media, complete, ordinals = self.fixture(Path(directory))
            changes = ({"imported": True}, {"mapped_records": 1}, {"policy": "foreign"}, {"state": "review"},
                       {"source_records": 9}, {"last_ordinal": 999}, {"processed_records": True},
                       {"manifest_sha256": "0" * 64}, {"created_at": "today"}, {"snapshot_uuid": "other"},
                       {"collection_uuid": str(uuid.uuid4())}, {"collection_revision": 2}, {"collection_revision": True},
                       {"root_uuid": str(uuid.uuid4())}, {"root_revision": 1}, {"root_revision": True},
                       {"last_ordinal": 0}, {"processed_records": 1, "mapped_records": 1})
            for change in changes:
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**complete, **change}, upload.manifest, upload.sha, staged["collection_uuid"], media)
            reviewed = {**complete, "state": "review", "mapped_records": 1, "review_records": 1}
            self.assertEqual(reviewed, client.validate_progress(reviewed, upload.manifest, upload.sha, staged["collection_uuid"], media))
            running = {**complete, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
            for result in (running, {**running, "last_ordinal": ordinals[0] - 1}, {**complete, "created_at": "2026-01-01T00:00:00Z"}):
                with self.subTest(result=result), self.assertRaises(Unavailable):
                    client.validate_progress(result, upload.manifest, upload.sha, staged["collection_uuid"], media, running)

    def test_empty_optional_families_and_media_prerequisite(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, staged, media, complete, _ = self.fixture(root, count=0)
            with patch.object(client, "request", side_effect=[staged, media, Unavailable("missing", 404), complete]):
                self.assertEqual(complete, client.import_history(root / "snapshot", upload.sha))
            for response in (Unavailable("missing", 404), {**media, "state": "running", "phase": "assets"},
                             {**media, "snapshot_uuid": str(uuid.uuid4())}):
                with self.subTest(response=response), patch.object(client, "request", side_effect=[staged, response]) as request, self.assertRaises(Unavailable):
                    client.import_history(root / "snapshot", upload.sha)
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))
            for code in (401, 403, 409, 503):
                with self.subTest(code=code), patch.object(client, "request", side_effect=[staged, media, Unavailable("rejected", code)]) as request, self.assertRaises(Unavailable):
                    client.import_history(root / "snapshot", upload.sha)
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))


if __name__ == "__main__":
    unittest.main()
