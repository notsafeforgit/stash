import copy
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.client import Client, Unavailable
from stash_ingest.collections import lookup_collections
from stash_ingest.encoding import InvalidData, decode
from helpers import PRODUCER, ROOT

TARGET = "https://www.reddit.com/user/example/submitted/?sort=new"


def candidate(state="active"):
    return {"collection_uuid": str(uuid.uuid4()), "collection_revision": 3, "state": state}


class CollectionLookupTests(unittest.TestCase):
    def setUp(self):
        self.client = Client("http://fixture.invalid", PRODUCER)
        self.client.capabilities = Mock(return_value={"collection_lookup": True})
        self.response = {"root_uuid": ROOT, "targets": [{"target_url": TARGET, "candidates": [candidate()], "has_more": False}]}
        self.client._request = Mock(side_effect=lambda *args, **kwargs: copy.deepcopy(self.response))

    def test_resolution_preserves_exact_urls_and_does_not_select_ambiguous_or_inactive_collections(self):
        states = ("resolved", "unresolved", "ambiguous", "disabled", "retired")
        values = ([candidate()], [], [candidate(), candidate()], [candidate("disabled")], [candidate("retired")])
        for expected, candidates in zip(states, values, strict=True):
            self.response["targets"][0]["candidates"] = candidates
            result = lookup_collections(self.client, [TARGET], ROOT)
            self.assertEqual(result["targets"][0]["state"], expected)
            self.assertEqual(result["targets"][0]["candidates"], candidates)
        method, route, raw = self.client._request.call_args.args
        self.assertEqual((method, route), ("POST", "/collections/lookup"))
        self.assertEqual(decode(raw), {"root_uuid": ROOT, "targets": [TARGET]})
        self.response["root_uuid"] = None
        self.assertIsNone(lookup_collections(self.client, [TARGET], None)["root_uuid"])

    def test_missing_capability_and_api_outage_are_not_empty_successes(self):
        self.client.capabilities.return_value = {}
        with self.assertRaisesRegex(Unavailable, "incompatible_collection_lookup"):
            lookup_collections(self.client, [TARGET], ROOT)
        self.client._request.assert_not_called()
        self.client.capabilities.side_effect = Unavailable("network_unavailable")
        with self.assertRaisesRegex(Unavailable, "network_unavailable"):
            lookup_collections(self.client, [TARGET], ROOT)

    def test_invalid_source_targets_stop_before_network_access(self):
        for targets in ([], [TARGET] * 51, [TARGET, TARGET], [None], ["file:///private"],
                        ["https://user:password@example.invalid"], [TARGET + "\n"], [TARGET + "\ud800"], [TARGET + "x" * 8192]):
            with self.subTest(targets=repr(targets)[:50]), self.assertRaises(InvalidData):
                lookup_collections(self.client, targets, ROOT)
        self.client.capabilities.assert_not_called()

    def test_wrong_root_target_identity_and_malformed_candidates_are_rejected(self):
        original = copy.deepcopy(self.response)
        bad = []
        changed = copy.deepcopy(original)
        changed["root_uuid"] = None
        bad.append(changed)
        changed = copy.deepcopy(original)
        changed["targets"][0]["target_url"] += "&other=1"
        bad.append(changed)
        for key, value in (("collection_uuid", "bad"), ("collection_revision", True), ("collection_revision", 0), ("state", "unknown")):
            changed = copy.deepcopy(original)
            changed["targets"][0]["candidates"][0][key] = value
            bad.append(changed)
        changed = copy.deepcopy(original)
        changed["targets"][0]["candidates"] *= 2
        bad.append(changed)
        for response in (None, {}, {"root_uuid": ROOT, "targets": []}, *bad):
            self.response = response
            with self.assertRaisesRegex(Unavailable, "invalid_collection_lookup"):
                lookup_collections(self.client, [TARGET], ROOT)

    def test_same_collection_cannot_resolve_two_different_current_targets(self):
        duplicate = copy.deepcopy(self.response["targets"][0])
        duplicate["target_url"] += "&other=1"
        self.response["targets"].append(duplicate)
        with self.assertRaisesRegex(Unavailable, "invalid_collection_lookup"):
            lookup_collections(self.client, [TARGET, duplicate["target_url"]], ROOT)

    def test_root_grants_allow_large_batches_but_truncated_matches_stay_ambiguous(self):
        self.response["targets"][0].update(candidates=[candidate() for _ in range(128)], has_more=True)
        other = {"target_url": TARGET + "&other=1", "candidates": [candidate()], "has_more": False}
        self.response["targets"].append(other)
        result = lookup_collections(self.client, [TARGET, other["target_url"]], ROOT)
        self.assertEqual([item["state"] for item in result["targets"]], ["ambiguous", "resolved"])
        self.assertTrue(result["targets"][0]["has_more"])
        self.assertEqual(self.client._request.call_args.kwargs["max_response_bytes"], 4 << 20)
        self.response["targets"][0]["candidates"] = [candidate()]
        with self.assertRaisesRegex(Unavailable, "invalid_collection_lookup"):
            lookup_collections(self.client, [TARGET, other["target_url"]], ROOT)


if __name__ == "__main__":
    unittest.main()
