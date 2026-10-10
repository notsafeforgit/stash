import json
from pathlib import Path
import tempfile
import unittest
import uuid

from split_worker_profiles import split_profiles


class SiteWorkerProfilesTests(unittest.TestCase):
    def test_initial_and_incremental_lists_partition_sites_without_changing_profiles(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            entries, initial_ids = [], []
            for site in ("reddit", "twitter"):
                for suffix, operation in (("daily", "download"), ("full-history", "download"), ("metadata", "post.enrich")):
                    name = site + "-" + suffix
                    profile = {"source_category": site, "gallery": {"skip": True if suffix == "full-history" else "abort:4"}}
                    (root / (name + ".json")).write_text(json.dumps(profile))
                    entries.append({"id": name, "operation": operation, "profile": name + ".json"})
                    if suffix == "full-history":
                        initial_ids.append(name)
            path = root / "worker-dispatch.json"
            path.write_text(json.dumps({"schema": "stash-gallery-dispatch-v1", "uuid": str(uuid.uuid4()), "profiles": entries}))
            before = {p.name: p.read_bytes() for p in root.iterdir()}
            initial = split_profiles(path, lane="initial", initial_profiles=initial_ids)
            incremental = split_profiles(path, lane="incremental", initial_profiles=initial_ids)
            self.assertEqual(set(initial), {"reddit", "twitter"})
            self.assertEqual(initial, split_profiles(path, lane="initial", initial_profiles=initial_ids))
            for site in initial:
                self.assertEqual([e["id"] for e in initial[site]["profiles"]], [site + "-full-history", site + "-metadata"])
                self.assertEqual([e["id"] for e in incremental[site]["profiles"]], [site + "-daily", site + "-metadata"])
                self.assertNotEqual(initial[site]["uuid"], incremental[site]["uuid"])
            self.assertEqual({p.name: p.read_bytes() for p in root.iterdir()}, before)
            for options in ({"lane": "initial"}, {"lane": "initial", "initial_profiles": ["missing"]},
                            {"lane": "incremental", "initial_profiles": ["reddit-daily"]},
                            {"lane": "initial", "initial_profiles": ["reddit-metadata"]}):
                with self.assertRaises(ValueError):
                    split_profiles(path, **options)

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
