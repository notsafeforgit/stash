import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_upload import AutomationUploadClient
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, decode, digest
from test_automation_snapshot import automation_fixture, SNAPSHOT, SOURCE, CAPTURED


class MemoryAutomationClient(AutomationUploadClient):
    def __init__(self, manifest, sha):
        self.manifest, self.sha = manifest, sha
        self.next = 0
        self.bodies = {}
        self.calls = []
        self.lose = None

    def receipt(self):
        m = self.manifest
        return {**{key: m[key] for key in ("snapshot_uuid", "registry_source_uuid", "source_sha256", "captured_at", "records")},
                "manifest_sha256": self.sha, "chunks": len(m["chunks"]), "bytes": sum(c["bytes"] for c in m["chunks"]),
                "state": "received" if self.next == len(m["chunks"]) else "receiving", "next_chunk": self.next,
                "received_records": sum(c["rows"] for c in m["chunks"][:self.next]),
                "received_bytes": sum(c["bytes"] for c in m["chunks"][:self.next]),
                "registry_import_uuid": "33333333-3333-4333-8333-333333333333", "created_at": CAPTURED, "updated_at": CAPTURED,
                "pending_families": sorted(m["tables"]), "imported": False}

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


class AutomationUploadTests(unittest.TestCase):
    def fixture(self, root, empty=False):
        automation_fixture(root / "automation.sqlite", empty=empty)
        with patch("stash_ingest.catalog_snapshot.MAX_CHUNK_ROWS", 2):
            result = prepare(root / "automation.sqlite", root / "snapshot", SNAPSHOT, SOURCE, CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        return MemoryAutomationClient(manifest, result["manifest_sha256"]), result

    def test_lost_ack_resume_and_empty_snapshot_do_not_import_jobs(self):
        for empty in (False, True):
            with self.subTest(empty=empty), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                client, prepared = self.fixture(root, empty)
                for method in (("POST",) if empty else ("POST", "PUT")):
                    client.lose = method
                    with self.assertRaises(Unavailable):
                        client.upload(root / "snapshot", prepared["manifest_sha256"])
                receipt = client.upload(root / "snapshot", prepared["manifest_sha256"])
                self.assertEqual("received", receipt["state"])
                self.assertFalse(receipt["imported"])
                self.assertEqual(10, len(receipt["pending_families"]))
                self.assertEqual(prepared["chunks"], len(client.bodies))
                for index, chunk in enumerate(client.manifest["chunks"]):
                    self.assertEqual((root / "snapshot" / chunk["file"]).read_bytes(), client.bodies[index])
                prior = len(client.calls)
                self.assertEqual(receipt, client.upload(root / "snapshot", prepared["manifest_sha256"]))
                self.assertEqual(prior + 1, len(client.calls))

    def test_wrong_receipts_and_rebound_registry_never_acknowledge(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            client, prepared = self.fixture(root)
            original = client.receipt()
            for key, value in (("source_sha256", "0" * 64), ("registry_source_uuid", SNAPSHOT), ("next_chunk", True),
                               ("received_records", 2), ("pending_families", []), ("imported", True), ("registry_import_uuid", "invalid")):
                bad = copy.deepcopy(original)
                bad[key] = value
                with self.subTest(key=key), self.assertRaises(Unavailable):
                    client.validate_receipt(bad, client.manifest, client.sha)
            request = client.request

            def rebound(*args):
                result = request(*args)
                if args[0] == "PUT":
                    result["registry_import_uuid"] = "44444444-4444-4444-8444-444444444444"
                return result

            with patch.object(client, "request", rebound), self.assertRaisesRegex(Unavailable, "binding_changed"):
                client.upload(root / "snapshot", prepared["manifest_sha256"])

    def test_changed_local_chunk_is_not_sent(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            client, prepared = self.fixture(root)
            request = client.request

            def change_after_begin(*args):
                result = request(*args)
                if args[0] == "POST":
                    (root / "snapshot/records-000000.jsonl").write_bytes(b"changed\n")
                return result

            with patch.object(client, "request", change_after_begin), self.assertRaises(InvalidData):
                client.upload(root / "snapshot", prepared["manifest_sha256"])
            self.assertEqual([("POST", "")], client.calls)


if __name__ == "__main__":
    unittest.main()
