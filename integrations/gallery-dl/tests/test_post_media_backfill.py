import copy
from collections import Counter
from pathlib import Path
import tempfile
import unittest
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData
from stash_ingest.post_media_backfill import Plan, inspect_plan, prepare
from stash_ingest.post_media_client import POLICY, PostMediaClient, validate_result


def preview(post):
    return {"post_uuid": post, "post_revision": 3, "post_state": "active", "policy": POLICY, "signature": "a" * 64,
            "candidates": [{"media_uuid": str(uuid.uuid4()), "media_revision": 4, "media_kind": "scene", "media_state": "active",
                            "association_state": "undecided", "status": "matched", "decisions": [], "proofs": [{
                                **{key: str(uuid.uuid4()) for key in ("evidence_uuid", "post_file_uuid", "match_uuid", "file_uuid", "observation_uuid")},
                                "generation": 1, "relative_path": "original.mp4", "basis": "catalog-file", "status": "valid"}]}]}


def receipt(record):
    proposal = record["preview"]
    counts = Counter(candidate["status"] for candidate in proposal["candidates"])
    decisions = []
    for candidate in proposal["candidates"]:
        if candidate["status"] != "matched":
            continue
        decisions.append({"uuid": str(uuid.uuid5(uuid.UUID(record["request_uuid"]), "post-media-backfill\0" + candidate["media_uuid"])),
                          "post_uuid": record["post_uuid"], "media_uuid": candidate["media_uuid"], "media_revision": candidate["media_revision"],
                          "post_revision": proposal["post_revision"] + len(decisions) + 1, "state": "linked", "origin": "migration",
                          "reason": "Reviewed historical post/file match: " + POLICY, "created_at": "2026-10-06T00:00:00Z"})
    return {"uuid": record["request_uuid"], "post_uuid": record["post_uuid"], "signature": proposal["signature"],
            "selected": counts["matched"], "preserved": counts["preserved"], "review": counts["review"], "unavailable": counts["unavailable"],
            "decisions": decisions}


class MemoryPostClient(PostMediaClient):
    def __init__(self, size=1):
        super().__init__("http://127.0.0.1:9999")
        self.previews = {post: preview(post) for post in sorted(str(uuid.uuid4()) for _ in range(size))}
        self.receipts, self.calls, self.lost, self.limits = {}, [], set(), set()
        self.posts_count = 0
        self.discovery_override = None

    def request(self, method, path, value=None, **kwargs):
        self.calls.append((method, path, copy.deepcopy(value)))
        if path.startswith("/post-media-backfill-posts?"):
            if self.discovery_override is not None:
                return self.discovery_override
            after = path.split("after=")[1]
            return [post for post in self.previews if post > after][:100]
        if path.endswith("/media-backfill-preview"):
            post = path.split("/")[2]
            if post in self.limits:
                raise Unavailable("post_media_request_rejected", 422)
            return copy.deepcopy(self.previews[post])
        if path.startswith("/post-media-backfills/"):
            request = path.rsplit("/", 1)[1]
            if request not in self.receipts:
                raise Unavailable("post_media_request_rejected", 404)
            return copy.deepcopy(self.receipts[request])
        if method != "POST" or not path.endswith("/media-backfills"):
            raise AssertionError(path)
        post = path.split("/")[2]
        self.posts_count += 1
        if value["signature"] != self.previews[post]["signature"]:
            raise Unavailable("post_media_request_rejected", 409)
        record = {"post_uuid": post, "request_uuid": value["uuid"], "preview": self.previews[post]}
        result = receipt(record)
        if value["uuid"] in self.receipts and self.receipts[value["uuid"]] != result:
            raise Unavailable("post_media_request_rejected", 409)
        self.receipts[value["uuid"]] = result
        if post in self.lost:
            self.lost.remove(post)
            raise Unavailable("network_unavailable")
        return copy.deepcopy(result)


