import json
from pathlib import Path
import tempfile
import unittest
import uuid

from split_worker_profiles import split_profiles


class SiteWorkerProfilesTests(unittest.TestCase):
    def test_partitions_all_operations_with_independent_stable_rotation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            entries = []
            for site in ("reddit", "twitter"):
                for operation in ("download", "post.enrich", "account.list_page", "post.verify_candidate"):
                    name = site + "-" + operation
                    (root / (name + ".json")).write_text(json.dumps({"source_category": site}))
                    entries.append({"id": name, "operation": operation, "profile": name + ".json"})
            original = {"schema": "stash-gallery-dispatch-v1", "uuid": str(uuid.uuid4()), "profiles": entries}
            path = root / "worker-dispatch.json"
            path.write_text(json.dumps(original))
            result = split_profiles(path)
            self.assertEqual(set(result), {"reddit", "twitter"})
            self.assertEqual(result, split_profiles(path))
            self.assertNotEqual(result["reddit"]["uuid"], result["twitter"]["uuid"])
            self.assertNotIn(original["uuid"], [group["uuid"] for group in result.values()])
            self.assertEqual([entry for group in result.values() for entry in group["profiles"]], entries)
            self.assertEqual(json.loads(path.read_text()), original)

    def test_refuses_unsafe_service_and_duplicate_entries_before_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "source.json").write_text(json.dumps({"source_category": "../unexpected"}))
            entry = {"id": "source", "operation": "download", "profile": "source.json"}
            path = root / "worker-dispatch.json"
            document = {"schema": "stash-gallery-dispatch-v1", "uuid": str(uuid.uuid4()), "profiles": [entry]}
            path.write_text(json.dumps(document))
            with self.assertRaises(ValueError):
                split_profiles(path)
            (root / "source.json").write_text(json.dumps({"source_category": "reddit"}))
            document["profiles"].append(entry)
            path.write_text(json.dumps(document))
            with self.assertRaises(ValueError):
                split_profiles(path)
