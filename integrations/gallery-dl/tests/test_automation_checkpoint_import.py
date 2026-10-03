import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.automation_checkpoint_import import AutomationCheckpointClient, CHECKPOINT_POLICY, checkpoint_count
from stash_ingest.automation_snapshot import prepare
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, decode
from test_automation_snapshot import automation_fixture, SNAPSHOT, SOURCE, CAPTURED
from test_automation_upload import MemoryAutomationClient


class AutomationCheckpointTests(unittest.TestCase):
    def fixture(self, root, empty=False):
        automation_fixture(root / "automation.sqlite", empty=empty)
        prepared = prepare(root / "automation.sqlite", root / "snapshot", SNAPSHOT, SOURCE, CAPTURED)
        manifest = decode((root / "snapshot/manifest.json").read_bytes())
        upload = MemoryAutomationClient(manifest, prepared["manifest_sha256"])
        upload.next = len(manifest["chunks"])
        rows = [decode(line) for chunk in manifest["chunks"] for line in (root / "snapshot" / chunk["file"]).read_bytes().splitlines()]
        ordinals = [i for i, row in enumerate(rows, 1) if row["table"] == "enrichment_jobs" and row["values"]["staged_json"] is not None]
        total = len(ordinals)
        progress = {"snapshot_uuid": SNAPSHOT, "manifest_sha256": upload.sha, "policy": CHECKPOINT_POLICY,
                    "state": "mapped", "last_ordinal": ordinals[-1] if ordinals else 0, "source_records": total,
                    "processed_records": total, "mapped_records": total, "review_records": 0,
                    "created_at": CAPTURED, "updated_at": CAPTURED, "imported": False}
        return AutomationCheckpointClient("http://127.0.0.1:9999"), upload, progress, ordinals

    def test_sparse_resume_exact_staged_count_and_completed_replay(self):
        for empty in (False, True):
            with self.subTest(empty=empty), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                client, upload, completed, ordinals = self.fixture(root, empty)
                self.assertEqual(len(ordinals), checkpoint_count(root / "snapshot", upload.manifest))
                if not empty:
                    self.assertEqual(2, len(ordinals))
                    running = {**completed, "state": "running", "processed_records": 1, "mapped_records": 1, "last_ordinal": ordinals[0]}
                    with patch.object(client, "request", side_effect=[upload.receipt(), running, completed]) as request:
                        self.assertEqual(completed, client.import_checkpoints(root / "snapshot", upload.sha))
                        self.assertEqual(ordinals[0], decode(request.call_args.args[2])["after"])
                with patch.object(client, "request", side_effect=[upload.receipt(), Unavailable("missing", 404), completed]) as request:
                    self.assertEqual(completed, client.import_checkpoints(root / "snapshot", upload.sha))
                    self.assertEqual("POST", request.call_args.args[0])
                    self.assertEqual(0, decode(request.call_args.args[2])["after"])
                with patch.object(client, "request", side_effect=[upload.receipt(), completed]) as request:
                    self.assertEqual(completed, client.import_checkpoints(root / "snapshot", upload.sha))
                    self.assertTrue(all(call.args[0] == "GET" for call in request.call_args_list))

    def test_false_completion_foreign_binding_and_stalled_cursors_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            client, upload, completed, ordinals = self.fixture(Path(directory))
            for change in ({"imported": True}, {"source_records": 0, "processed_records": 0, "mapped_records": 0, "last_ordinal": 0},
                           {"policy": "automation-enrichment-v1"}, {"manifest_sha256": "0" * 64}, {"snapshot_uuid": "other"},
                           {"state": "review"}, {"processed_records": True}, {"last_ordinal": 999}):
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    client.validate_progress({**completed, **change}, upload.manifest, upload.sha, len(ordinals))
            running = {**completed, "state": "running", "last_ordinal": ordinals[0], "processed_records": 1, "mapped_records": 1}
            with self.assertRaises(Unavailable):
                client.validate_progress(running, upload.manifest, upload.sha, len(ordinals), running)
            reviewed = {**completed, "state": "review", "mapped_records": 1, "review_records": 1}
            self.assertEqual(reviewed, client.validate_progress(reviewed, upload.manifest, upload.sha, len(ordinals), running))

    def test_corrupt_local_chunks_and_incomplete_upload_cannot_begin(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            client, upload, _, _ = self.fixture(root)
            incomplete = copy.deepcopy(upload.receipt())
            incomplete["state"] = "receiving"
            with patch.object(client, "request", return_value=incomplete) as request, self.assertRaises(Unavailable):
                client.import_checkpoints(root / "snapshot", upload.sha)
            self.assertEqual(1, request.call_count)
            chunk = root / "snapshot" / upload.manifest["chunks"][0]["file"]
            chunk.write_bytes(chunk.read_bytes() + b" ")
            with self.assertRaises(InvalidData):
                checkpoint_count(root / "snapshot", upload.manifest)
            with patch.object(client, "request") as request, self.assertRaises(InvalidData):
                client.import_checkpoints(root / "snapshot", upload.sha)
            request.assert_not_called()


if __name__ == "__main__":
    unittest.main()
