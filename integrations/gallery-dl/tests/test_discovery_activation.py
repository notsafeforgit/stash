import copy
from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.discovery_activation import Plan, inspect_plan, main, prepare
from stash_ingest.discovery_activation_client import (DiscoveryActivationClient, binding_input, canonical_input,
                                                      target_identity, validate_preview, validate_receipt)
from stash_ingest.encoding import InvalidData, digest, encode, native_json

STAMP = "2026-10-04T03:04:05Z"


def binding(count=3):
    return {"manifest_sha256": "a" * 64,
            "listing": {"account_uuid": str(uuid.uuid4()), "collection_uuid": str(uuid.uuid4()),
                        "collection_revision": 2, "root_uuid": None,
                        "profile_url": "https://www.reddit.com/user/juniper/submitted/?sort=new",
                        "policy_sha256": "b" * 64, "extractor_version": "1.32.15-dev",
                        "initial_cursor": {"after": "t3_original"}, "historical_pages": 67,
                        "legacy": {"snapshot_uuid": str(uuid.uuid4()), "account_ordinal": 7},
                        "not_before": "2026-10-04T05:00:00.120+02:00"},
            "targets": [{"source_ordinal": i + 10, "source_sha256": "c" * 64} for i in reversed(range(count))]}


class MemoryClient(DiscoveryActivationClient):
    def __init__(self):
        super().__init__("http://127.0.0.1:9999")
        self.calls, self.previews, self.receipts = [], {}, {}
        self.applies, self.lost, self.conflict = 0, False, False

    def request(self, method, path, value=None):
        self.calls.append((method, path, copy.deepcopy(value)))
        if path.endswith("/preview"):
            result = {"version": 1, "input": copy.deepcopy(value),
                      "listing_sha256": digest(native_json(value["listing"], 32768)), "account_sha256": "d" * 64,
                      "entries": [{**ref, "target_uuid": target_identity(value["listing"]["uuid"], ref["source_ordinal"]),
                                   "post_uuid": str(uuid.uuid4()), "post_revision": 5} for ref in value["targets"]],
                      "plan_sha256": "e" * 64}
            self.previews[value["uuid"]] = copy.deepcopy(result)
            return result
        if method == "GET":
            operation = path.rsplit("/", 1)[1]
            if operation not in self.receipts:
                raise Unavailable("discovery_activation_rejected", 404)
            return copy.deepcopy(self.receipts[operation])
        self.applies += 1
        if self.conflict:
            raise Unavailable("discovery_activation_rejected", 409)
        preview = self.previews[value["input"]["uuid"]]
        assert value["expected_plan_sha256"] == preview["plan_sha256"]
        receipt = {**copy.deepcopy(preview), "created_at": STAMP}
        self.receipts[value["input"]["uuid"]] = copy.deepcopy(receipt)
        if self.lost:
            self.lost = False
            raise Unavailable("network_unavailable")
        return receipt


