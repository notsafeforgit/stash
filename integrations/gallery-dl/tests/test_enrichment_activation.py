import copy
from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import tempfile
import unittest
from urllib.parse import parse_qs, urlsplit
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, digest, encode
from stash_ingest.enrichment_activation import Plan, inspect_plan, main, prepare
from stash_ingest.enrichment_activation_client import (ENTRY_FIELDS, EnrichmentActivationClient, validate_candidate,
                                                      validate_preview, validate_receipt)
from stash_ingest.translation_activation import Plan as TranslationPlan

STAMP = "2026-10-03T01:00:00Z"


class MemoryClient(EnrichmentActivationClient):
    def __init__(self, count=205):
        super().__init__("http://127.0.0.1:9999")
        self.snapshot, self.manifest_sha256 = str(uuid.uuid4()), "a" * 64
        self.rows, self.previews, self.receipts = [], {}, {}
        self.posts, self.lost = 0, False
        for index in range(count):
            target = str(uuid.uuid4())
            new_scope = bool(index % 2)
            self.rows.append({"ordinal": 400 + 3 * index, "target_uuid": target, "revision": 1,
                              "current_revision": 1, "state": "held", "post_state": "active", "post_uuid": str(uuid.uuid4()),
                              "post_revision": 5, "url_uuid": str(uuid.uuid4()), "url": "https://x.com/source/status/123",
                              "collection_uuid": str(uuid.uuid4()), "collection_revision": 1,
                              "current_collection_revision": 2 if new_scope else 1, "collection_state": "active",
                              "activation_collection_revision": 2 if new_scope else 1, "released_target_uuid": str(uuid.uuid4()) if new_scope else target,
                              "released_revision": 1 if new_scope else 2, "policy": "gallery-dl-metadata-v1",
                              "priority": 25, "not_before": STAMP, "disposition": "eligible"})

    def request(self, method, path, value=None):
        assert "enrichment" in path
        if "/held-targets?" in path:
            query = parse_qs(urlsplit(path).query)
            assert query["expected_manifest_sha256"] == [self.manifest_sha256]
            return copy.deepcopy([row for row in self.rows if row["ordinal"] > int(query["after"][0])][:100])
        if path.endswith("/preview"):
            by_target = {row["target_uuid"]: row for row in self.rows}
            result = {"version": 1, "input": value, "entries": [{key: by_target[ref["target_uuid"]][key] for key in ENTRY_FIELDS} for ref in value["targets"]]}
            result["plan_sha256"] = digest(encode(result))
            self.previews[value["uuid"]] = copy.deepcopy(result)
            return result
        if method == "GET":
            operation = path.rsplit("/", 1)[1]
            if operation not in self.receipts:
                raise Unavailable("enrichment_activation_rejected", 404)
            return copy.deepcopy(self.receipts[operation])
        self.posts += 1
        preview = self.previews[value["input"]["uuid"]]
        result = copy.deepcopy(preview)
        result.update(created_at=STAMP, activated=[{"target_uuid": e["released_target_uuid"], "revision": e["released_revision"]} for e in preview["entries"]])
        self.receipts[value["input"]["uuid"]] = copy.deepcopy(result)
        if self.lost:
            self.lost = False
            raise Unavailable("network_unavailable")
        return result


