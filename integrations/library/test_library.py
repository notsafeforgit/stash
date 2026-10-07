import re
import unittest
from unittest.mock import Mock

from stash_autotag_with_aliases import build_combined_path_regex, find_performers, matching_ids
from stash_library import StashClient, StashError, completed_count, filter_ast
from stash_scan_and_generate import run_scan
from tag_stash_collections import auto_apply_modes, resolve_tag_id, selected_ids
from update_image_titles_stash_performer import apply_titles, resolve_performer, title_changes


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

    def test_title_repair_does_not_guess_between_duplicate_names(self):
        client = Mock()
        client.find_all.return_value = [{"id": "7", "name": "Example"}, {"id": "8", "name": "Example"}]
        with self.assertRaisesRegex(StashError, "use --performer-id"):
            resolve_performer(client, "Example", None, 20)
        client.call.assert_not_called()
        client.call.return_value = {"findPerformer": {"id": "8", "name": "Example"}}
        self.assertEqual(resolve_performer(client, None, "8", 20)["id"], "8")
        client.call.return_value = {"findPerformer": None}
        with self.assertRaises(StashError):
            resolve_performer(client, None, "9", 20)

    def test_title_repair_selection_primary_file_and_empty_only(self):
        client = Mock()
        client.find_all.return_value = [
            {"id": "1", "title": "", "visual_files": [{"path": "/album.zip/primary.jpg"}, {"path": "/different.jpg"}]},
            {"id": "2", "title": "keep", "visual_files": [{"path": "/replace.jpg"}]},
            {"id": "3", "title": "same", "visual_files": [{"path": "/same.png"}]},
            {"id": "4", "title": "", "visual_files": []},
        ]
        self.assertEqual(title_changes(client, "8", 20, True), [{"id": "1", "title": "primary"}])
        self.assertEqual(title_changes(client, "8", 20), [{"id": "1", "title": "primary"}, {"id": "2", "title": "replace"}])
        self.assertEqual(client.find_all.call_args.args[2], filter_ast(performers={"value": ["8"], "modifier": "INCLUDES"}))
        changes = [{"id": "1", "title": "primary"}, {"id": "2", "title": "replace"}]
        self.assertEqual(apply_titles(client, changes, True), 2)
        client.call.assert_not_called()

    def test_title_repair_requires_exact_success_before_later_edits(self):
        changes = [{"id": "1", "title": "first"}, {"id": "2", "title": "second"}]
        for bad in [None, {"id": "2", "title": "first"}, {"id": "1", "title": "old"}]:
            with self.subTest(bad=bad):
                client = Mock()
                client.call.return_value = {"imageUpdate": bad}
                with self.assertRaises(StashError):
                    apply_titles(client, changes)
                self.assertEqual(client.call.call_count, 1)
        client = Mock()
        client.call.side_effect = [{"imageUpdate": value} for value in changes]
        self.assertEqual(apply_titles(client, changes), 2)

    def test_scan_explicit_path_dry_run_and_admission(self):
        client = Mock()
        with self.assertRaises(StashError):
            run_scan(client, " ")
        self.assertIsNone(run_scan(client, "/media/album.zip", True))
        client.call.assert_not_called()
        client.call.return_value = {"metadataScan": "42"}
        self.assertEqual(run_scan(client, "/media/album.zip"), "42")
        self.assertEqual(client.call.call_args.args[1], {"input": {
            "paths": ["/media/album.zip"], "scanGenerateCovers": True, "scanGeneratePhashes": True,
        }})
        for bad in [None, "", True, {"id": "42"}]:
            client.call.return_value = {"metadataScan": bad}
            with self.assertRaises(StashError):
                run_scan(client, "/media/album.zip")


if __name__ == "__main__":
    unittest.main()
