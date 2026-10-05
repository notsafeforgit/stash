import unittest

from stash_ingest.encoding import InvalidData, encode
from stash_ingest.metadata_policy_import import effective_settings, literal_defaults, snapshot


class MetadataPolicyImportTests(unittest.TestCase):
    def test_layers_retain_original_values_and_effective_precedence(self):
        document = snapshot(b'blacklist = ["rating"]\nskip_organized = True\n',
                            encode({"blacklist": '["rating","details"]', "filename_title_fallback": True}),
                            b'{"skip_organized":false}', "1.14.1", "2026-09-29T00:00:00Z")
        self.assertEqual(["rating"], document["values"]["python/blacklist"])
        self.assertEqual('["rating","details"]', document["values"]["declared/blacklist"])
        self.assertEqual({"blacklist": ["rating", "details"], "skip_organized": False, "filename_title_fallback": True},
                         effective_settings(document))
        self.assertEqual(3, len(document["source_files"]))

    def test_python_is_read_without_execution_and_unknown_values_survive(self):
        self.assertEqual({"new_option": {"value": "keep me"}}, literal_defaults(b'new_option = {"value": "keep me"}\n'))
        for body in (b'import os\nx = 1', b'x = open("/tmp/unexpected-write", "w")', b'x = 1\nx = 2',
                     b'if True:\n x = 1', b'x = float("nan")', b'x = {1,2}', b'_hidden = 1'):
            with self.subTest(body=body), self.assertRaises(InvalidData):
                literal_defaults(body)


if __name__ == "__main__":
    unittest.main()
