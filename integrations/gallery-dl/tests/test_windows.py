import copy
import json
from pathlib import Path
import unittest

from stash_ingest.encoding import InvalidData
from stash_ingest import windows


class WindowsTests(unittest.TestCase):
    def test_shared_server_corpus(self):
        corpus = json.loads((Path(__file__).resolve().parents[3] / "pkg/scrape/testdata/windows-v1.json").read_text())
        self.assertEqual(corpus["version"], "source-windows-v1")
        for case in corpus["cases"]:
            with self.subTest(case=case["name"]):
                original = copy.deepcopy(case)
                self.assertEqual(windows.union(case["wanted"]), case["union"])
                self.assertEqual(windows.subtract(case["wanted"], case["covered"]), case["remaining"])
                self.assertEqual(case, original)
        for window in corpus["invalid"]:
            with self.subTest(window=window), self.assertRaises(InvalidData):
                windows.normalize(window)


if __name__ == "__main__":
    unittest.main()