class EnrichmentActivationTests(unittest.TestCase):
    def fixture(self, directory, count=205):
        client = MemoryClient(count)
        prepared = prepare(client, Path(directory) / "plan", client.snapshot, client.manifest_sha256)
        return client, Plan(Path(directory) / "plan", prepared["plan_sha256"], client.endpoint), prepared

    def test_saved_handoffs_recover_lost_response_without_replacing_reviewed_targets(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, prepared = self.fixture(directory)
            self.assertEqual({"eligible": 205}, prepared["counts"])
            self.assertEqual(3, prepared["batches"])
            before = {p.name: p.read_bytes() for p in plan.directory.iterdir()}
            client.lost = True
            with self.assertRaises(Unavailable):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(1, client.posts)
            result = inspect_plan(client, Plan(plan.directory, plan.sha256, client.endpoint), apply=True)
            self.assertTrue(result["activation_complete"])
            self.assertEqual({"activated": 205}, result["counts"])
            self.assertEqual(3, client.posts)
            for row in client.rows:
                row.update(current_revision=4, state="excluded", collection_state="retired")
            self.assertEqual(result, inspect_plan(client, plan, apply=True))
            self.assertEqual(3, client.posts)
            self.assertEqual(before, {p.name: p.read_bytes() for p in plan.directory.iterdir()})
            self.assertEqual("not_checked", result["execution_status"])
            with self.assertRaises(InvalidData):
                TranslationPlan(plan.directory, plan.sha256)

    def test_preview_rejects_changed_url_scope_revision_or_delay(self):
        client = MemoryClient(1)
        preview = client.preview(client.snapshot, client.manifest_sha256, client.rows)
        for key, value in {"post_revision": 6, "url": "https://x.com/other/status/123", "url_uuid": str(uuid.uuid4()),
                           "collection_revision": 2, "activation_collection_revision": 2, "priority": 99,
                           "not_before": "2026-10-03T02:00:00Z", "released_target_uuid": str(uuid.uuid4())}.items():
            with self.subTest(key=key):
                changed = copy.deepcopy(preview)
                changed["entries"][0][key] = value
                with self.assertRaises(InvalidData):
                    validate_preview(changed, preview["input"], client.rows)
        receipt = client.apply(preview)
        receipt["activated"][0]["revision"] += 1
        with self.assertRaises(InvalidData):
            validate_receipt(receipt, preview)

    def test_unavailable_collections_and_changed_targets_require_review(self):
        cases = [{"collection_state": "disabled", "disposition": "collection_disabled"},
                 {"collection_state": "retired", "disposition": "collection_retired"},
                 {"post_state": "forgotten", "disposition": "post_forgotten"},
                 {"current_revision": 2, "state": "excluded", "disposition": "changed"}]
        for changes in cases:
            with self.subTest(changes=changes), tempfile.TemporaryDirectory() as directory:
                client = MemoryClient(1)
                client.rows[0].update(changes)
                report = prepare(client, Path(directory) / "plan", client.snapshot, client.manifest_sha256)
                plan = Plan(Path(directory) / "plan", report["plan_sha256"])
                result = inspect_plan(client, plan, apply=True)
                self.assertTrue(result["needs_review"])
                self.assertFalse(result["activation_complete"])
                self.assertFalse(client.posts)
                row = copy.deepcopy(client.rows[0])
                row["disposition"] = "eligible"
                with self.assertRaises(InvalidData):
                    validate_candidate(row, 0)

    def test_candidate_release_binding_is_checked_before_any_apply(self):
        client = MemoryClient(2)
        for key, value in {"activation_collection_revision": 1, "released_revision": 2,
                           "released_target_uuid": client.rows[1]["target_uuid"], "post_revision": True,
                           "url": "https://name:password@example.invalid/post"}.items():
            with self.subTest(key=key):
                row = copy.deepcopy(client.rows[1])
                row[key] = value
                with self.assertRaises(InvalidData):
                    validate_candidate(row, 0)
        row = copy.deepcopy(client.rows[1])
        row["disposition"] = "replacement_exists"
        self.assertEqual(row, validate_candidate(row, 0))
        with tempfile.TemporaryDirectory() as directory:
            client.rows[1] = copy.deepcopy(client.rows[0])
            client.rows[1]["ordinal"] += 1
            with self.assertRaises(InvalidData):
                prepare(client, Path(directory) / "plan", client.snapshot, client.manifest_sha256)
            self.assertFalse(client.posts)
            self.assertFalse(list(Path(directory).iterdir()))

    def test_largest_urls_fit_saved_pages_and_cli_show(self):
        with tempfile.TemporaryDirectory() as directory:
            client = MemoryClient(100)
            for row in client.rows:
                row["url"] += "?" + "&" * 8100
            result = prepare(client, Path(directory) / "plan", client.snapshot, client.manifest_sha256)
            plan = Plan(Path(directory) / "plan", result["plan_sha256"])
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(0, main(["show", "--plan", str(plan.directory), "--expected-sha256", plan.sha256, "--page", "0"]))
            self.assertEqual(plan.record(0), json.loads(output.getvalue()))
            self.assertEqual({"activated": 100}, inspect_plan(client, plan, apply=True)["counts"])


if __name__ == "__main__":
    unittest.main()
