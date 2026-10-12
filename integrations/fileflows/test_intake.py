import json
from pathlib import Path
import tempfile
import unittest
from uuid import uuid4

import intake


class FakeStash:
    def __init__(self):
        self.saved = None
        self.state = "queued"
        self.review = []
        self.applies = 0
        self.lose_response = False

    def request(self, route, value=None):
        if route.endswith("capabilities"):
            return {"file_ingestion": True, "file_metadata": True}
        if route.endswith("preview"):
            return dict(value, signature="a" * 64)
        if route.endswith("apply"):
            self.saved = value
            self.applies += 1
            if self.lose_response:
                self.lose_response = False
                raise OSError("Response lost after admission")
        if self.saved is None:
            return None
        done = self.state == "succeeded"
        return dict(self.saved, state=self.state, media_ingested=done, registration_committed=done,
                    publication={"media_uuid": str(uuid4()), "review": self.review} if done else None)


class IntakeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.media = self.root / "media"
        self.media.mkdir()
        self.config = {"state_dir": str(self.root / "state"), "folders": [{"path": str(self.media),
                       "relative_path": "Phone uploads", "collection_uuid": str(uuid4())}]}
        self.client = FakeStash()
        self.manifest = self.media / "stash-import-20261012-123456789.json"
        self.body = {"format": "stash-clip-import-v1", "batch_id": "20261012-123456789", "file": "Final [batch].mkv", "size": 2048,
                     "metadata": {"title": 'Title "quote" & 🎥', "details": "Line one\nLine two", "date": "2026-10-11"}}
        self.manifest.write_text(json.dumps(self.body))
        self.video = self.media / self.body["file"]
        self.video.write_bytes(b"v" * 2048)

    def run_once(self):
        return intake.run(self.config, self.client)

    def test_lost_admission_response_resumes_without_duplicate_and_removes_transport(self):
        self.client.lose_response = True
        self.assertIn("error", self.run_once()[0])
        self.assertEqual(self.client.applies, 1)
        self.assertTrue(self.manifest.exists())
        self.assertEqual(self.run_once()[0]["state"], "queued")
        self.client.state = "succeeded"
        self.assertEqual(self.run_once()[0]["state"], "succeeded")
        self.assertEqual(self.client.applies, 1)
        self.assertEqual(self.client.saved["metadata"], self.body["metadata"])
        self.assertTrue(self.video.exists())
        self.assertFalse(self.manifest.exists())
        self.assertEqual(list((self.root / "state").glob("*.json")), [])

    def test_missing_or_partial_video_is_not_admitted(self):
        self.video.unlink()
        self.assertEqual(self.run_once(), [])
        self.video.write_bytes(b"incomplete")
        self.assertIn("error", self.run_once()[0])
        self.assertEqual(self.client.applies, 0)

    def test_empty_description_is_omitted_like_the_stash_response(self):
        self.body["metadata"]["details"] = ""
        self.manifest.write_text(json.dumps(self.body))
        self.assertEqual(self.run_once()[0]["state"], "queued")
        self.assertNotIn("details", self.client.saved["metadata"])

    def test_traversal_symlinks_and_modified_pending_metadata(self):
        self.body["file"] = "../elsewhere.mkv"
        self.manifest.write_text(json.dumps(self.body))
        self.assertIn("error", self.run_once()[0])
        self.body["file"] = self.video.name
        self.manifest.write_text(json.dumps(self.body))
        self.video.unlink()
        self.video.symlink_to(self.root / "outside")
        (self.root / "outside").write_bytes(b"v" * 2048)
        self.assertIn("error", self.run_once()[0])
        self.video.unlink()
        self.video.write_bytes(b"v" * 2048)
        self.assertEqual(self.run_once()[0]["state"], "queued")
        self.body["metadata"]["title"] = "Changed after admission"
        self.manifest.write_text(json.dumps(self.body))
        self.assertIn("error", self.run_once()[0])
        self.assertEqual(self.client.applies, 1)

    def test_failure_or_conflict_preserves_metadata_for_review(self):
        self.run_once()
        self.client.state = "failed"
        self.assertEqual(self.run_once()[0]["state"], "failed")
        self.assertTrue(self.manifest.exists())
        self.client.state = "succeeded"
        self.client.review = ["metadata:details"]
        self.assertEqual(self.run_once()[0]["review"], self.client.review)
        self.assertTrue(self.manifest.exists())


if __name__ == "__main__":
    unittest.main()
