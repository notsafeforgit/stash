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
import uuid

from stash_ingest.album_backfill import Plan, inspect_plan, main, prepare, prepare_retry
from stash_ingest.album_client import AlbumClient, POLICIES, validate_status
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData

STAMP = "2026-10-02T01:00:00Z"


def preview(post):
    attachment, media = str(uuid.uuid4()), str(uuid.uuid4())
    return {"post_uuid": post, "policy": POLICIES[0], "signature": "a" * 64, "action": "create",
            "entries": [{"position": 0, "attachment_uuid": attachment, "media_uuid": media}],
            "add": [{"uuid": media, "kind": "image"}], "remove": [],
            "matches": [{"attachment_uuid": attachment, "status": "matched"}]}


class MemoryAlbumClient(AlbumClient):
    def __init__(self, size=1):
        super().__init__("http://127.0.0.1:9999")
        self.previews = {post: preview(post) for post in sorted(str(uuid.uuid4()) for _ in range(size))}
        self.jobs, self.calls, self.lost = {}, [], set()
        self.discovery_override = None
        self.post_requests = 0

    def request(self, method, path, value=None, **kwargs):
        self.calls.append((method, path, copy.deepcopy(value)))
        if path.startswith("/album-backfill-posts?"):
            if self.discovery_override is not None:
                return 200, self.discovery_override
            after = path.split("after=")[1].split("&")[0]
            return 200, [{"post_uuid": post, "post_state": "active", "selection_uuid": str(uuid.uuid4()), "mode": "pinned"}
                         for post in self.previews if post > after][:100]
        if path.endswith("/album-backfill/preview"):
            post = path.split("/")[2]
            return 200, copy.deepcopy(self.previews[post])
        if path.startswith("/album-backfill-requests/"):
            request = path.rsplit("/", 1)[1]
            if request not in self.jobs:
                raise Unavailable("album_request_rejected", 404)
            return 200, copy.deepcopy(self.jobs[request])
        if path.endswith("/cancel"):
            current = next(job for job in self.jobs.values() if job["job_uuid"] == path.split("/")[2])
            if current["revision"] != value["expected_revision"]:
                raise Unavailable("album_request_rejected", 409)
            current.update(state="cancelled", revision=current["revision"] + 1)
            result = copy.deepcopy(current)
        elif path.endswith("/album-backfills") or path.endswith("/retry"):
            self.post_requests += 1
            parent = None
            if path.endswith("/retry"):
                parent = next(job for job in self.jobs.values() if job["job_uuid"] == path.split("/")[2])
                if parent["state"] not in ("failed", "cancelled") or parent["revision"] != value["expected_revision"]:
                    raise Unavailable("album_request_rejected", 409)
                post = parent["post_uuid"]
            else:
                post = path.split("/")[2]
                if value["signature"] != self.previews[post]["signature"]:
                    raise Unavailable("album_request_rejected", 409)
            request = value["request_uuid"]
            if request not in self.jobs:
                result = {"job_uuid": str(uuid.uuid4()), "sequence": len(self.jobs) + 1, "post_uuid": post,
                          "policy": self.previews[post]["policy"], "signature": self.previews[post]["signature"],
                          "state": "queued", "revision": 1, "attempts": 0, "max_attempts": 10,
                          "created_at": STAMP, "updated_at": STAMP, "available_at": STAMP,
                          "publication_committed": False, "hooks_finished": False}
                if parent:
                    result["resume_from_job_uuid"] = parent["job_uuid"]
                    if parent.get("publication"):
                        result["publication"] = copy.deepcopy(parent["publication"])
                        result["publication_committed"] = True
                self.jobs[request] = result
            result = copy.deepcopy(self.jobs[request])
        else:
            raise AssertionError(path)
        if path in self.lost:
            self.lost.remove(path)
            raise Unavailable("network_unavailable")
        return (202 if result["state"] in ("queued", "running") else 200), result

    def finish(self, request, failed=False):
        job = self.jobs[request]
        job.update(state="failed" if failed else "succeeded", revision=job["revision"] + 2, attempts=1,
                   publication_committed=True, hooks_finished=not failed)
        if "publication" not in job:
            job["publication"] = {"event_uuid": job["job_uuid"], "post_uuid": job["post_uuid"], "gallery_uuid": str(uuid.uuid4()),
                                  "action": "create", "created": True, "selected": 1, "review": 0, "unavailable": 0, "added": 1, "removed": 0}


