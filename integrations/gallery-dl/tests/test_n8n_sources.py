from copy import deepcopy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, urlsplit
import uuid

from stash_ingest import n8n_sources, source_accounts, source_lists, source_policy
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, encode
from test_source_management import ENDPOINT, Remote, STAMP
from helpers import ROOT, PRODUCER


def policy_definition():
    return {"enabled": True, "apply_to_scans": False, "rules": {
        kind: {"on_create": True, "on_existing": True, "skip_organized_on_create": True,
               "mark_organized": False, "filename_title_fallback": True,
               "mappings": {"title": {"jq": ".source.metadata.title | select(type == \"string\" and length > 0)"}}}
        for kind in ("scene", "image")}}


class SourcesRemote(Remote):
    def __init__(self):
        super().__init__()
        self.policy_history = {}
        self.policy_puts = []
        self.drop_policy = False
        self.policies = source_policy.SourcePolicyClient(ENDPOINT)
        self.policies.request = Mock(side_effect=self.policy_request)

    def request(self, method, path, value=None):
        if path.startswith("/source-accounts/lookup?"):
            return []
        return super().request(method, path, value)

    def policy_request(self, method, path, value=None):
        route = urlsplit(path)
        parts = route.path.strip("/").split("/")
        assert parts[0] == "collections" and parts[2] == "metadata-policy"
        identity = parts[1]
        rows = self.policy_history.setdefault(identity, [])
        if method == "GET":
            if len(parts) == 4:
                assert parts[3] == "history"
                assert parse_qs(route.query) == {"after": ["0"], "limit": ["1"]}
                return deepcopy(rows[:1])
            return deepcopy(rows[-1]) if rows else None
        assert method == "PUT" and self.histories[identity][-1]["revision"] == value["expected_collection_revision"]
        if rows:
            raise Unavailable("conflict", 409)
        self.policy_puts.append(deepcopy(value))
        rows.append({"collection_uuid": identity, "revision": 1,
                     "collection_revision": value["expected_collection_revision"], "definition": deepcopy(value["definition"]),
                     "origin": "review", "reason": value["reason"], "created_at": STAMP})
        if self.drop_policy:
            self.drop_policy = False
            raise Unavailable("network_unavailable")
        return deepcopy(rows[-1])


class NativeSourceWorkflowsTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name)
        for name in ("state", "locks"):
            (self.path / name).mkdir()
        self.reddit = self.path / "reddit-list.txt"
        self.twitter = self.path / "twitter-list.txt"
        self.reddit.write_bytes(b"# Keep this comment\r\nhttps://reddit.com/r/example/\r\n")
        self.twitter.write_bytes(b"# Keep this comment\nhttps://x.com/i/user/999 # another account\n")
        config = {"version": 1, "root_uuid": ROOT, "locks": str(self.path / "locks"), "state": str(self.path / "state"),
                  "lists": {"reddit": str(self.reddit), "twitter": str(self.twitter)}, "new_source_policy": policy_definition()}
        self.config = self.path / "runtime.json"
        self.config.write_bytes(encode(config))
        self.runtime = n8n_sources.Runtime(self.config)
        self.remote = SourcesRemote()
        self.node = str(uuid.uuid4())

    def request(self, action="add", platform="reddit", identity="Example", execution="100"):
        return n8n_sources.request(PRODUCER, ENDPOINT, ROOT, action, platform, identity, "workflow", execution, self.node, 0)

    def run_request(self, call, value):
        return n8n_sources.run(self.remote.app, self.remote.policies, self.remote.producer, self.runtime, value, call)

    def test_new_source_preserves_list_and_initializes_each_policy(self):
        result = self.run_request(*self.request())
        self.assertTrue(result["complete"])
        self.assertEqual(result["scrape_completion"], "not_checked")
        self.assertEqual(len(self.remote.histories), 6)
        self.assertEqual(len(self.remote.policy_puts), 6)
        self.assertEqual(self.reddit.read_bytes(), b"# Keep this comment\r\nhttps://reddit.com/r/example/\r\nhttps://reddit.com/user/Example/submitted/\r\n")
        self.assertTrue(all(row[-1]["account_uuid"] is None for row in self.remote.histories.values()))
        self.assertTrue(all(not row["definition"]["apply_to_scans"] for row in self.remote.policy_puts))

    def test_lost_policy_response_resumes_original_collections_and_definitions(self):
        call, value = self.request()
        self.remote.drop_policy = True
        before = self.reddit.read_bytes()
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        self.assertEqual(self.reddit.read_bytes(), before)
        planned = (self.runtime.state / call / "plan.json").read_bytes()
        changed = json.loads(self.config.read_bytes())
        changed["new_source_policy"]["rules"]["scene"]["mappings"]["title"]["jq"] = '"Later default"'
        self.config.write_bytes(encode(changed))
        self.runtime = n8n_sources.Runtime(self.config)
        self.assertTrue(self.run_request(call, value)["complete"])
        self.assertEqual((self.runtime.state / call / "plan.json").read_bytes(), planned)
        self.assertEqual(len(self.remote.histories), 6)
        self.assertEqual(len(self.remote.policy_puts), 6)
        self.assertEqual(self.remote.policy_puts[-1]["definition"], policy_definition())

    def test_completed_replay_preserves_later_list_and_source_edits(self):
        call, value = self.request(platform="twitter", identity="123")
        original = self.run_request(call, value)
        self.twitter.write_bytes(b"# Later owner removed the account\n")
        identity = next(iter(self.remote.histories))
        self.remote.edit(identity, state="disabled")
        puts = len(self.remote.puts)
        self.assertEqual(self.run_request(call, value), original)
        self.assertEqual(len(self.remote.puts), puts)
        self.assertEqual(self.twitter.read_bytes(), b"# Later owner removed the account\n")

    def test_same_execution_cannot_change_the_input_or_action(self):
        call, value = self.request()
        self.run_request(call, value)
        other, changed = self.request(identity="Different")
        self.assertEqual(other, call)
        with self.assertRaises(Unavailable):
            self.run_request(other, changed)
        other, changed = self.request(action="remove")
        self.assertEqual(other, call)
        with self.assertRaises(Unavailable):
            self.run_request(other, changed)

    def test_pending_replay_does_not_overwrite_later_policy_edit(self):
        call, value = self.request()
        self.remote.drop_policy = True
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        rows = next(v for v in self.remote.policy_history.values() if v)
        rows.append({**deepcopy(rows[0]), "revision": 2, "reason": "Owner disabled the policy",
                     "definition": {**deepcopy(rows[0]["definition"]), "enabled": False}})
        puts = len(self.remote.puts)
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        self.assertEqual(len(self.remote.puts), puts)
        self.assertEqual(len(self.remote.policy_puts), 1)
        self.assertNotIn(b"/user/Example/", self.reddit.read_bytes())

    def test_existing_source_policy_and_scope_are_preserved(self):
        self.run_request(*self.request())
        source_id = next(iter(self.remote.histories))
        self.remote.policy_history[source_id][0]["definition"]["rules"]["scene"]["mappings"]["title"]["jq"] = '"Explicit owner title"'
        policies = deepcopy(self.remote.policy_history)
        result = self.run_request(*self.request(execution="101"))
        self.assertFalse(result["added"])
        self.assertEqual(self.remote.policy_history, policies)
        self.assertEqual(len(self.remote.puts), 6)

    def test_remove_only_exact_account_preserves_comments_and_other_sources(self):
        self.run_request(*self.request(platform="twitter", identity="123"))
        self.twitter.write_bytes(self.twitter.read_bytes() + b"https://twitter.com/i/user/123 # duplicate\n")
        result = self.run_request(*self.request("remove", "twitter", "123", "101"))
        self.assertEqual(result["removedCount"], 2)
        self.assertEqual(result["removedIds"], ["123"])
        self.assertEqual(self.twitter.read_bytes(), b"# Keep this comment\nhttps://x.com/i/user/999 # another account\n")
        self.assertEqual(next(iter(self.remote.histories.values()))[-1]["state"], "disabled")
        self.assertEqual(len(self.remote.policy_puts), 1)

    def test_missing_removal_does_not_create_sources_or_policies(self):
        result = self.run_request(*self.request("remove", "twitter", "123"))
        self.assertEqual(result["status"], "noop")
        self.assertEqual(self.remote.histories, {})
        self.assertEqual(self.remote.policy_puts, [])

    def test_changed_list_during_pending_operation_is_not_rewritten(self):
        call, value = self.request()
        self.remote.drop_policy = True
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        self.reddit.write_bytes(b"# New list edit\n")
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        self.assertEqual(self.reddit.read_bytes(), b"# New list edit\n")
        self.assertEqual(len(self.remote.policy_puts), 1)

    def test_lost_local_completion_recovers_after_list_publication(self):
        call, value = self.request()
        save = n8n_sources.save
        def drop_receipt(path, value):
            if path.name == "receipt.json":
                raise OSError("Power failed before receipt publication")
            return save(path, value)
        with patch.object(n8n_sources, "save", side_effect=drop_receipt), self.assertRaises(OSError):
            self.run_request(call, value)
        after = self.reddit.read_bytes()
        self.assertTrue(self.run_request(call, value)["complete"])
        self.assertEqual(self.reddit.read_bytes(), after)
        self.assertEqual(len(self.remote.puts), 6)
        self.assertEqual(len(self.remote.policy_puts), 6)

    def test_unknown_performer_links_stop_without_using_names(self):
        identity = str(uuid.uuid4())
        self.remote.app.request = Mock(side_effect=[
            {"uuid": identity, "kind": "performer", "local_id": 10, "revision": 1},
            {"requested_uuid": identity, "performer": {"uuid": identity, "revision": 1, "state": "active", "name": "Example"}, "accounts": []},
        ])
        before = self.reddit.read_bytes()
        with self.assertRaisesRegex(Unavailable, "performer_has_no_linked"):
            self.run_request(*self.request("remove-performer", "reddit", "10"))
        self.assertEqual(self.reddit.read_bytes(), before)
        self.assertEqual(self.remote.puts, [])

    def test_invalid_runtime_and_shell_inputs_fail_before_remote_mutation(self):
        for identity in ("a'b", "$(whoami)", "../Example", "me"):
            with self.subTest(identity=identity), self.assertRaises(InvalidData):
                self.request(identity=identity)
        config = json.loads(self.config.read_bytes())
        config["new_source_policy"]["apply_to_scans"] = True
        self.config.write_bytes(encode(config))
        with self.assertRaises(InvalidData):
            n8n_sources.Runtime(self.config)
        self.assertEqual(self.remote.puts, [])

    def test_empty_optional_policy_fields_use_the_native_canonical_form(self):
        value = policy_definition()
        value["rules"]["scene"]["organized_requires"] = []
        value["rules"]["scene"]["mappings"]["title"]["reference_names"] = False
        self.assertEqual(source_policy.definition(value), policy_definition())
        self.assertIn("organized_requires", value["rules"]["scene"], "normalization must not mutate caller state")
        del value["rules"]["image"]["on_existing"]
        with self.assertRaises(InvalidData):
            source_policy.definition(value)

    def test_ambiguous_account_identifiers_require_review_before_any_write(self):
        rows = [{"uuid": str(uuid.uuid4()), "namespace": "native:reddit", "revision": 1,
                 "more_identifiers": False, "identifiers": []} for _ in range(2)]
        for row in rows:
            row["canonical_uuid"] = row["uuid"]
        self.remote.app.request = Mock(return_value=rows)
        with self.assertRaises(Unavailable):
            self.run_request(*self.request())
        self.assertEqual(self.remote.puts, [])
        self.assertEqual(self.remote.policy_puts, [])
        self.assertNotIn(b"/user/Example/", self.reddit.read_bytes())


if __name__ == "__main__":
    unittest.main()
