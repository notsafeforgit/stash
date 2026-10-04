import re
import unittest
from unittest.mock import Mock

from stash_autotag_with_aliases import build_combined_path_regex, find_performers, matching_ids
from stash_library import StashClient, StashError, completed_count, filter_ast
from tag_stash_collections import auto_apply_modes, resolve_tag_id, selected_ids


class LibraryHelpersTests(unittest.TestCase):
    def test_bulk_only_counts_confirmed_ids_and_uses_native_result(self):
        client = StashClient("http://unused/graphql")
        client.call = Mock(side_effect=[
            {"result": {"status": "COMPLETED", "job_id": None, "selected_count": 2, "updated_ids": ["2", "1"]}},
            {"result": {"status": "COMPLETED", "job_id": None, "selected_count": 1, "updated_ids": ["3"]}},
        ])
        self.assertEqual(client.add_relationship("Scene", ["1", "2", "2", "3"], "tag_ids", "9", 2), 3)
        self.assertEqual(client.call.call_count, 2)
        query, variables = client.call.call_args_list[0].args
        self.assertIn("result: bulkSceneUpdate", query)
        self.assertIn("status job_id selected_count updated_ids", query)
        self.assertEqual(variables, {"input": {"ids": ["1", "2"], "tag_ids": {"ids": ["9"], "mode": "ADD"}}})

    def test_unconfirmed_batch_stops_subsequent_mutations(self):
        for bad in [None, [{"id": "1"}], {"status": "QUEUED", "job_id": "12"},
                    {"status": "COMPLETED", "job_id": None, "selected_count": 1, "updated_ids": ["99"]},
                    {"status": "COMPLETED", "job_id": None, "selected_count": 1, "updated_ids": []}]:
            with self.subTest(bad=bad):
                client = StashClient("http://unused/graphql")
                client.call = Mock(return_value={"result": bad})
                with self.assertRaises(StashError):
                    client.add_relationship("Image", ["1", "2"], "performer_ids", "7", 1)
                self.assertEqual(client.call.call_count, 1)

    def test_dry_run_sends_no_mutation(self):
        client = StashClient("http://unused/graphql")
        client.call = Mock()
        self.assertEqual(client.add_relationship("Gallery", ["1", "1", "2"], "performer_ids", "7", dry_run=True), 2)
        client.call.assert_not_called()

    def test_bounded_stable_pagination_and_canonical_filter(self):
        client = StashClient("http://unused/graphql")
        client.call = Mock(side_effect=[{"findScenes": {"scenes": [{"id": "1"}, {"id": "2"}]}}, {"findScenes": {"scenes": [{"id": "3"}]}}])
        self.assertEqual(selected_ids(client, "scenes", "movie", "8", 2), ["1", "2", "3"])
        for page, call in enumerate(client.call.call_args_list, 1):
            query, variables = call.args
            self.assertIn("scene_filter_ast: $ast", query)
            self.assertNotIn("SceneFilterType", query)
            self.assertEqual(variables["ast"], filter_ast(groups={"value": ["8"], "modifier": "INCLUDES"}))
            self.assertEqual(variables["filter"], {"page": page, "per_page": 2, "sort": "id", "direction": "ASC"})

    def test_repeated_page_cannot_loop_forever(self):
        client = StashClient("http://unused/graphql")
        client.call = Mock(return_value={"findImages": {"images": [{"id": "1"}]}})
        with self.assertRaisesRegex(StashError, "no progress"):
            client.find_all("Image", per_page=1)

    def test_alias_paths_keep_boundaries_and_exclude_existing_links(self):
        regex = build_combined_path_regex("Jane Doe", ["JD", "jane doe", "A+B"])
        for path in ["/Jane_Doe/a.mp4", "C:\\JD\\a.jpg", "/A+B/title.mp4"]:
            self.assertIsNotNone(re.search(regex, path), path)
        self.assertIsNone(re.search(regex, "/NotJDmore/a.mp4"))
        client = Mock()
        client.find_all.return_value = [{"id": "1"}]
        self.assertEqual(matching_ids(client, "Gallery", {"id": "7", "name": "Jane Doe", "alias_list": ["JD"]}, 10), ["1"])
        ast = client.find_all.call_args.kwargs["ast"]
        self.assertEqual(ast["root"]["group"]["operator"], "AND")
        self.assertEqual(ast["root"]["group"]["children"][1], {"condition": {"field": "performers", "value": {"value": ["7"], "modifier": "EXCLUDES"}}})

    def test_missing_requested_performer_does_not_expand_to_library(self):
        client = Mock()
        client.call.return_value = {"findPerformer": None}
        self.assertEqual(find_performers(client, ["7"], 20), [])
        client.find_all.assert_not_called()

    def test_tag_ambiguity_and_target_validation(self):
        client = Mock()
        client.find_all.return_value = [{"id": "1", "name": "Tag"}, {"id": "2", "name": "TAG"}]
        self.assertEqual(resolve_tag_id(client, "Tag", 2), "1")
        with self.assertRaisesRegex(StashError, "refusing to guess"):
            resolve_tag_id(client, "tag", 2)
        self.assertEqual(auto_apply_modes("gallery", "auto"), ["images"])
        with self.assertRaises(StashError):
            auto_apply_modes("group", "both")


if __name__ == "__main__":
    unittest.main()