class DiscoveryActivationTests(unittest.TestCase):
    def fixture(self, directory, count=3):
        client = MemoryClient()
        path = Path(directory) / "plan.json"
        prepared = prepare(client, path, binding(count))
        return client, Plan(path, prepared["plan_sha256"], client.endpoint), prepared

    def test_review_is_read_only_and_lost_apply_recovers_exact_saved_binding(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, prepared = self.fixture(directory)
            before = plan.path.read_bytes()
            self.assertEqual(0o600, stat.S_IMODE(plan.path.stat().st_mode))
            self.assertFalse(prepared["activated"])
            self.assertEqual(1, len(client.calls))
            preview = plan.read()["preview"]
            self.assertEqual([10, 11, 12], [ref["source_ordinal"] for ref in preview["input"]["targets"]])
            self.assertEqual("2026-10-04T03:00:00.12Z", preview["input"]["listing"]["not_before"])
            self.assertEqual({"after": "t3_original"}, preview["input"]["listing"]["initial_cursor"])
            self.assertEqual(67, preview["input"]["listing"]["historical_pages"])
            self.assertTrue(inspect_plan(client, plan)["pending"])
            client.lost = True
            with self.assertRaises(Unavailable):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(1, client.applies)
            client.previews.clear()  # Recovery must not regenerate any review.
            recovered = inspect_plan(client, Plan(plan.path, plan.sha256, client.endpoint), apply=True)
            self.assertTrue(recovered["activated"])
            self.assertFalse(recovered["pending"])
            self.assertEqual("not_checked", recovered["execution_status"])
            self.assertEqual(1, client.applies)
            self.assertEqual(before, plan.path.read_bytes())

    def test_input_rejects_ambiguous_or_unbounded_selection_before_network_access(self):
        original = binding()
        copied = copy.deepcopy(original)
        value = binding_input(original)
        self.assertEqual(copied, original)
        mutations = [lambda x: x["targets"].append(x["targets"][0]),
                     lambda x: x.update(targets=[]),
                     lambda x: x["targets"][0].update(source_ordinal=True),
                     lambda x: x["targets"][0].update(source_sha256="x" * 64),
                     lambda x: x["listing"].update(legacy=None),
                     lambda x: x["listing"].update(initial_cursor={"cursor": "wrong-platform"}),
                     lambda x: x["listing"].update(not_before="2026-10-04T03:00:00.0001Z"),
                     lambda x: x["listing"].update(not_before="9999-12-31T23:59:59-01:00"),
                     lambda x: x["listing"].update(collection_revision=True),
                     lambda x: x.update(credentials={"token": "must-stay-local"}),
                     lambda x: x["listing"].update(profile_url="https://user:pass@www.reddit.com/user/juniper/")]
        for index, mutate in enumerate(mutations):
            with self.subTest(case=index):
                bad = copy.deepcopy(value)
                mutate(bad)
                client = MemoryClient()
                with self.assertRaises(InvalidData):
                    client.preview(bad)
                self.assertFalse(client.calls)
        with self.assertRaises(InvalidData):
            binding_input(binding(1001))

    def test_preview_rejects_changed_resume_scope_targets_or_native_post_revision_shape(self):
        client = MemoryClient()
        expected = binding_input(binding())
        preview = client.preview(expected)
        mutations = [lambda x: x["input"]["listing"].update(initial_cursor=None),
                     lambda x: x["input"]["listing"].update(historical_pages=0),
                     lambda x: x["input"]["listing"].update(collection_uuid=str(uuid.uuid4())),
                     lambda x: x["input"].update(manifest_sha256="f" * 64),
                     lambda x: x["entries"].reverse(),
                     lambda x: x["entries"][0].update(target_uuid=str(uuid.uuid4())),
                     lambda x: x["entries"][0].update(source_sha256="f" * 64),
                     lambda x: x["entries"][0].update(post_revision=True),
                     lambda x: x["entries"].pop(),
                     lambda x: x.update(listing_sha256="f" * 64),
                     lambda x: x.update(version=True)]
        for index, mutate in enumerate(mutations):
            with self.subTest(case=index):
                bad = copy.deepcopy(preview)
                mutate(bad)
                with self.assertRaises(InvalidData):
                    validate_preview(bad, expected)
        receipt = client.apply(preview)
        receipt["entries"][0]["post_revision"] += 1
        with self.assertRaises(InvalidData):
            validate_receipt(receipt, preview)
        receipt = client.receipts[expected["uuid"]]
        receipt["input"]["listing"]["policy_sha256"] = "f" * 64
        with self.assertRaises(Unavailable):
            client.status(preview)

    def test_changed_plan_wrong_endpoint_and_symlink_cannot_apply(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            with self.assertRaises(InvalidData):
                Plan(plan.path, plan.sha256, "http://127.0.0.1:9998")
            other = MemoryClient()
            other.endpoint = "http://127.0.0.1:9998"
            with self.assertRaises(InvalidData):
                inspect_plan(other, plan, apply=True)
            self.assertFalse(other.calls)
            changed = plan.read()
            changed["preview"]["input"]["listing"]["initial_cursor"] = None
            plan.path.write_bytes(encode(changed))
            with self.assertRaises(InvalidData):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(0, client.applies)
            with self.assertRaises(InvalidData):
                prepare(client, plan.path, binding())
            link = Path(directory) / "link.json"
            link.symlink_to(plan.path)
            with self.assertRaises(OSError):
                Plan(link, digest(plan.path.read_bytes()))
            with self.assertRaises(InvalidData):
                prepare(client, link, binding())

    def test_cli_conflict_keeps_review_and_show_needs_no_api_key(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            saved = plan.path.read_bytes()
            args = ["--plan", str(plan.path), "--expected-sha256", plan.sha256]
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(0, main(["show", *args]))
            self.assertEqual(plan.read(), json.loads(output.getvalue()))
            client.conflict = True
            errors = io.StringIO()
            with patch("stash_ingest.discovery_activation.DiscoveryActivationClient", return_value=client), redirect_stderr(errors):
                self.assertEqual(2, main(["apply", *args, "--endpoint", client.endpoint]))
            self.assertTrue(json.loads(errors.getvalue())["needs_review"])
            self.assertEqual(saved, plan.path.read_bytes())
            client.conflict = False
            output = io.StringIO()
            with patch("stash_ingest.discovery_activation.DiscoveryActivationClient", return_value=client), redirect_stdout(output):
                self.assertEqual(3, main(["status", *args, "--endpoint", client.endpoint]))
            self.assertTrue(json.loads(output.getvalue())["pending"])
            self.assertEqual(1, client.applies)

    def test_maximum_batch_and_exact_deadline_canonicalization(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, prepared = self.fixture(directory, 1000)
            self.assertEqual(1000, prepared["targets"])
            self.assertTrue(inspect_plan(client, plan, apply=True)["activated"])
        original = binding_input(binding(1))
        for stamp, expected in [("2026-10-04T03:00:00.000Z", "2026-10-04T03:00:00Z"),
                                ("2026-10-04T02:00:00.001-01:00", "2026-10-04T03:00:00.001Z")]:
            original["listing"]["not_before"] = stamp
            self.assertEqual(expected, canonical_input(original)["listing"]["not_before"])


if __name__ == "__main__":
    unittest.main()
