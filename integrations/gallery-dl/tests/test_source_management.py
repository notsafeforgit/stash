from copy import deepcopy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock
from urllib.parse import parse_qs, urlsplit
import uuid

from stash_ingest.client import Client, Unavailable
from stash_ingest.encoding import InvalidData, decode, digest, encode
from stash_ingest.publication_lock import PublicationBusy
from stash_ingest.source_management import (DEFINITION, SourceManagementClient, inspect, intent, load_plan,
                                            management_lock, prepare, validate_plan)
from helpers import ROOT, PRODUCER

STAMP = "2026-10-07T00:00:00Z"
ENDPOINT = "http://fixture.invalid"


def spec(operation="ensure", count=2):
    return {"root_uuid": ROOT, "operation": operation, "reason": "Requested source change", "targets": [
        {"label": f"Source {i}", "kind": "account", "namespace": "native:reddit", "state": "active",
         "target_url": f"https://www.reddit.com/user/example{i}/submitted/?sort=new",
         "root_uuid": ROOT, "account_uuid": None, "path_prefix": "."} for i in range(count)]}


class Remote:
    def __init__(self):
        self.histories = {}
        self.puts = []
        self.drop_next = False
        self.app = SourceManagementClient(ENDPOINT)
        self.app.request = Mock(side_effect=self.request)
        self.producer = Client(ENDPOINT, PRODUCER)
        self.producer.capabilities = Mock(return_value={"collection_lookup": True, "root_uuids": [ROOT]})
        self.producer._request = Mock(side_effect=self.lookup)

    def add(self, value, identity=None, reason="Initial source"):
        identity = identity or str(uuid.uuid4())
        history = self.histories.setdefault(identity, [])
        row = {**deepcopy(value), "uuid": identity, "revision": len(history)+1, "created_at": STAMP,
               "origin": "review", "reason": reason, "recorded_at": STAMP}
        history.append(row)
        return {key: item for key, item in row.items() if key not in ("origin", "reason", "recorded_at")}

    def edit(self, identity, **changes):
        value = {key: self.histories[identity][-1][key] for key in DEFINITION}
        return self.add({**value, **changes}, identity, reason="Later owner edit")

    def request(self, method, path, value=None):
        route = urlsplit(path)
        parts = route.path.strip("/").split("/")
        assert parts[0] == "collections"
        identity = parts[1]
        rows = self.histories.get(identity)
        if method == "GET":
            if not rows:
                raise Unavailable("not_found", 404)
            if len(parts) == 3 and parts[2] == "history":
                query = parse_qs(route.query)
                return deepcopy(rows[int(query["after"][0]):int(query["after"][0])+int(query["limit"][0])])
            return {key: deepcopy(item) for key, item in rows[-1].items() if key not in ("origin", "reason", "recorded_at")}
        assert method == "PUT" and identity == value["uuid"]
        if value["expected_revision"] != len(rows or []):
            raise Unavailable("stale", 409)
        self.puts.append(deepcopy(value))
        result = self.add({key: value[key] for key in DEFINITION}, identity, value["reason"])
        if self.drop_next:
            self.drop_next = False
            raise Unavailable("network_unavailable")
        return result

    def lookup(self, method, path, body, **_):
        assert (method, path) == ("POST", "/collections/lookup")
        value = decode(body)
        results = []
        for target in value["targets"]:
            candidates = sorted((
                {"collection_uuid": identity, "collection_revision": history[-1]["revision"], "state": history[-1]["state"]}
                for identity, history in self.histories.items()
                if history[-1]["target_url"] == target and history[-1]["root_uuid"] == value["root_uuid"]
            ), key=lambda row: row["collection_uuid"])
            results.append({"target_url": target, "candidates": candidates[:128], "has_more": len(candidates) > 128})
        return {"root_uuid": value["root_uuid"], "targets": results}


