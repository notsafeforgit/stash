import copy
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_media_import import CatalogMediaClient, MEDIA_POLICY, validate_binding
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, decode
from test_catalog_snapshot import CAPTURED, catalog_fixture
from test_catalog_upload import MemoryUploadClient


class MemoryMediaClient(CatalogMediaClient):
    def __init__(self, upload, binding):
        super().__init__("http://127.0.0.1:9999")
        self.upload, self.binding = upload, binding
        self.upload.next = len(upload.manifest["chunks"])
        self.progress = None
        self.posts = 0
        self.lose = None

    def request(self, method, suffix, body, manifest_sha256, content_type):
        assert manifest_sha256 == self.upload.sha
        if "/media-import" not in suffix:
            assert method == "GET"
            return self.upload.receipt()
        if method == "GET":
            if self.progress is None:
                raise Unavailable("catalog_snapshot_rejected", 404)
            return copy.deepcopy(self.progress)
        assert method == "POST"
        request = decode(body)
        self.posts += 1
        if suffix.endswith("/media-import"):
            assert request == {"expected_manifest_sha256": self.upload.sha, **self.binding}
            assert self.progress is None
            self.progress = {**self.binding, "snapshot_uuid": self.upload.manifest["snapshot_uuid"],
                             "manifest_sha256": self.upload.sha, "policy": MEDIA_POLICY, "state": "running", "phase": "assets",
                             "source_records": 6, "last_ordinal": 0, "processed_records": 0, "mapped_records": 0,
                             "review_records": 0, "unavailable_records": 0, "matched_files": 0, "media_associations": 0,
                             "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
            operation = "begin"
        else:
            assert request == {"expected_manifest_sha256": self.upload.sha, "after": self.progress["processed_records"]}
            if self.progress["phase"] == "assets":
                self.progress.update(phase="files", last_ordinal=0, processed_records=2, mapped_records=2)
            else:
                self.progress.update(state="mapped", phase="complete", last_ordinal=0, processed_records=6,
                                     mapped_records=4, matched_files=1, media_associations=1, unavailable_records=2)
            operation = "advance"
        if self.lose == operation:
            self.lose = None
            raise Unavailable("network_unavailable")
        return copy.deepcopy(self.progress)


class CatalogMediaTests(unittest.TestCase):
    def fixture(self, root):
        source = root / "catalog.sqlite"
        catalog_fixture(source)
        with sqlite3.connect(source) as db:
            for i in range(2):
                asset, path = f"path:{i}", f"file{i}.mp4"
                db.execute("INSERT INTO assets(asset_id,created_at) VALUES(?,?)", (asset, CAPTURED))
                db.execute("INSERT INTO files(relpath,asset_id,state,first_observed,role) VALUES(?,?,'missing',?,'local')", (path, asset, CAPTURED))
                db.execute("INSERT INTO appearances(post_key,attachment_key,asset_id,source_relpath) VALUES('reddit:post:album',?,?,?)", (path, asset, path))
        prepared = prepare(source, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
        manifest = json.loads((root / "snapshot/manifest.json").read_bytes())
        binding = {"root_uuid": str(uuid.uuid4()), "root_revision": 1, "collection_revision": 1, "library_root_path": "/media"}
        upload = MemoryUploadClient(manifest, prepared["manifest_sha256"])
        return MemoryMediaClient(upload, binding), prepared

    def test_lost_binding_and_phase_responses_resume_without_rebinding(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            for operation in ("begin", "advance"):
                client.lose = operation
                with self.assertRaises(Unavailable):
                    client.import_media(root / "snapshot", prepared["manifest_sha256"], client.binding)
            self.assertEqual(2, client.posts)
            result = client.import_media(root / "snapshot", prepared["manifest_sha256"], client.binding)
            self.assertEqual("mapped", result["state"])
            self.assertFalse(result["imported"])
            self.assertEqual(3, client.posts)
            self.assertEqual(result, client.import_media(root / "snapshot", prepared["manifest_sha256"], client.binding))
            self.assertEqual(3, client.posts)

    def test_changed_binding_invalid_progress_and_premature_completion_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            result = client.import_media(root / "snapshot", prepared["manifest_sha256"], client.binding)
            for change in ({"imported": True}, {"mapped_records": 1}, {"matched_files": 9}, {"phase": "assets"},
                           {"policy": "unknown"}, {"source_records": 7}, {"last_ordinal": 1}, {"processed_records": True},
                           {"manifest_sha256": "0" * 64}, {"created_at": "today"}, {"snapshot_uuid": "other"},
                           {"root_uuid": str(uuid.uuid4())}, {"root_revision": True}, {"library_root_path": "/another"}):
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**result, **change}, client.upload.manifest, client.upload.sha, client.binding)
            running = {**result, "state": "running", "phase": "appearances"}
            with self.assertRaises(Unavailable):
                client.validate_progress(running, client.upload.manifest, client.upload.sha, client.binding, running)
            with self.assertRaises(Unavailable):
                client.import_media(root / "snapshot", prepared["manifest_sha256"], {**client.binding, "library_root_path": "/different"})
            self.assertEqual(3, client.posts)

    def test_invalid_bindings_incomplete_upload_and_auth_errors_do_not_write(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            for change in ({"root_uuid": "invalid"}, {"root_revision": True}, {"collection_revision": 0},
                           {"library_root_path": "../relative"}, {"library_root_path": "/a/../b"}, {"library_root_path": "//media"}):
                with self.subTest(change=change), self.assertRaises(InvalidData):
                    validate_binding({**client.binding, **change})
            client.upload.next = 0
            with self.assertRaises(Unavailable):
                client.import_media(root / "snapshot", prepared["manifest_sha256"], client.binding)
            client.upload.next = len(client.upload.manifest["chunks"])
            original = client.request
            def forbidden(method, suffix, *args):
                if suffix.endswith("/media-import"):
                    raise Unavailable("catalog_snapshot_rejected", 403)
                return original(method, suffix, *args)
            with patch.object(client, "request", side_effect=forbidden), self.assertRaises(Unavailable):
                client.import_media(root / "snapshot", prepared["manifest_sha256"], client.binding)
            self.assertEqual(0, client.posts)


if __name__ == "__main__":
    unittest.main()
