from collections import Counter
import json
from pathlib import Path
import tempfile
import unittest

from ci_backend_tests import check_coverage, discover_tests, read_results, shard_names


def listing(packages):
    lines = []
    for package, names in packages.items():
        lines += [json.dumps({"Action": "start", "Package": package})]
        lines += [json.dumps({"Action": "output", "Package": package, "Output": name + "\n"}) for name in names]
        lines += [json.dumps({"Action": "output", "Package": package, "Output": f"ok  \t{package}\t0.1s\n"}),
                  json.dumps({"Action": "pass", "Package": package})]
    return "\n".join(lines)


class BackendShardingTest(unittest.TestCase):
    def test_registry_keeps_tests_examples_and_fuzz_per_package(self):
        registry = discover_tests(listing({
            "example/api": ["TestRoute", "ExampleClient", "FuzzDecode", "BenchmarkRoute"],
            "example/ingest": ["TestRoute", "TestCapture"],
        }) + "\nnot json\n" + json.dumps({"Action": "output", "Output": "TestNoPackage\n"}))
        self.assertEqual(registry, {
            "example/api": ["ExampleClient", "FuzzDecode", "TestRoute"],
            "example/ingest": ["TestCapture", "TestRoute"],
        })

    def test_bad_registry_fails_instead_of_passing_without_tests(self):
        for output in ("", listing({"example/api": []}), listing({"example/api": ["TestOne", "TestOne"]})):
            with self.subTest(output=output), self.assertRaises(ValueError):
                discover_tests(output)

    def test_shards_run_every_package_test_exactly_once(self):
        registry = {
            "example/api": ["TestA", "TestB", "TestShared"],
            "example/ingest": ["TestC", "TestShared"],
            "example/small": ["TestD"],
        }
        shards = [shard_names(registry, index, 3) for index in range(1, 4)]
        counts = Counter((package, name) for shard in shards for package, names in shard.items() for name in names)
        self.assertEqual(counts, Counter({(p, n): 1 for p, names in registry.items() for n in names}))
        # A shared name is selected in every package of the same shard.
        owner = [shard for shard in shards if "TestShared" in shard["example/api"]]
        self.assertEqual(len(owner), 1)
        self.assertIn("TestShared", owner[0]["example/ingest"])
        for shard, total in ((0, 3), (4, 3), (1, 0), (6, 6)):
            with self.subTest(shard=shard, total=total), self.assertRaises(ValueError):
                shard_names(registry, shard, total)

    def results(self, events):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "tests.jsonl"
            path.write_text("\n".join(json.dumps(event) for event in events) + "\nbuild output\n")
            return read_results(path)

    def test_completion_requires_selected_tests_once_and_passing_packages(self):
        expected = {"example/api": ["TestOne"], "example/other": []}
        passing = [
            {"Action": "pass", "Package": "example/api", "Test": "TestOne", "Elapsed": 2},
            {"Action": "pass", "Package": "example/api", "Test": "TestOne/sub"},
            {"Action": "pass", "Package": "example/api"},
            {"Action": "pass", "Package": "example/other"},
        ]
        self.assertEqual(check_coverage(expected, *self.results(passing)), [])
        failures = {
            "missing": passing[2:],
            "duplicate": passing[:1] + passing,
            "unexpected": [{"Action": "pass", "Package": "example/other", "Test": "TestOne"}] + passing,
            "test failed": [{**passing[0], "Action": "fail"}] + passing[1:],
            "unselected package failed": passing[:3] + [{"Action": "fail", "Package": "example/other"}],
            "package unfinished": passing[:2],
        }
        for name, events in failures.items():
            with self.subTest(name):
                self.assertNotEqual(check_coverage(expected, *self.results(events)), [])


if __name__ == "__main__":
    unittest.main()