class SourceManagementTests(unittest.TestCase):
    def test_explicit_retired_restoration_recovers_lost_response_with_same_identity(self):
        remote = Remote()
        before = remote.add({**spec()["targets"][0], "state": "retired"})
        value = {**{key: before[key] for key in DEFINITION}, "state": "active",
                 "uuid": before["uuid"], "expected_revision": before["revision"], "reason": "Restore collection"}
        remote.drop_next = True
        with self.assertRaisesRegex(Unavailable, "network_unavailable"):
            remote.app.apply(value)
        restored = remote.app.apply(value)
        self.assertEqual((restored["uuid"], restored["revision"], restored["state"]), (before["uuid"], 2, "active"))
        self.assertEqual(len(remote.puts), 1)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.remote = Remote()
        self.number = 0

    def prepare(self, value=None):
        self.number += 1
        output = self.directory / f"plan-{self.number}.json"
        sha = prepare(self.remote.app, self.remote.producer, value or spec(), output)
        return output, sha, load_plan(output, sha, self.remote.app, self.remote.producer)

    def inspect(self, plan, apply=False):
        return inspect(self.remote.app, self.remote.producer, plan, apply=apply)

    def test_registration_reuses_selected_collection_without_relabeling_or_changing_attribution(self):
        value = spec()
        before = self.remote.add({**value["targets"][0], "label": "Owner label", "kind": "legacy_catalog",
                                  "account_uuid": str(uuid.uuid4()), "path_prefix": "Existing folder"})
        output, sha, plan = self.prepare(value)
        self.assertEqual(self.remote.puts, [])
        self.assertEqual(plan["entries"][0], {"before": before, "input": None})
        self.assertEqual(output.stat().st_mode & 0o777, 0o600)
        self.assertEqual(digest(output.read_bytes()), sha)
        result = self.inspect(plan, apply=True)
        self.assertTrue(result["complete"])
        self.assertEqual(result["states"], ["unchanged", "completed"])
        self.assertEqual(result["scrape_completion"], "not_checked")
        self.assertEqual(self.remote.app.collection(before["uuid"]), before)
        self.assertEqual(len(self.remote.puts), 1)
        self.assertIsNone(self.remote.puts[0]["account_uuid"])
        self.assertTrue(self.inspect(plan, apply=True)["complete"])
        self.assertEqual(len(self.remote.puts), 1)

    def test_lost_reply_and_process_restart_resume_original_collection_ids(self):
        output, sha, plan = self.prepare()
        self.remote.drop_next = True
        with self.assertRaisesRegex(Unavailable, "network_unavailable"):
            self.inspect(plan, apply=True)
        self.assertEqual(len(self.remote.puts), 1)
        resumed = load_plan(output, sha, self.remote.app, self.remote.producer)
        self.assertEqual(resumed, plan)
        self.assertEqual(self.inspect(resumed)["states"], ["completed", "pending"])
        self.assertTrue(self.inspect(resumed, apply=True)["complete"])
        self.assertEqual([row["uuid"] for row in self.remote.puts], [row["input"]["uuid"] for row in plan["entries"]])

    def test_preflight_conflict_does_not_partially_create_other_sources(self):
        _, _, plan = self.prepare()
        self.remote.add(spec()["targets"][1])
        result = self.inspect(plan, apply=True)
        self.assertTrue(result["needs_review"])
        self.assertFalse(result["complete"])
        self.assertEqual(self.remote.puts, [])

    def test_disabled_retired_and_ambiguous_existing_sources_are_never_implicitly_reactivated(self):
        for state in ("disabled", "retired", "ambiguous"):
            with self.subTest(state=state):
                self.remote = Remote()
                self.remote.add({**spec()["targets"][0], "state": "active" if state == "ambiguous" else state})
                if state == "ambiguous":
                    self.remote.add(spec()["targets"][0])
                with self.assertRaises(Unavailable):
                    self.prepare()
                self.assertEqual(self.remote.puts, [])

    def test_collection_limited_token_cannot_claim_a_source_is_missing(self):
        self.remote.producer.capabilities.return_value["root_uuids"] = []
        with self.assertRaisesRegex(Unavailable, "requires_root_grant"):
            self.prepare()
        self.remote.producer._request.assert_not_called()
        self.remote.app.request.assert_not_called()

    def test_disable_preserves_definition_and_never_creates_a_missing_source(self):
        value = spec("disable", 3)
        first = self.remote.add({**value["targets"][0], "label": "Custom label", "account_uuid": str(uuid.uuid4()), "path_prefix": "Purchased"})
        second = self.remote.add({**value["targets"][1], "state": "retired"})
        _, _, plan = self.prepare(value)
        self.assertTrue(self.inspect(plan, apply=True)["complete"])
        self.assertEqual(len(self.remote.puts), 1)
        saved = self.remote.app.collection(first["uuid"])
        self.assertEqual(saved, {**first, "revision": 2, "state": "disabled"})
        self.assertEqual(self.remote.app.collection(second["uuid"]), second)
        self.assertEqual(len(self.remote.histories), 2)

    def test_historical_ack_is_recovered_without_undoing_a_later_owner_change(self):
        _, _, plan = self.prepare(spec(count=1))
        value = plan["entries"][0]["input"]
        self.remote.app.apply(value)
        self.remote.edit(value["uuid"], state="disabled", label="Owner decision")
        recovered = self.remote.app.recover(value)
        self.assertEqual(recovered["revision"], 1)
        result = self.inspect(plan, apply=True)
        self.assertEqual(result["states"], ["changed"])
        self.assertTrue(result["needs_review"])
        self.assertEqual(len(self.remote.puts), 1)
        self.assertEqual(self.remote.app.collection(value["uuid"])["label"], "Owner decision")

    def test_renamed_target_or_new_ambiguity_is_not_substituted_on_retry(self):
        for action in ("rename", "duplicate"):
            with self.subTest(action=action):
                self.remote = Remote()
                before = self.remote.add(spec()["targets"][0])
                _, _, plan = self.prepare(spec(count=1))
                if action == "rename":
                    self.remote.edit(before["uuid"], target_url=before["target_url"] + "&changed=1")
                else:
                    self.remote.add(spec()["targets"][0])
                self.assertTrue(self.inspect(plan, apply=True)["needs_review"])
                self.assertEqual(self.remote.puts, [])

    def test_plan_bytes_endpoint_and_producer_are_bound_and_existing_file_is_preserved(self):
        output, sha, plan = self.prepare()
        before = output.read_bytes()
        with self.assertRaises(InvalidData):
            prepare(self.remote.app, self.remote.producer, spec(), output)
        self.assertEqual(output.read_bytes(), before)
        with self.assertRaises(InvalidData):
            load_plan(output, "0" * 64, self.remote.app, self.remote.producer)
        other = Client(ENDPOINT, str(uuid.uuid4()))
        with self.assertRaises(InvalidData):
            load_plan(output, sha, self.remote.app, other)
        other_app = SourceManagementClient("http://other.invalid")
        with self.assertRaises(InvalidData):
            inspect(other_app, self.remote.producer, plan, apply=True)
        self.assertEqual(self.remote.puts, [])

    def test_modified_plan_cannot_repurpose_the_operation_or_cross_its_root(self):
        _, _, plan = self.prepare()
        for key, value in (("state", "disabled"), ("root_uuid", str(uuid.uuid4())), ("path_prefix", "../outside"),
                           ("account_uuid", str(uuid.uuid4())), ("expected_revision", 1)):
            changed = deepcopy(plan)
            changed["entries"][0]["input"][key] = value
            with self.subTest(key=key), self.assertRaises(InvalidData):
                validate_plan(changed)
        changed = deepcopy(plan)
        changed["entries"][1]["input"]["uuid"] = changed["entries"][0]["input"]["uuid"]
        with self.assertRaises(InvalidData):
            validate_plan(changed)

    def test_invalid_and_duplicate_targets_stop_before_api_access(self):
        cases = [spec(count=0), spec(count=51)]
        for key, value in (("performer_uuid", str(uuid.uuid4())), ("target_url", "https://user:pass@example.invalid"),
                           ("path_prefix", "C:/outside"), ("namespace", "reddit"), ("label", "bad\nlabel")):
            changed = spec()
            changed["targets"][0][key] = value
            cases.append(changed)
        duplicate = spec()
        duplicate["targets"][1] = deepcopy(duplicate["targets"][0])
        cases.append(duplicate)
        for value in cases:
            with self.assertRaises(InvalidData):
                intent(value)
        self.remote.app.request.assert_not_called()
        self.remote.producer._request.assert_not_called()

    def test_concurrent_management_cannot_enter_same_shared_root(self):
        with management_lock(self.directory, ROOT):
            with self.assertRaises(PublicationBusy):
                with management_lock(self.directory, ROOT):
                    self.fail("Concurrent management acquired the same lock")

    def test_wrong_response_identity_revision_or_history_is_not_success(self):
        _, _, plan = self.prepare(spec(count=1))
        value = plan["entries"][0]["input"]
        self.remote.app.apply(value)
        original = self.remote.app.request.side_effect
        for field, replacement in (("uuid", str(uuid.uuid4())), ("revision", 2), ("reason", "unrelated operation")):
            def response(method, path, data=None):
                result = original(method, path, data)
                if isinstance(result, list) and result:
                    result[0][field] = replacement
                return result
            self.remote.app.request.side_effect = response
            with self.subTest(field=field), self.assertRaises(Unavailable):
                self.remote.app.recover(value)


if __name__ == "__main__":
    unittest.main()
