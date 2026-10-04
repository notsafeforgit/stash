import copy
from pathlib import Path
import unittest

from stash_ingest.discovery_fetch import MAX_RECORDS, page_cursor, profile_platform, validate_page
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.metadata_bundle import Bundle


class DiscoveryContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        path = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/discovery-pages-v1.json"
        cls.fixture = decode(path.read_bytes(), preserve_numbers=True)

    def test_shared_native_pages_keep_exact_records_and_metadata(self):
        for case in self.fixture["pages"]:
            with self.subTest(case=case["name"]):
                page = case["page"]
                parsed = validate_page(page, page["url"], page["extractor_version"], page["cursor"])
                self.assertEqual(encode(parsed), encode(page))
                checkpoint = Bundle(page["url"], page["extractor_version"], max_records=MAX_RECORDS).checkpoint()
                checkpoint["records"] = parsed["records"]
                bundle = Bundle(page["url"], page["extractor_version"], checkpoint, max_records=MAX_RECORDS)
                actual = [bundle.metadata(i, with_parent=True) for i in range(len(page["records"]))]
                self.assertEqual(encode(actual), encode(case["metadata"]))

    def test_supported_profile_forms_match_native_contract(self):
        for case in self.fixture["profiles"]:
            with self.subTest(url=case["url"]):
                if case["platform"]:
                    self.assertEqual(profile_platform(case["url"]), case["platform"])
                else:
                    with self.assertRaises(InvalidData):
                        profile_platform(case["url"])
        with self.assertRaises(InvalidData):
            profile_platform("https://x.com/\ud800/timeline")

    def test_shared_native_invalid_pages_are_rejected(self):
        for case in self.fixture["rejected"]:
            with self.subTest(case=case["name"]):
                page = copy.deepcopy(self.fixture["pages"][0]["page"])
                parent = page
                for part in case["path"][:-1]:
                    parent = parent[part]
                key = case["path"][-1]
                if case.get("remove"):
                    del parent[key]
                else:
                    parent[key] = case["value"]
                with self.assertRaises(InvalidData):
                    # Supply the mutated request too: it must fail structurally,
                    # not merely because it differs from the original fixture.
                    validate_page(page, page["url"], page["extractor_version"], page.get("cursor"))

    def test_cursor_bounds_apply_to_utf8_bytes_and_service(self):
        self.assertEqual(page_cursor("twitter", {"cursor": "é" * 4096}), {"cursor": "é" * 4096})
        for platform, cursor in (("twitter", {"cursor": "é" * 4097}),
                                 ("twitter", {"cursor": "\ud800"}),
                                 ("reddit", {"after": "t3_ABC"}),
                                 ("reddit", {"after": "t3_valid", "cursor": "extra"})):
            with self.subTest(cursor=cursor), self.assertRaises(InvalidData):
                page_cursor(platform, cursor)


if __name__ == "__main__":
    unittest.main()
