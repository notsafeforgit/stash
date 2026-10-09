import copy
import json
from pathlib import Path
import unittest

from stash_ingest.encoding import InvalidData
from stash_ingest import source
from stash_ingest.retention import retain


class SourceAdapterTests(unittest.TestCase):
    def test_linked_reddit_attachment_contract(self):
        path = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/reddit-external-media-v1.json"
        for case in json.loads(path.read_text())["cases"]:
            with self.subTest(case=case["name"]):
                original = copy.deepcopy(case["source"])
                kept = retain(case["source"])
                if case["attachment"] is None:
                    with self.assertRaises(InvalidData):
                        source.attachment(kept)
                else:
                    self.assertEqual(case["attachment"], source.attachment(kept))
                    self.assertEqual({"namespace": "native:reddit", "value": "post"}, source.post(kept))
                self.assertEqual(original, case["source"])

    def test_shared_server_identity_and_metadata_contract(self):
        path = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/captured-post-adapters-v1.json"
        fixture = json.loads(path.read_text())
        for case in fixture["cases"]:
            with self.subTest(case=case["name"]):
                original = copy.deepcopy(case["source"])
                if case["post"] is None:
                    with self.assertRaises(InvalidData):
                        source.post(case["source"])
                else:
                    self.assertEqual(case["post"], source.post(case["source"]))
                    self.assertEqual(case["post"], source.post(retain(case["source"])))
                    self.assertEqual(case["metadata"], source.metadata(case["source"]))
                self.assertEqual(original, case["source"])

    def test_post_identity_does_not_claim_an_unimplemented_attachment(self):
        for data in ({"category": "tiktok", "id": "123"},
                     {"category": "instagram", "post_id": "123"},
                     {"category": "coomer", "service": "onlyfans", "user": "456", "id": "123"}):
            with self.subTest(data=data):
                self.assertIsNotNone(source.post(data))
                with self.assertRaises(source.UnsupportedSource):
                    source.attachment(data)


if __name__ == "__main__":
    unittest.main()
