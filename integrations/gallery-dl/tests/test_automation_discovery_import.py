import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_discovery_import import AutomationDiscoveryClient, DISCOVERY_POLICY
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, decode
from test_automation_snapshot import automation_fixture, SNAPSHOT, SOURCE, CAPTURED
from test_automation_upload import MemoryAutomationClient


class AutomationDiscoveryTests(unittest.TestCase):
    def fixture(self, root, empty=False):
        automation_fixture(root / "automation.sqlite", empty=empty)
        prepared = prepare(root / "automation.sqlite", root / "snapshot", SNAPSHOT, SOURCE, CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        upload = MemoryAutomationClient(manifest, prepared["manifest_sha256"])
        upload.next = len(manifest["chunks"])
        rows = [decode(line) for chunk in manifest["chunks"] for line in (root / "snapshot" / chunk["file"]).read_bytes().splitlines()]
        ordinals = [index for index, row in enumerate(rows, 1) if row["table"] in ("discovery_accounts", "discovery_targets", "discovery_candidates", "maintenance")]
        progress = {"snapshot_uuid": manifest["snapshot_uuid"], "manifest_sha256": upload.sha,
                    "policy": DISCOVERY_POLICY, "state": "mapped", "last_ordinal": ordinals[-1] if ordinals else 0,
                    "source_records": len(ordinals), "processed_records": len(ordinals), "mapped_records": len(ordinals),
                    "review_records": 0, "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        return AutomationDiscoveryClient("http://127.0.0.1:9999"), upload, progress, ordinals

    def test_sparse_checkpoint_resume_empty_import_and_terminal_read_only_replay(self):
        for empty in (False, True):
            with self.subTest(empty=empty), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                client, upload, complete, ordinals = self.fixture(root, empty)
                if not empty:
                    running = {**complete, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
                    with patch.object(client, "request", side_effect=[upload.receipt(), running, complete]) as request:
                        self.assertEqual(complete, client.import_discovery(root / "snapshot", upload.sha))
                        self.assertEqual({"expected_manifest_sha256": upload.sha, "after": ordinals[0]}, decode(request.call_args.args[2]))
                with patch.object(client, "request", side_effect=[upload.receipt(), Unavailable("missing", 404), complete]) as request:
                    self.assertEqual(complete, client.import_discovery(root / "snapshot", upload.sha))
                    self.assertEqual("POST", request.call_args.args[0])
                    self.assertEqual(0, decode(request.call_args.args[2])["after"])
                with patch.object(client, "request", side_effect=[upload.receipt(), complete]) as request:
                    self.assertEqual(complete, client.import_discovery(root / "snapshot", upload.sha))
                    self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))

    def test_changed_binding_false_completion_and_stalled_progress_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            client, upload, complete, ordinals = self.fixture(Path(directory))
            changes = ({"imported": True}, {"mapped_records": 1}, {"policy": "foreign"}, {"state": "review"},
                       {"source_records": 9}, {"last_ordinal": 999}, {"processed_records": True},
                       {"manifest_sha256": "0" * 64}, {"created_at": "today"}, {"snapshot_uuid": "other"},
                       {"last_ordinal": 0}, {"processed_records": 1, "mapped_records": 1})
            for change in changes:
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**complete, **change}, upload.manifest, upload.sha)
            reviewed = {**complete, "state": "review", "mapped_records": complete["mapped_records"] - 1, "review_records": 1}
            self.assertEqual(reviewed, client.validate_progress(reviewed, upload.manifest, upload.sha))
            running = {**complete, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
            for result in (running, {**running, "last_ordinal": ordinals[0] - 1}, {**complete, "created_at": "2026-01-01T00:00:00Z"}):
                with self.subTest(result=result), self.assertRaises(Unavailable):
                    client.validate_progress(result, upload.manifest, upload.sha, running)
            with self.assertRaises(Unavailable):
                client.validate_progress(reviewed, upload.manifest, upload.sha, complete)

    def test_incomplete_upload_permissions_and_changed_local_source_never_start_import(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, _, _ = self.fixture(root)
            staged = upload.receipt()
            incomplete = copy.deepcopy(staged)
            incomplete["state"] = "receiving"
            with patch.object(client, "request", return_value=incomplete) as request, self.assertRaises(Unavailable):
                client.import_discovery(root / "snapshot", upload.sha)
            self.assertEqual(1, request.call_count)
            for code in (401, 403, 409, 503):
                with self.subTest(code=code), patch.object(client, "request", side_effect=[staged, Unavailable("rejected", code)]) as request, self.assertRaises(Unavailable):
                    client.import_discovery(root / "snapshot", upload.sha)
                self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))
            (root / "snapshot" / upload.manifest["chunks"][0]["file"]).write_bytes(b"changed\n")
            with patch.object(client, "request") as request, self.assertRaises(InvalidData):
                client.import_discovery(root / "snapshot", upload.sha)
            request.assert_not_called()


if __name__ == "__main__":
    unittest.main()
