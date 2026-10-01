import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_snapshot import prepare
from stash_ingest.catalog_upload import CatalogUploadClient
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, decode, digest
from test_catalog_snapshot import CAPTURED, catalog_fixture


class MemoryUploadClient(CatalogUploadClient):
    def __init__(self, manifest, manifest_sha256):
        super().__init__("http://127.0.0.1:9999")
        self.manifest, self.sha = manifest, manifest_sha256
        self.next = 0
        self.calls = []
        self.bodies = {}
        self.lose = None

    def receipt(self):
        m = self.manifest
        return {**{key: m[key] for key in ("snapshot_uuid", "registry_source_uuid", "catalog_id", "captured_at", "records")},
                "manifest_sha256": self.sha, "chunks": len(m["chunks"]), "bytes": sum(c["bytes"] for c in m["chunks"]),
                "state": "received" if self.next == len(m["chunks"]) else "receiving", "next_chunk": self.next,
                "received_records": sum(c["rows"] for c in m["chunks"][:self.next]),
                "received_bytes": sum(c["bytes"] for c in m["chunks"][:self.next]),
                "registry_import_uuid": "33333333-3333-4333-8333-333333333333", "collection_uuid": "44444444-4444-4444-8444-444444444444",
                "created_at": CAPTURED, "updated_at": CAPTURED, "pending_families": sorted(m["tables"]), "imported": False}

    def request(self, method, suffix, body, manifest_sha256, content_type):
        assert manifest_sha256 == self.sha
        self.calls.append((method, suffix))
        if method == "POST":
            assert digest(body) == self.sha
        else:
            index = int(suffix.rsplit("/", 1)[1])
            assert index == self.next and digest(body) == self.manifest["chunks"][index]["sha256"]
            assert content_type == "application/x-ndjson"
            self.bodies[index] = body
            self.next += 1
        if self.lose == method:
            self.lose = None
            raise Unavailable("network_unavailable")
        return self.receipt()


class CatalogUploadTests(unittest.TestCase):
    def fixture(self, root):
        source = root / "catalog.sqlite"
        catalog_fixture(source)
        with patch("stash_ingest.catalog_snapshot.MAX_CHUNK_ROWS", 2):
            result = prepare(source, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        return MemoryUploadClient(manifest, result["manifest_sha256"]), result

    def test_lost_begin_and_chunk_responses_resume_original_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            for method in ("POST", "PUT"):
                client.lose = method
                with self.assertRaises(Unavailable):
                    client.upload(root / "snapshot", prepared["manifest_sha256"])
            receipt = client.upload(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual("received", receipt["state"])
            self.assertFalse(receipt["imported"])
            self.assertEqual(prepared["chunks"], len(client.bodies))
            for index, chunk in enumerate(client.manifest["chunks"]):
                self.assertEqual((root / "snapshot" / chunk["file"]).read_bytes(), client.bodies[index])
            prior = len(client.calls)
            self.assertEqual(receipt, client.upload(root / "snapshot", prepared["manifest_sha256"]))
            self.assertEqual(prior + 1, len(client.calls))

    def test_wrong_or_premature_receipts_never_acknowledge_import(self):
        with tempfile.TemporaryDirectory() as directory:
            client, _ = self.fixture(Path(directory))
            for change in ({"manifest_sha256": "0" * 64}, {"snapshot_uuid": str(uuid.uuid4())},
                           {"next_chunk": True}, {"received_records": False}, {"received_bytes": 1},
                           {"state": "received"}, {"imported": True}, {"pending_families": []},
                           {"registry_import_uuid": "not-a-uuid"}, {"created_at": "2026-01-01"}):
                receipt = {**client.receipt(), **change}
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_receipt(receipt, client.manifest, client.sha)
            with self.assertRaises(Unavailable):
                client.validate_receipt(client.receipt(), client.manifest, client.sha, minimum=1)

    def test_changed_files_after_verification_are_not_uploaded(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            original = client.request
            def change_after_begin(*args):
                result = original(*args)
                (root / "snapshot" / client.manifest["chunks"][0]["file"]).write_bytes(b"changed\n")
                return result
            with patch.object(client, "request", side_effect=change_after_begin), self.assertRaises(InvalidData):
                client.upload(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual({}, client.bodies)
            with self.assertRaises(InvalidData):
                client.upload(root / "snapshot", "0" * 64)

    def test_receipt_binding_cannot_change_between_chunks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, prepared = self.fixture(root)
            original = client.request
            def change_collection(method, *args):
                result = copy.deepcopy(original(method, *args))
                if method == "PUT":
                    result["collection_uuid"] = str(uuid.uuid4())
                return result
            with patch.object(client, "request", side_effect=change_collection), self.assertRaises(Unavailable):
                client.upload(root / "snapshot", prepared["manifest_sha256"])


if __name__ == "__main__":
    unittest.main()
