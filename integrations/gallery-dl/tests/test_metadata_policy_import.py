import unittest

from stash_ingest.encoding import InvalidData, digest, encode, native_json
from stash_ingest.metadata_policy_import import canonical_binding, effective_settings, literal_defaults, snapshot


class MetadataPolicyImportTests(unittest.TestCase):
    def binding(self, mappings):
        document = snapshot(b'create_missing_tags = False\n', b'{}', b'{}', "1.14.1", "2026-09-29T00:00:00Z")
        return {"uuid": "fae0a2b3-e5c2-42cf-bdd1-27a14c922f93", "document": document, "folder_sources": [],
                "policy": {"collection_uuid": "2350c460-7cbb-4c3c-8833-0799ecc048cd", "expected_revision": 0,
                           "expected_collection_revision": 1, "origin": "migration", "reason": "Retain exact name rules",
                           "definition": {"enabled": False, "apply_to_scans": True,
                                          "rules": {"scene": {"mappings": mappings}}}},
                "dispositions": {"python/create_missing_tags": {"action": "mapped", "reason": "Only match existing names"}}}

    def test_relationship_name_mappings_survive_frozen_binding(self):
        mappings = {"performers": {"value": ["Known alias"], "reference_names": True},
                    "studio": {"value": "Studio alias", "reference_names": True},
                    "tags": {"jq": ".source.payload.tags // empty", "reference_names": True},
                    "groups": {"value": [{"name": "Album", "scene_index": 3}], "reference_names": True}}
        binding = canonical_binding(self.binding(mappings))
        self.assertEqual(mappings, binding["policy"]["definition"]["rules"]["scene"]["mappings"])
        self.assertEqual(binding, canonical_binding(binding))
        self.assertEqual(["Known alias"], mappings["performers"]["value"])

    def test_false_name_switches_have_the_same_native_digest_as_omitted(self):
        value = {"value": ["56c9895d-03c1-4a93-8c0d-fbd99d27de22"]}
        expected = canonical_binding(self.binding({"performers": value}))
        explicit = canonical_binding(self.binding({"performers": {**value, "performer_names": False, "reference_names": False}}))
        self.assertEqual(expected, explicit)
        self.assertEqual(digest(native_json(expected, 1 << 20)), digest(native_json(explicit, 1 << 20)))
        legacy = canonical_binding(self.binding({"performers": {"value": ["Alias"], "performer_names": True}}))
        self.assertTrue(legacy["policy"]["definition"]["rules"]["scene"]["mappings"]["performers"]["performer_names"])

    def test_typed_fallbacks_preserve_empty_values_and_frozen_digests(self):
        for value in (None, False, 0, "", [], {"note": "default"}, ["56c9895d-03c1-4a93-8c0d-fbd99d27de22"]):
            with self.subTest(value=value):
                mapping = {"jq": "empty", "fallback": value}
                binding = canonical_binding(self.binding({"performers": mapping}))
                self.assertEqual(mapping, binding["policy"]["definition"]["rules"]["scene"]["mappings"]["performers"])
                self.assertEqual(binding, canonical_binding(binding))
                without = canonical_binding(self.binding({"performers": {"jq": "empty"}}))
                self.assertNotEqual(digest(native_json(without, 1 << 20)), digest(native_json(binding, 1 << 20)))
        for mapping in ({"value": [], "fallback": []}, {"jq": "", "value": None, "fallback": None}):
            with self.subTest(mapping=mapping), self.assertRaises(InvalidData):
                canonical_binding(self.binding({"performers": mapping}))

    def test_invalid_name_switches_are_rejected_before_submission(self):
        for flag in ("reference_names", "performer_names"):
            for value in (None, 0, 1, "true", [], {}):
                with self.subTest(flag=flag, value=value), self.assertRaises(InvalidData):
                    canonical_binding(self.binding({"performers": {"value": ["Alias"], flag: value}}))
        for field, mapping in (("title", {"value": "Title", "reference_names": True}),
                               ("studio", {"value": "Studio", "performer_names": True}),
                               ("performers", {"value": ["Alias"], "reference_names": True, "performer_names": True})):
            with self.subTest(field=field), self.assertRaises(InvalidData):
                canonical_binding(self.binding({field: mapping}))

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
