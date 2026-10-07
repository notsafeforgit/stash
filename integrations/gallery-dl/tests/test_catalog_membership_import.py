from contextlib import closing
import copy
import json
import sqlite3
import uuid
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.catalog_membership_import import CatalogMembershipClient, MEMBERSHIP_POLICY
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
from test_catalog_upload import MemoryUploadClient
from test_catalog_snapshot import CAPTURED, catalog_fixture


class MemoryMembershipClient(CatalogMembershipClient):
    def __init__(self, upload):
        super().__init__("http://127.0.0.1:9999")
        self.upload = upload
        self.upload.next = len(upload.manifest["chunks"])
        self.progress = None
        self.posts = 0
        self.lose = True

    def request(self, method, suffix, body, manifest_sha256, content_type):
        assert manifest_sha256 == self.upload.sha
        if not suffix.endswith("/membership-import"):
            assert method == "GET"
            return self.upload.receipt()
        if method == "GET":
            if self.progress is None:
                raise Unavailable("catalog_snapshot_rejected", 404)
            return copy.deepcopy(self.progress)
        assert method == "POST"
        request = decode(body)
        assert request == {"expected_manifest_sha256": self.upload.sha, "after": self.progress["last_ordinal"] if self.progress else 0}
        self.posts += 1
        if self.progress is None:
            self.progress = {"snapshot_uuid": self.upload.manifest["snapshot_uuid"], "manifest_sha256": self.upload.sha,
                             "state": "running", "policy": MEMBERSHIP_POLICY, "source_records": 2, "last_ordinal": 1, "processed_records": 1,
                             "mapped_records": 1, "review_records": 0,
                             "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        else:
            self.progress.update(state="mapped", last_ordinal=12, processed_records=2, mapped_records=2)
        if self.lose:
            self.lose = False
            raise Unavailable("network_unavailable")
        return copy.deepcopy(self.progress)


class CatalogMembershipTests(unittest.TestCase):
    def fixture(self, root):
        source = root / "catalog.sqlite"
        catalog_fixture(source)
        with closing(sqlite3.connect(source)) as db, db:
            for i in range(2):
                db.execute("INSERT INTO memberships VALUES('reddit:post:album',?,'collection',?)", (f"directory:Group {i}", f"Group {i}"))
        prepared = prepare(source, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
        manifest = json.loads((root / "snapshot/manifest.json").read_bytes())
        upload = MemoryUploadClient(manifest, prepared["manifest_sha256"])
        return MemoryMembershipClient(upload), prepared

    def test_restart_reads_committed_checkpoint_after_lost_response(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            with self.assertRaises(Unavailable):
                client.import_memberships(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual(1, client.posts)
            result = client.import_memberships(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual("mapped", result["state"])
            self.assertFalse(result["imported"])
            self.assertEqual(2, client.posts)
            self.assertEqual(result, client.import_memberships(root / "snapshot", prepared["manifest_sha256"]))
            self.assertEqual(2, client.posts)

    def test_incomplete_upload_and_non_404_failures_do_not_start_mapping(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            client.upload.next = 0
            with self.assertRaises(Unavailable):
                client.import_memberships(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual(0, client.posts)
            client.upload.next = len(client.upload.manifest["chunks"])
            original = client.request
            def unavailable(method, suffix, *args):
                if suffix.endswith("/membership-import"):
                    raise Unavailable("catalog_snapshot_rejected", 403)
                return original(method, suffix, *args)
            with patch.object(client, "request", side_effect=unavailable), self.assertRaises(Unavailable):
                client.import_memberships(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual(0, client.posts)

    def test_changed_counters_bindings_and_premature_completion_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            client.lose = False
            result = client.import_memberships(root / "snapshot", prepared["manifest_sha256"])
            for change in ({"imported": True}, {"mapped_records": 1}, {"policy": "foreign"}, {"state": "review"},
                           {"source_records": 7}, {"last_ordinal": 999}, {"processed_records": True}, {"manifest_sha256": "0" * 64},
                           {"created_at": "today"}, {"snapshot_uuid": "other"}):
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**result, **change}, client.upload.manifest, client.upload.sha)
            running = {**result, "state": "running"}
            with self.assertRaises(Unavailable):
                client.validate_progress(running, client.upload.manifest, client.upload.sha, running)
            reviewed = {**result, "state": "review", "mapped_records": 1, "review_records": 1}
            self.assertEqual(reviewed, client.validate_progress(reviewed, client.upload.manifest, client.upload.sha))


if __name__ == "__main__":
    unittest.main()
