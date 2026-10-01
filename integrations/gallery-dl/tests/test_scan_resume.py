from types import SimpleNamespace
import unittest

from stash_ingest.scan_resume import PREFIX, legacy_cursor


class LegacyCursorTests(unittest.TestCase):
    def test_original_hash_format_including_large_ids_and_ascii_escaping(self):
        job = SimpleNamespace(extractor=SimpleNamespace(category="reddit"),
                              archive=SimpleNamespace(keygen=lambda data: "reddit_post5_5"))
        path = SimpleNamespace(kwdict={}, filename="fallback")
        self.assertEqual(legacy_cursor(job, path), PREFIX + "38963f4715842ae02d1d49cb39e84198e6a2fd09b7e58da94d6a8690e0fa3015")
        job.archive = None
        path.kwdict = {"id": "é", "tweet_id": 9007199254740993, "num": 1, "filename": "花"}
        self.assertEqual(legacy_cursor(job, path), PREFIX + "c8aea22d6b4030a3ee40b170a594c276aa596e5d97b12df319d984a73e1de9ed")
        path.kwdict = {"_url": "https://media.invalid/full.jpg"}
        self.assertEqual(legacy_cursor(job, path), PREFIX + "cdd4bf440cc4bceca2998f2d6a545072dc1c872c9c3e4a061f645bfa0083aa61")