class AlbumBackfillTests(unittest.TestCase):
    def fixture(self, directory, size=1):
        client = MemoryAlbumClient(size)
        output = Path(directory) / "plan"
        report = prepare(client, output, POLICIES[0])
        return client, Plan(output, report["plan_sha256"], client.endpoint), report

    def test_plan_is_read_only_and_restart_reuses_lost_admission(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, report = self.fixture(directory)
            self.assertFalse(report["submitted"])
            self.assertFalse(client.jobs)
            post = next(iter(plan.items))
            record = plan.record(post)
            before = {p.name: p.read_bytes() for p in plan.directory.iterdir()}
            client.lost.add(f"/posts/{post}/album-backfills")
            with self.assertRaises(Unavailable):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(1, len(client.jobs))
            recovered = Plan(plan.directory, plan.sha256, client.endpoint)
            result = inspect_plan(client, recovered, apply=True)
            self.assertFalse(result["complete"])
            self.assertTrue(result["pending"])
            self.assertEqual(1, client.post_requests)
            client.finish(record["operation"]["request_uuid"])
            # Successful writes have made the old preview stale, but its request
            # remains inspectable/replayable without another POST or preview.
            client.previews[post]["signature"] = "b" * 64
            self.assertTrue(inspect_plan(client, recovered, apply=True)["complete"])
            self.assertEqual(1, client.post_requests)
            self.assertEqual(before, {p.name: p.read_bytes() for p in plan.directory.iterdir()})
            self.assertEqual(0o700, plan.directory.stat().st_mode & 0o777)
            self.assertTrue(all(p.stat().st_mode & 0o077 == 0 for p in plan.directory.iterdir()))

    def test_all_records_checked_before_network_and_changed_endpoint_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory, 2)
            initial_calls = len(client.calls)
            posts = list(plan.items)
            path = plan.directory / (posts[-1] + ".json")
            original = path.read_bytes()
            path.write_bytes(original + b" ")
            with self.assertRaises(InvalidData):
                Plan(plan.directory, plan.sha256, client.endpoint)
            self.assertEqual(initial_calls, len(client.calls))
            with self.assertRaises(InvalidData):
                Plan(plan.directory, plan.sha256, "http://127.0.0.1:9998")
            path.write_bytes(original)
            with self.assertRaises(InvalidData):
                Plan(plan.directory, "0" * 64, client.endpoint)
            with self.assertRaises(InvalidData):
                prepare(client, plan.directory, POLICIES[0])
            self.assertEqual(initial_calls, len(client.calls), "an existing plan is never silently replaced by fresh requests")
            path.unlink()
            outside = Path(directory) / "outside.json"
            outside.write_bytes(original)
            path.symlink_to(outside)
            with self.assertRaises(OSError):
                Plan(plan.directory, plan.sha256, client.endpoint)
            self.assertFalse(client.jobs)

    def test_failed_notifications_retry_preserves_event_and_queued_cancel(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            post = next(iter(plan.items))
            record = plan.record(post)
            inspect_plan(client, plan, apply=True)
            client.finish(record["operation"]["request_uuid"], failed=True)
            original = client.status(record)
            output = Path(directory) / "retry"
            saved = prepare_retry(client, plan, post, output)
            retry = Plan(output, saved["plan_sha256"], client.endpoint)
            self.assertEqual(1, len(client.jobs), "preparing a retry never submits it")
            inspect_plan(client, retry, apply=True)
            pending = client.status(retry.record(post))
            self.assertEqual(original["publication"], pending["publication"])
            client.lost.add(f"/album-backfills/{pending['job_uuid']}/cancel")
            with self.assertRaises(Unavailable):
                client.cancel(retry.record(post), pending["revision"])
            cancelled = client.cancel(retry.record(post), pending["revision"])
            self.assertEqual("cancelled", cancelled["state"])
            final_output = Path(directory) / "retry-again"
            final_saved = prepare_retry(client, retry, post, final_output)
            final = Plan(final_output, final_saved["plan_sha256"], client.endpoint)
            inspect_plan(client, final, apply=True)
            client.finish(final.record(post)["operation"]["request_uuid"])
            self.assertTrue(inspect_plan(client, final)["complete"])
            self.assertEqual(original["publication"], client.status(final.record(post))["publication"])
            self.assertEqual("failed", client.status(record)["state"])

    def test_stale_review_does_not_refresh_or_claim_completion(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            post = next(iter(plan.items))
            client.previews[post]["signature"] = "b" * 64
            report = inspect_plan(client, plan, apply=True)
            self.assertEqual("conflict", report["records"][0]["state"])
            self.assertTrue(report["needs_review"])
            self.assertFalse(report["complete"])
            self.assertFalse(client.jobs)
            self.assertEqual("a" * 64, plan.record(post)["preview"]["signature"])

    def test_discovery_is_paginated_and_rejects_repeated_or_foreign_rows(self):
        client = MemoryAlbumClient(101)
        rows = list(client.selected_posts())
        self.assertEqual(list(client.previews), [row["post_uuid"] for row in rows])
        self.assertEqual(2, len(client.calls))
        for bad in (rows[:2][::-1], [rows[0], rows[0]], [{**rows[0], "mode": "guess"}], [{**rows[0], "post_uuid": "../escape"}]):
            client.discovery_override = bad
            with self.subTest(bad=bad), self.assertRaises(Unavailable):
                list(client.selected_posts())

    def test_filename_discovery_includes_posts_without_source_lists(self):
        client = MemoryAlbumClient()
        post = next(iter(client.previews))
        client.discovery_override = [{"post_uuid": post, "post_state": "active", "selection_uuid": "", "mode": "unselected"}]
        self.assertEqual(1, len(list(client.selected_posts("legacy-twitter-filename-v1"))))
        self.assertIn("policy=legacy-twitter-filename-v1", client.calls[-1][1])
        with self.assertRaises(Unavailable):
            list(client.selected_posts())
        client.previews[post]["policy"] = "legacy-twitter-filename-v1"
        with tempfile.TemporaryDirectory() as directory:
            report = prepare(client, Path(directory) / "plan", "legacy-twitter-filename-v1")
            self.assertEqual(1, report["posts"])
            self.assertFalse(client.jobs, "preparing a recovery does not mutate the library")

    def test_forgotten_and_review_items_are_retained_without_submission(self):
        with tempfile.TemporaryDirectory() as directory:
            client = MemoryAlbumClient(2)
            posts = list(client.previews)
            rows = list(client.selected_posts())
            rows[0]["post_state"] = "forgotten"
            client.discovery_override = rows
            client.previews[posts[1]]["action"] = "review"
            report = prepare(client, Path(directory) / "plan", POLICIES[0])
            plan = Plan(Path(directory) / "plan", report["plan_sha256"])
            result = inspect_plan(client, plan, apply=True)
            self.assertEqual(1, result["counts"]["forgotten"])
            self.assertEqual(1, result["counts"]["review_required"])
            self.assertFalse(result["complete"])
            self.assertFalse(client.jobs)

    def test_status_checks_scope_counts_event_identity_and_completion(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            record = plan.record(next(iter(plan.items)))
            inspect_plan(client, plan, apply=True)
            client.finish(record["operation"]["request_uuid"])
            good = client.status(record)
            for change in ({"post_uuid": str(uuid.uuid4())}, {"policy": POLICIES[1]}, {"signature": "b" * 64},
                           {"state": "queued"}, {"hooks_finished": False}, {"publication_committed": False},
                           {"attempts": True}, {"revision": 0}, {"resume_from_job_uuid": str(uuid.uuid4())},
                           {"available_at": "tomorrow"}, {"error_code": "private secret\n"},
                           {"publication": {**good["publication"], "event_uuid": str(uuid.uuid4())}},
                           {"publication": {**good["publication"], "selected": 0}}):
                with self.subTest(change=change), self.assertRaises(Unavailable):
                    validate_status({**good, **change}, record)

    def test_cli_distinguishes_pending_and_complete(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            base = ["--plan", str(plan.directory), "--expected-sha256", plan.sha256, "--endpoint", client.endpoint]
            with patch("stash_ingest.album_backfill.AlbumClient", return_value=client):
                out = io.StringIO()
                with redirect_stdout(out):
                    self.assertEqual(3, main(["apply", *base]))
                self.assertFalse(json.loads(out.getvalue())["complete"])
                client.finish(plan.record(next(iter(plan.items)))["operation"]["request_uuid"])
                with redirect_stdout(io.StringIO()):
                    self.assertEqual(0, main(["status", *base]))
            # Use the real constructor here: a fixed mock client would discard
            # the supplied endpoint before the plan can compare it.
            with redirect_stderr(io.StringIO()):
                self.assertEqual(1, main(["apply", *base[:-1], "http://elsewhere.test"]))

    def test_noop_and_existing_gallery_results_remain_bound_to_review(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            record = plan.record(next(iter(plan.items)))
            inspect_plan(client, plan, apply=True)
            client.finish(record["operation"]["request_uuid"])
            status = client.status(record)
            for action in ("disabled", "ineligible", "sync"):
                check = copy.deepcopy(record)
                check["preview"].update(action=action, add=[], remove=[], entries=[], matches=[])
                result = copy.deepcopy(status)
                result["publication"].update(action=action, created=False, selected=0, added=0)
                if action != "ineligible":
                    check["preview"]["gallery"] = {"uuid": result["publication"]["gallery_uuid"]}
                else:
                    del result["publication"]["gallery_uuid"]
                self.assertEqual(result, validate_status(result, check))
                if action != "ineligible":
                    result["publication"]["gallery_uuid"] = str(uuid.uuid4())
                    with self.assertRaises(Unavailable):
                        validate_status(result, check)

    def test_transport_uses_application_key_and_rejects_redirects_and_invalid_responses(self):
        calls = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                calls.append((self.path, self.headers.get("ApiKey"), self.headers.get("Authorization")))
                status, headers, body = 200, {"Content-Type": "application/json"}, b"[]"
                if self.path.endswith("redirect"):
                    status, headers, body = 302, {"Location": "/must-not-follow"}, b""
                elif self.path.endswith("denied"):
                    status, body = 403, b"private-error-detail"
                elif self.path.endswith("html"):
                    headers, body = {"Content-Type": "text/html"}, b"[]"
                elif self.path.endswith("compressed"):
                    headers["Content-Encoding"] = "gzip"
                elif self.path.endswith("oversized"):
                    body = b" " * 65537
                elif self.path.endswith("malformed"):
                    body = b'{"duplicate":1,"duplicate":2}'
                self.send_response(status)
                for key, value in headers.items():
                    self.send_header(key, value)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            client = AlbumClient(f"http://127.0.0.1:{server.server_port}", "ALBUM_TEST_KEY")
            with patch.dict("os.environ", {"ALBUM_TEST_KEY": "fixture-application-key", "http_proxy": "http://127.0.0.1:1"}):
                self.assertEqual((200, []), client.request("GET", "/ok"))
                for path in ("redirect", "denied", "html", "compressed", "oversized", "malformed"):
                    with self.subTest(path=path), self.assertRaises(Unavailable) as caught:
                        client.request("GET", "/" + path)
                    self.assertNotIn("private-error-detail", str(caught.exception))
                    self.assertNotIn("fixture-application-key", str(caught.exception))
            with patch.dict("os.environ", {"ALBUM_TEST_KEY": ""}):
                with self.assertRaises(Unavailable):
                    client.request("GET", "/missing-key")
            self.assertEqual(7, len(calls))
            self.assertTrue(all(path.startswith("/api/v3/archive/") and key == "fixture-application-key" and auth is None
                                for path, key, auth in calls))
        finally:
            server.shutdown()
            thread.join()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
