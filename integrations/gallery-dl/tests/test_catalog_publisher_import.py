import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.catalog_publisher_import import CatalogPublisherClient, PUBLISHER_POLICY
from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
import test_catalog_upload as catalog_upload_tests
from test_catalog_snapshot import CAPTURED


class MemoryPublisherClient(CatalogPublisherClient):
    def __init__(self, upload):
        super().__init__("http://127.0.0.1:9999")
        self.upload = upload
        self.upload.next = len(upload.manifest["chunks"])
        self.progress = None
        self.posts = 0
        self.lose = True

    def request(self, method, suffix, body, manifest_sha256, content_type):
        assert manifest_sha256 == self.upload.sha
        if not suffix.endswith("/publisher-import"):
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
                             "policy": PUBLISHER_POLICY, "state": "running", "source_records": 3,
                             "last_ordinal": 1, "processed_records": 1, "linked_records": 1, "preserved_records": 0,
                             "unavailable_records": 0, "review_records": 0, "created_accounts": 1,
                             "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        else:
            self.progress.update(state="mapped", last_ordinal=self.upload.manifest["records"], processed_records=3,
                                 linked_records=2, unavailable_records=1)
        if self.lose:
            self.lose = False
            raise Unavailable("network_unavailable")
        return copy.deepcopy(self.progress)


class CatalogPublisherTests(unittest.TestCase):
    def fixture(self, root):
        upload, prepared = catalog_upload_tests.CatalogUploadTests().fixture(root)
        return MemoryPublisherClient(upload), prepared

    def test_restart_reads_committed_checkpoint_after_lost_response(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            with self.assertRaises(Unavailable):
                client.import_publishers(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual(1, client.posts)
            result = client.import_publishers(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual("mapped", result["state"])
            self.assertFalse(result["imported"])
            self.assertEqual(2, client.posts)
            self.assertEqual(result, client.import_publishers(root / "snapshot", prepared["manifest_sha256"]))
            self.assertEqual(2, client.posts)

    def test_incomplete_upload_and_non_404_failures_do_not_start_mapping(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            client.upload.next = 0
            with self.assertRaises(Unavailable):
                client.import_publishers(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual(0, client.posts)
            client.upload.next = len(client.upload.manifest["chunks"])
            original = client.request
            def unavailable(method, suffix, *args):
                if suffix.endswith("/publisher-import"):
                    raise Unavailable("catalog_snapshot_rejected", 403)
                return original(method, suffix, *args)
            with patch.object(client, "request", side_effect=unavailable), self.assertRaises(Unavailable):
                client.import_publishers(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual(0, client.posts)

    def test_changed_policy_counters_bindings_and_premature_completion_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            client.lose = False
            result = client.import_publishers(root / "snapshot", prepared["manifest_sha256"])
            for change in ({"imported": True}, {"linked_records": 1}, {"created_accounts": 3}, {"state": "review"},
                           {"policy": "unknown"}, {"source_records": 7}, {"last_ordinal": 999}, {"processed_records": True},
                           {"manifest_sha256": "0" * 64}, {"created_at": "today"}, {"snapshot_uuid": "other"}):
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**result, **change}, client.upload.manifest, client.upload.sha)
            running = {**result, "state": "running"}
            with self.assertRaises(Unavailable):
                client.validate_progress(running, client.upload.manifest, client.upload.sha, running)
            reviewed = {**result, "state": "review", "linked_records": 1, "review_records": 1}
            self.assertEqual(reviewed, client.validate_progress(reviewed, client.upload.manifest, client.upload.sha))


if __name__ == "__main__":
    unittest.main()
