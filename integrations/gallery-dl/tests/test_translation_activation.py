import copy
from contextlib import redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, HTTPServer
import io
import json
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, digest, encode
from stash_ingest.translation_activation import Plan, inspect_plan, main, prepare
from stash_ingest.translation_activation_client import TranslationActivationClient, validate_candidate, validate_preview, validate_receipt

STAMP = "2026-10-02T01:00:00Z"


class MemoryClient(TranslationActivationClient):
    def __init__(self, count=205):
        super().__init__("http://127.0.0.1:9999")
        self.snapshot, self.manifest_sha256 = str(uuid.uuid4()), "a" * 64
        self.rows, self.receipts, self.calls, self.conflicts = [], {}, [], set()
        self.lost, self.posts, self.override = False, 0, None
        for index in range(count):
            self.rows.append({"ordinal": 400 + index * 3, "target_uuid": str(uuid.uuid4()), "revision": 1,
                              "current_revision": 1, "state": "held", "post_state": "active", "request_uuid": str(uuid.uuid4()),
                              "post_uuid": str(uuid.uuid4()), "field": "caption", "priority": 25, "not_before": STAMP,
                              "disposition": "eligible", "collection_uuid": str(uuid.uuid4()), "collection_revision": 1})
        if count >= 3:
            self.rows[0].update(current_revision=2, disposition="changed")
            self.rows[1].update(current_revision=3, state="completed", disposition="completed")
            self.rows[2].update(post_state="forgotten", disposition="post_forgotten")

    def request(self, method, path, value=None):
        self.calls.append((method, path, copy.deepcopy(value)))
        if "/held-targets?" in path:
            if self.override is not None:
                return copy.deepcopy(self.override)
            query = parse_qs(urlsplit(path).query)
            assert query["expected_manifest_sha256"] == [self.manifest_sha256]
            return copy.deepcopy([row for row in self.rows if row["ordinal"] > int(query["after"][0])][:100])
        if path.endswith("/preview"):
            by_id = {row["target_uuid"]: row for row in self.rows}
            entries = []
            for ref in value["targets"]:
                row = by_id[ref["target_uuid"]]
                entries.append({key: row[key] for key in ("target_uuid", "revision", "request_uuid", "post_uuid",
                                                          "collection_uuid", "collection_revision", "field", "priority", "not_before")})
            result = {"version": 1, "input": value, "entries": entries}
            result["plan_sha256"] = digest(encode(result))
            return copy.deepcopy(result)
        if method == "GET":
            operation = path.rsplit("/", 1)[1]
            if operation not in self.receipts:
                raise Unavailable("translation_activation_rejected", 404)
            return copy.deepcopy(self.receipts[operation])
        self.posts += 1
        operation = value["input"]["uuid"]
        if operation in self.conflicts:
            raise Unavailable("translation_activation_rejected", 409)
        preview = next(call[2] for call in self.calls if call[0] == "POST" and call[1].endswith("/preview") and call[2]["uuid"] == operation)
        result = self.request("POST", "/translation-activations/preview", preview)
        result.update(created_at=STAMP, activated=[{"target_uuid": ref["target_uuid"], "revision": ref["revision"] + 1}
                                                  for ref in preview["targets"]])
        self.receipts[operation] = copy.deepcopy(result)
        if self.lost:
            self.lost = False
            raise Unavailable("network_unavailable")
        return result


