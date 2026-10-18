from collections import Counter
import contextlib
import io
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from ci_sqlite_tests import (
    check_coverage, discover_tests, read_results, run_tests, shard_tests,
    sqlite_subpackages, test_command,
)


class SQLiteShardingTest(unittest.TestCase):
    def test_subpackage_discovery_does_not_require_generated_ui(self):
        makefile = Path(__file__).resolve().parents[1] / "Makefile"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "go.mod").write_text("module example.invalid/archive\n\ngo 1.20\n")
            (root / "Makefile").write_text(makefile.read_text())
            for package in ("pkg/sqlite", "pkg/sqlite/blob", "pkg/sqlite/future"):
                target = root / package
                target.mkdir(parents=True, exist_ok=True)
                (target / "package.go").write_text("package fixture\n")
            (root / "ui").mkdir()
            (root / "ui/ui.go").write_text(
                'package ui\nimport _ "embed"\n//go:embed build/app.js\nvar app []byte\n')
            with contextlib.chdir(root), patch.dict(os.environ, {"GOWORK": "off", "GOTOOLCHAIN": "local"}):
                unrelated = subprocess.run(["go", "list", "./..."], text=True, capture_output=True)
                self.assertNotEqual(unrelated.returncode, 0)
                self.assertIn("no matching files found", unrelated.stderr)
                self.assertEqual(sqlite_subpackages("example.invalid/archive"), [
                    "example.invalid/archive/pkg/sqlite/blob", "example.invalid/archive/pkg/sqlite/future",
                ])

    def test_discovery_and_shards_cover_tests_examples_and_fuzz_seeds_once(self):
        names = discover_tests("\n".join([
            "TestWrite", "TestWriteRollback", "TestNewFeature", "ExampleRead",
            "FuzzDecode", "BenchmarkQuery", "ok\texample/pkg/sqlite\t1.2s",
        ]))
        shards = [shard_tests(names, index, 4) for index in range(1, 5)]
        counts = Counter(name for shard in shards for name in shard)
        self.assertEqual(counts, Counter({name: 1 for name in names}))
        self.assertEqual(len(names), 5)
        self.assertEqual(shards, [shard_tests(list(reversed(names)), index, 4) for index in range(1, 5)])

    def test_bad_registry_or_shard_fails_instead_of_passing_without_tests(self):
        for output in ("ok package (cached)", "TestDuplicate\nTestDuplicate\n"):
            with self.subTest(output=output), self.assertRaises(ValueError):
                discover_tests(output)
        for shard, total in ((0, 4), (5, 4), (1, 0), (2, 2)):
            with self.subTest(shard=shard, total=total), self.assertRaises(ValueError):
                shard_tests(["TestOnly"], shard, total)

    def test_filters_are_exact_and_shell_quoted_for_make(self):
        command = test_command(["example/pkg/sqlite"], ["-json", "-run", "^(TestOne|TestTwo)$"])
        with tempfile.TemporaryDirectory() as directory:
            makefile = Path(directory) / "Makefile"
            # Execute a real Make recipe to cover both Make's dollar expansion
            # and shell quoting, rather than merely checking a command string.
            makefile.write_text("it:\n\t@" + shlex.join([sys.executable, "-c",
                "import json,sys; print(json.dumps(sys.argv[1:]))"]) + " $(GO_TEST_FLAGS)\n")
            output = subprocess.check_output([*command, "-f", str(makefile)], text=True)
        flags = json.loads(output)
        self.assertEqual(flags, ["-json", "-run", "^(TestOne|TestTwo)$"])
        self.assertIsNotNone(re.fullmatch(flags[-1], "TestOne"))
        self.assertIsNone(re.fullmatch(flags[-1], "TestOneMore"))

    def test_completion_requires_every_selected_test_once_and_successful_package(self):
        names = ["TestOne", "TestHelper"]
        timings = {"TestOne": {"result": "pass"}, "TestHelper": {"result": "skip"}}
        self.assertEqual(check_coverage(names, Counter(names), timings, True), [])
        for completed, passed in ((Counter(), True), (Counter(names + names[:1]), True),
                                  (Counter(names + ["TestUnexpected"]), True), (Counter(names), False)):
            with self.subTest(completed=completed, passed=passed):
                self.assertTrue(check_coverage(names, completed, timings, passed))
        timings["TestOne"]["result"] = "fail"
        self.assertTrue(check_coverage(names, Counter(names), timings, True))

    def test_json_results_do_not_double_count_subtests_or_other_packages(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "tests.jsonl"
            events = [
                {"Package": "sqlite", "Action": "pass", "Test": "TestOne/child", "Elapsed": 2},
                {"Package": "sqlite", "Action": "pass", "Test": "TestOne", "Elapsed": 3},
                {"Package": "other", "Action": "pass", "Test": "TestOther", "Elapsed": 4},
                {"Package": "sqlite", "Action": "skip", "Test": "TestHelper"},
                {"Package": "sqlite", "Action": "output", "Output": "ok  \tsqlite\t(cached)\n"},
                {"Package": "sqlite", "Action": "pass", "Elapsed": 5},
            ]
            path.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            completed, timings, passed, cached = read_results(path, "sqlite")
            self.assertEqual(completed, Counter(["TestOne", "TestHelper"]))
            self.assertEqual(timings["TestOne"]["seconds"], 3)
            self.assertTrue(passed)
            self.assertTrue(cached)
            self.assertFalse(read_results(path, "missing")[2])

    def test_failed_process_retains_output_and_cannot_be_successful(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "tests.jsonl"
            with contextlib.redirect_stdout(io.StringIO()):
                status = run_tests([sys.executable, "-c", "print('build failed'); raise SystemExit(2)"], path)
            self.assertEqual(status, 2)
            self.assertEqual(path.read_text(), "build failed\n")
            completed, timings, passed, cached = read_results(path, "sqlite")
            self.assertFalse(cached)
            self.assertTrue(check_coverage(["TestOne"], completed, timings, passed))


if __name__ == "__main__":
    unittest.main()