class PostMediaBackfillTests(unittest.TestCase):
    def fixture(self, directory, size=1):
        client = MemoryPostClient(size)
        destination = Path(directory) / "plan"
        report = prepare(client, destination)
        return client, Plan(destination, report["plan_sha256"], client.endpoint), report

    def test_read_only_preparation_and_lost_response_resume_original_request(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, report = self.fixture(directory)
            self.assertFalse(report["submitted"])
            self.assertFalse(client.receipts)
            record = next(plan.records())
            saved = {p.name: p.read_bytes() for p in plan.directory.iterdir()}
            client.lost.add(record["post_uuid"])
            with self.assertRaises(Unavailable):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(1, len(client.receipts))
            client.previews[record["post_uuid"]]["signature"] = "b" * 64
            resumed = Plan(plan.directory, plan.sha256, client.endpoint)
            result = inspect_plan(client, resumed, apply=True)
            self.assertTrue(result["processed"])
            self.assertFalse(result["pending"])
            self.assertFalse(result["needs_review"])
            self.assertEqual(1, result["outcomes"]["selected"])
            self.assertEqual(1, client.posts_count)
            self.assertEqual(saved, {p.name: p.read_bytes() for p in plan.directory.iterdir()})
            self.assertEqual(0o700, plan.directory.stat().st_mode & 0o777)
            self.assertTrue(all(p.stat().st_mode & 0o077 == 0 for p in plan.directory.iterdir()))

    def test_pagination_and_bounded_parts_keep_all_posts(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, report = self.fixture(directory, 205)
            self.assertEqual(205, report["posts"])
            self.assertEqual(3, report["parts"])
            self.assertEqual(list(client.previews), [row["post_uuid"] for row in plan.records()])
            self.assertEqual(3, sum("backfill-posts?" in path for _, path, _ in client.calls))
            for post in list(client.previews)[::100]:
                self.assertEqual(post, plan.record(post)["post_uuid"])

    def test_changed_final_part_is_rejected_before_any_network_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory, 101)
            final = plan.directory / plan.manifest["parts"][-1]["name"]
            final.write_bytes(final.read_bytes() + b" ")
            before = len(client.calls)
            with self.assertRaises(InvalidData):
                Plan(plan.directory, plan.sha256, client.endpoint)
            self.assertEqual(before, len(client.calls))
            self.assertEqual(0, client.posts_count)

    def test_open_plan_rechecks_parts_endpoint_and_symlinks(self):
        with tempfile.TemporaryDirectory() as directory:
            client, plan, _ = self.fixture(directory)
            part = plan.directory / plan.manifest["parts"][0]["name"]
            original = part.read_bytes()
            part.write_bytes(original + b" ")
            with self.assertRaises(InvalidData):
                inspect_plan(client, plan, apply=True)
            self.assertEqual(0, client.posts_count)
            part.write_bytes(original)
            with self.assertRaises(InvalidData):
                Plan(plan.directory, plan.sha256, "http://127.0.0.1:9998")
            with self.assertRaises(InvalidData):
                inspect_plan(MemoryPostClientEndpoint(), plan, apply=True)
            calls = len(client.calls)
            with self.assertRaises(InvalidData):
                prepare(client, plan.directory)
            self.assertEqual(calls, len(client.calls))
            other = Path(directory) / "other.json"
            other.write_bytes(original)
            part.unlink()
            part.symlink_to(other)
            with self.assertRaises(OSError):
                Plan(plan.directory, plan.sha256)

    def test_limits_and_stale_posts_remain_visible_without_blocking_other_posts(self):
        with tempfile.TemporaryDirectory() as directory:
            client = MemoryPostClient(3)
            posts = list(client.previews)
            client.limits.add(posts[0])
            report = prepare(client, Path(directory) / "plan")
            plan = Plan(Path(directory) / "plan", report["plan_sha256"], client.endpoint)
            self.assertEqual(1, report["review_limit_posts"])
            client.previews[posts[1]]["signature"] = "b" * 64
            result = inspect_plan(client, plan, apply=True)
            self.assertFalse(result["processed"])
            self.assertTrue(result["needs_review"])
            self.assertEqual({"review_limit": 1, "conflict": 1, "committed": 1}, result["counts"])
            self.assertEqual(1, len(client.receipts))

    def test_wrong_receipt_target_counts_and_signature_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            _, plan, _ = self.fixture(directory)
            record = next(plan.records())
            result = receipt(record)
            self.assertEqual(result, validate_result(result, record))
            for field, value in (("signature", "b" * 64), ("selected", True), ("post_uuid", str(uuid.uuid4()))):
                bad = {**result, field: value}
                with self.assertRaises(Unavailable):
                    validate_result(bad, record)
            bad = copy.deepcopy(result)
            bad["decisions"][0]["media_uuid"] = str(uuid.uuid4())
            with self.assertRaises(Unavailable):
                validate_result(bad, record)

    def test_repeated_discovery_cursor_and_fake_matches_are_rejected(self):
        client = MemoryPostClient()
        post = next(iter(client.previews))
        client.discovery_override = [post, post]
        with self.assertRaises(Unavailable):
            list(client.posts())
        client.previews[post]["candidates"][0]["proofs"] = []
        with self.assertRaises(Unavailable):
            client.preview(post)


class MemoryPostClientEndpoint(MemoryPostClient):
    def __init__(self):
        super().__init__()
        self.endpoint = "http://127.0.0.1:9998"


if __name__ == "__main__":
    unittest.main()