class TranslationActivationTests(unittest.TestCase):
    def fixture(self, directory, count=205):
        client = MemoryClient(count)
        output = Path(directory) / "plan"
        report = prepare(client, output, client.snapshot, client.manifest_sha256)
        return client, Plan(output, report["plan_sha256"], client.endpoint), report

    def test_review_is_read_only_and_lost_apply_reuses_immutable_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, prepared = self.fixture(directory)
            self.assertFalse(prepared["activated"])
            self.assertEqual(202, prepared["counts"]["eligible"])
            self.assertEqual(3, prepared["batches"])
            self.assertFalse(client.receipts)
            before = {p.name: p.read_bytes() for p in plan.directory.iterdir()}
            status = inspect_plan(client, plan)
            self.assertTrue(status["pending"])
            client.lost = True
            with self.assertRaises(Unavailable):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(1, len(client.receipts))
            result = inspect_plan(client, Plan(plan.directory, plan.sha256, client.endpoint), apply=True)
            self.assertTrue(result["activation_complete"])
            self.assertEqual({"activated": 202}, result["counts"])
            self.assertEqual(3, client.posts, "lost committed response is inspected before retrying")
            self.assertEqual("not_checked", result["execution_status"])
            for row in client.rows:
                row.update(state="held", current_revision=5, priority=99)
            self.assertEqual(result, inspect_plan(client, plan, apply=True), "later schedules cannot replace the saved operation")
            self.assertEqual(3, client.posts)
            self.assertEqual(before, {p.name: p.read_bytes() for p in plan.directory.iterdir()})
            self.assertEqual(0o700, plan.directory.stat().st_mode & 0o777)
            self.assertTrue(all(p.stat().st_mode & 0o077 == 0 for p in plan.directory.iterdir()))

    def test_every_page_is_validated_before_mutation_and_again_when_read(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            page = plan.directory / "page-000002.json"
            body = page.read_bytes()
            page.write_bytes(body + b" ")
            with self.assertRaises(InvalidData):
                Plan(plan.directory, plan.sha256, client.endpoint)
            self.assertFalse(client.receipts)
            page.write_bytes(body)
            with self.assertRaises(InvalidData):
                Plan(plan.directory, "0" * 64, client.endpoint)
            with self.assertRaises(InvalidData):
                Plan(plan.directory, plan.sha256, "http://127.0.0.1:9998")
            with self.assertRaises(InvalidData):
                prepare(client, plan.directory, client.snapshot, client.manifest_sha256)
            page.unlink()
            outside = Path(directory) / "outside.json"
            outside.write_bytes(body)
            page.symlink_to(outside)
            with self.assertRaises(OSError):
                Plan(plan.directory, plan.sha256, client.endpoint)
            first = plan.directory / "page-000000.json"
            first.write_bytes(first.read_bytes() + b" ")
            with self.assertRaises(InvalidData):
                inspect_plan(client, plan, apply=True)
            self.assertFalse(client.receipts)

    def test_conflicting_batch_stays_reviewable_while_other_batches_resume(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            operation = plan.record(1)["preview"]["input"]["uuid"]
            client.conflicts.add(operation)
            result = inspect_plan(client, plan, apply=True)
            self.assertFalse(result["activation_complete"])
            self.assertTrue(result["needs_review"])
            self.assertEqual({"activated": 102, "conflict": 100}, result["counts"])
            self.assertEqual([{"page": 1, "activation_uuid": operation}], result["conflicts"])
            self.assertEqual(2, len(client.receipts))

    def test_show_reads_only_selected_page_but_apply_requires_complete_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            last = plan.directory / "page-000002.json"
            last.write_bytes(last.read_bytes() + b" ")
            from stash_ingest.activation_plan import read_regular
            reads = []
            def read(path, limit):
                reads.append(Path(path).name)
                return read_regular(path, limit)
            out = io.StringIO()
            with patch("stash_ingest.activation_plan.read_regular", side_effect=read), redirect_stdout(out):
                self.assertEqual(0, main(["show", "--plan", str(plan.directory), "--expected-sha256", plan.sha256, "--page", "0"]))
            self.assertEqual(["manifest.json", "page-000000.json"], reads)
            quick = Plan(plan.directory, plan.sha256, verify_pages=False)
            with self.assertRaises(InvalidData):
                inspect_plan(client, quick, apply=True)
            self.assertFalse(client.receipts)

    def test_empty_and_excluded_plans_do_not_create_operations(self):
        for count in (0, 3):
            with self.subTest(count=count), tempfile.TemporaryDirectory() as directory:
                client, plan, report = self.fixture(directory, count)
                self.assertEqual(0, report["batches"])
                self.assertTrue(inspect_plan(client, plan, apply=True)["activation_complete"])
                self.assertFalse(client.posts)

    def test_invalid_candidates_previews_and_receipts_cannot_claim_success(self):
        client = MemoryClient(1)
        original = client.rows[0]
        for change in ({"ordinal": True}, {"disposition": "completed"}, {"current_revision": 0},
                       {"collection_revision": None}, {"priority": 101}, {"target_uuid": "bad"},
                       {"post_state": "unknown"}, {"field": "urls"}, {"not_before": "no timezone"}):
            with self.subTest(change=change), self.assertRaises(InvalidData):
                validate_candidate({**original, **change}, 0)
        client.override = [original, original]
        with self.assertRaises(Unavailable):
            list(client.candidate_pages(client.snapshot, client.manifest_sha256))
        preview = client.preview(client.snapshot, client.manifest_sha256, client.rows)
        for transform in (
            lambda p: p["input"].update(snapshot_uuid=str(uuid.uuid4())),
            lambda p: p["entries"][0].update(priority=26),
            lambda p: p["input"]["targets"][0].update(revision=True),
            lambda p: p.update(version=True),
        ):
            changed = copy.deepcopy(preview)
            transform(changed)
            with self.assertRaises(InvalidData):
                validate_preview(changed, preview["input"], client.rows)
        receipt = client.apply(preview)
        for transform in (
            lambda r: r.update(created_at="invalid"),
            lambda r: r["activated"][0].update(revision=3),
            lambda r: r.update(input={}),
        ):
            changed = copy.deepcopy(receipt)
            transform(changed)
            with self.assertRaises(InvalidData):
                validate_receipt(changed, preview)

    def test_duplicate_cross_page_target_and_wrong_source_are_rejected_locally(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            record = plan.record(1)
            record["candidates"][0] = copy.deepcopy(plan.record(0)["candidates"][0])
            record["candidates"][0]["ordinal"] = record["after"] + 1
            # Even a newly reviewed, rehashed file must have a consistent preview.
            body = encode(record)
            (plan.directory / "page-000001.json").write_bytes(body)
            manifest = copy.deepcopy(plan.manifest)
            manifest["pages"][1]["sha256"] = digest(body)
            body = encode(manifest)
            (plan.directory / "manifest.json").write_bytes(body)
            with self.assertRaises(InvalidData):
                Plan(plan.directory, digest(body), client.endpoint)
            self.assertFalse(client.receipts)

    def test_cli_show_and_status_preserve_distinct_activation_and_execution_status(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory, 1)
            args = ["--plan", str(plan.directory), "--expected-sha256", plan.sha256]
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(0, main(["show", *args, "--page", "0"]))
            self.assertEqual(plan.record(0), json.loads(out.getvalue()))
            out = io.StringIO()
            with patch("stash_ingest.translation_activation.TranslationActivationClient", return_value=client), redirect_stdout(out):
                self.assertEqual(3, main(["status", *args, "--endpoint", client.endpoint]))
            self.assertEqual("not_checked", json.loads(out.getvalue())["execution_status"])
            out = io.StringIO()
            with redirect_stderr(out):
                self.assertEqual(1, main(["show", *args, "--page", "1"]))
            self.assertFalse(json.loads(out.getvalue())["activation_complete"])

    def test_http_rejects_redirects_compression_and_malformed_json(self):
        state = {"mode": "ok"}
        seen = []
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append((self.headers.get("ApiKey"), self.headers.get("Authorization")))
                mode = state["mode"]
                self.send_response(302 if mode == "redirect" else 200)
                self.send_header("Content-Type", "application/json")
                if mode == "redirect":
                    self.send_header("Location", "/other")
                if mode == "compressed":
                    self.send_header("Content-Encoding", "gzip")
                self.end_headers()
                self.wfile.write(b'{"duplicate":1,"duplicate":2}' if mode == "malformed" else b"[]")
            def log_message(self, *args):
                pass
        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            client = TranslationActivationClient("http://127.0.0.1:" + str(server.server_port))
            with patch.dict("os.environ", {"STASH_API_KEY": "fixture-key"}):
                self.assertEqual([], client.request("GET", "/fixture"))
                for mode in ("redirect", "compressed", "malformed"):
                    state["mode"] = mode
                    with self.assertRaises(Unavailable):
                        client.request("GET", "/fixture")
            self.assertEqual([("fixture-key", None)] * 4, seen)
        finally:
            server.shutdown()
            thread.join()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
