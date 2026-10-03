import copy
from datetime import datetime, timezone
from pathlib import Path
import sys
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch

from gallery_dl import config, exception, extractor
from gallery_dl.extractor.common import Extractor, Message
from gallery_dl.extractor.reddit import RedditExtractor
from gallery_dl.extractor.twitter import TwitterExtractor

from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.gallery import SUPPORTED_VERSION
from stash_ingest.metadata_bundle import Bundle
from stash_ingest.metadata_fetch import collect, fetch, safe_config, _exchange, is_post
from stash_ingest import source
from stash_ingest.runs import SourcePaused

URL = "https://fixture.invalid/post"
CHILD = "https://fixture.invalid/child"


class Post(Extractor):
    category = "reddit"
    subcategory = "submission"
    pattern = r"https://fixture.invalid/.*"
    messages = ()
    failure = None

    def items(self):
        yield from copy.deepcopy(self.messages)
        if self.failure is not None:
            raise self.failure


class Child(Post):
    category = "redgifs"
    subcategory = "image"


class RedditPost(RedditExtractor):
    subcategory = "submission"
    pattern = Post.pattern

    def submissions(self):
        return [(copy.deepcopy(self.data), ())]


class TwitterPost(TwitterExtractor):
    subcategory = "tweet"
    pattern = r"https://fixture.invalid/(post)"

    def login(self):
        pass

    def metadata(self):
        return {}

    def tweets(self):
        return iter([copy.deepcopy(self.data)])


def factory(cls, url, messages, failure=None):
    result = cls.from_url(url)
    result.messages, result.failure = messages, failure
    return result


def post_data(**changes):
    return {"id": "abc123", "title": "Original caption", "author": "source-account",
            "date": datetime(2026, 10, 2, tzinfo=timezone.utc), **changes}


class FetchTests(unittest.TestCase):
    def tearDown(self):
        config.clear()

    def bundle(self, messages, settings=None):
        result = collect(URL, settings or {}, factory=lambda url: factory(Post, url, messages))
        self.assertNotIn("error", result)
        return Bundle(URL, SUPPORTED_VERSION, result)

    def test_shared_post_fields_and_exact_media_patches(self):
        data = post_data(profile={"name": "A", "bio": "B"}, cookies="private-cookie")
        messages = [(Message.Directory, "", data)] + [
            (Message.Url, "https://media.invalid/" + str(i) + ".jpg", {**data, "num": i}) for i in range(3)]
        before = copy.deepcopy(messages)
        bundle = self.bundle(messages)
        records = bundle.value["records"]
        self.assertEqual(len(records), 4)
        self.assertEqual([r["base"] for r in records], [None, 0, 0, 0])
        self.assertEqual(set(records[1]["patch"]), {"_url", "num"})
        self.assertEqual(encode(bundle.value).count(b"Original caption"), 1)
        self.assertNotIn(b"private-cookie", encode(bundle.value))
        self.assertEqual(bundle.metadata(3)["num"], 2)
        self.assertEqual(bundle.metadata(3)["profile"], {"name": "A", "bio": "B"})
        self.assertEqual(bundle.metadata(0)["date"], "2026-10-02T00:00:00+00:00")
        self.assertEqual(messages, before)

    def test_native_transcript_fixture_reconstructs_and_preserves_observation_times(self):
        path = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
        fixture = decode(path.read_bytes())
        for kind in ("initial", "complete"):
            saved = fixture[kind]
            bundle = Bundle(saved["url"], saved["extractor_version"], saved)
            self.assertEqual(bundle.checkpoint(), saved)
            for i in range(len(saved["records"])):
                self.assertEqual(bundle.metadata(i, with_parent=True), fixture["metadata"][i])
        self.assertEqual(bundle.metadata(0)["large_id"], 9223372036854775815)
        duplicate = copy.deepcopy(fixture["complete"])
        duplicate["records"].append(duplicate["records"][0])
        with self.assertRaisesRegex(InvalidData, "Duplicate"):
            Bundle(duplicate["url"], duplicate["extractor_version"], duplicate)
        for kind in ("pending", "unresolved"):
            duplicate = copy.deepcopy(fixture["initial"])
            duplicate[kind].append(duplicate[kind][0])
            with self.assertRaisesRegex(InvalidData, "Duplicate"):
                Bundle(duplicate["url"], duplicate["extractor_version"], duplicate)

    def test_checkpoint_observation_times_match_native_bounds(self):
        bundle = Bundle(URL, SUPPORTED_VERSION)
        bundle.append("post", {"category": "reddit", "id": "abc123", "source_extractor_url": URL})
        saved = copy.deepcopy(bundle.checkpoint())
        for stamp in ("0001-01-01T00:00:00Z", "0001-01-01T01:00:00+01:00",
                      "0001-01-01T00:00:00+01:00", "9999-12-31T23:30:00-01:00",
                      "2026-10-03T01:00:00+01:60", "2026-10-03T01:00:00", "2026-10-03"):
            with self.subTest(invalid=stamp):
                saved["records"][0]["observed_at"] = stamp
                with self.assertRaisesRegex(InvalidData, "observation time"):
                    Bundle(URL, SUPPORTED_VERSION, saved)
        for stamp in ("2026-10-03T01:00:00.123456789Z", "0001-01-01T00:00:00.000000001Z",
                      "0001-01-01T01:00:00.000000001+01:00"):
            with self.subTest(valid=stamp):
                saved["records"][0]["observed_at"] = stamp
                resumed = Bundle(URL, SUPPORTED_VERSION, saved)
                self.assertEqual(resumed.checkpoint()["records"][0]["observed_at"], stamp)

    def test_compact_expansion_is_bounded_before_appending_or_resuming(self):
        bundle = Bundle(URL, SUPPORTED_VERSION)
        root = {"category": "reddit", "id": "abc123", "source_extractor_url": URL, "caption": "x" * 200}
        bundle.append("post", root)
        before = copy.deepcopy(bundle.checkpoint())
        with patch("stash_ingest.metadata_bundle.MAX_EXPANDED_BYTES", bundle._expanded_bytes + 1):
            with self.assertRaisesRegex(InvalidData, "expansion"):
                bundle.append("media", {**root, "num": 1}, base=0)
        self.assertEqual(bundle.checkpoint(), before)
        bundle.append("media", {**root, "num": 1}, base=0)
        with patch("stash_ingest.metadata_bundle.MAX_EXPANDED_BYTES", bundle._expanded_bytes - 1):
            with self.assertRaisesRegex(InvalidData, "expansion"):
                Bundle(URL, SUPPORTED_VERSION, bundle.checkpoint())
        unicode_root = {**root, "caption": "\u2028" * 10}
        with patch("stash_ingest.metadata_bundle.MAX_PAYLOAD_BYTES", len(encode(unicode_root)) + 1):
            with self.assertRaisesRegex(InvalidData, "native byte"):
                Bundle(URL, SUPPORTED_VERSION).append("post", unicode_root)

    def test_fetch_does_not_construct_writers_or_touch_existing_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            media, cookies = directory / "existing.jpg", directory / "cookies.txt"
            media.write_bytes(b"unchanged image")
            cookies.write_text("# Netscape HTTP Cookie File\n")
            settings = {"extractor": {"directory": str(directory), "filename": "new.jpg",
                "cookies": str(cookies), "cookies-update": str(directory / "exported.txt"),
                "archive": str(directory / "archive.db"), "write-pages": True,
                "file-filter": "False", "actions": {"init": "exec forbidden"},
                "postprocessors": [{"name": "exec", "command": "forbidden"}]},
                "cache": {"file": str(directory / "cache.db")}}
            before = {p.name: p.read_bytes() for p in directory.iterdir()}
            with patch("gallery_dl.job.DownloadJob", side_effect=AssertionError("download job")), \
                    patch("gallery_dl.downloader.find", side_effect=AssertionError("downloader")), \
                    patch("gallery_dl.postprocessor.find", side_effect=AssertionError("postprocessor")), \
                    patch("gallery_dl.archive.DownloadArchive", side_effect=AssertionError("archive")):
                bundle = self.bundle([(Message.Directory, "", post_data()),
                                      (Message.Url, "https://media.invalid/a.jpg", post_data())], settings)
            self.assertEqual(len(bundle.value["records"]), 2, "old filters cannot silently omit evidence")
            self.assertEqual(before, {p.name: p.read_bytes() for p in directory.iterdir()})
            self.assertFalse(config.get(("extractor",), "cookies-update"))
            self.assertEqual(config.get(("cache",), "file"), ":memory:")

    def test_source_config_retains_access_and_pacing_at_every_supported_level(self):
        settings = {"extractor": {"reddit": {"cookies": {"key": "secret"}, "sleep-request": 7,
                    "submission": {"headers": {"Authorization": "secret"}, "comments": 200,
                                   "actions": {"all": "exec forbidden"}}},
                    "kemono": {"original": False, "post": {"original": False}}}}
        before = copy.deepcopy(settings)
        cleaned = safe_config(settings)
        config.clear()
        config._config.update(cleaned)
        reddit = Post.from_url(URL)
        self.assertEqual(reddit.config("cookies"), {"key": "secret"})
        self.assertEqual(reddit.config("sleep-request"), 7)
        self.assertEqual(reddit.config("comments"), 0)
        self.assertEqual(reddit.config("headers"), {"Authorization": "secret"})
        self.assertIsNone(reddit.config("actions"))
        self.assertTrue(config.interpolate(("extractor", "kemono", "post"), "original"))
        self.assertEqual(settings, before)

    def test_child_retry_retains_parent_and_does_not_repeat_parent_request(self):
        parent_messages = [(Message.Directory, "", post_data()), (Message.Queue, CHILD, post_data())]
        child_messages = [(Message.Directory, "", {"id": "child", "caption": "Child caption"}),
                          (Message.Url, "https://media.invalid/video.mp4", {"id": "child"})]
        visited = []

        settings = {"extractor": {"reddit>redgifs": {"cookies": {"special": "child-access"}}}}

        def failed(url):
            visited.append(url)
            return factory(Post, url, parent_messages) if url == URL else factory(
                Child, url, child_messages, exception.AuthenticationError("private response"))

        first = collect(URL, settings, factory=failed)
        self.assertEqual(visited, [URL, CHILD])
        self.assertEqual(first["pending"][0]["reason"], "authentication")
        self.assertEqual([r["kind"] for r in first["records"]], ["post", "context"])
        self.assertNotIn(b"private response", encode(first))
        visited.clear()

        children = []

        def repaired(url):
            visited.append(url)
            item = factory(Child, url, child_messages)
            children.append(item)
            return item

        finished = collect(URL, settings, decode(encode(first)), factory=repaired)
        self.assertEqual(visited, [CHILD])
        self.assertEqual(finished["pending"], [])
        self.assertEqual(finished["records"][:2], first["records"])
        self.assertEqual(children[0].config("cookies"), {"special": "child-access"})
        bundle = Bundle(URL, SUPPORTED_VERSION, finished)
        self.assertEqual(bundle.metadata(3, with_parent=True)["_reddit"]["title"], "Original caption")
        self.assertEqual(collect(URL, {}, finished, factory=lambda _: self.fail("completed checkpoint refetched")), finished)

    def test_busy_child_is_saved_before_initialization_and_only_child_is_retried(self):
        parent = factory(Post, URL, [(Message.Directory, "", post_data()), (Message.Queue, CHILD, post_data())])
        child = factory(Child, CHILD, [(Message.Directory, "", {"id": "child"})])
        initialized = []
        original = child.initialize
        child.initialize = lambda: (initialized.append(CHILD), original())[-1]
        contacts = []

        def reserve(url):
            contacts.append(url)
            return url == URL

        first = collect(URL, {}, factory=lambda url: parent if url == URL else child, reserve_source=reserve)
        self.assertEqual(contacts, [URL, CHILD])
        self.assertEqual(initialized, [])
        self.assertEqual(first["pending"][0]["reason"], "source_busy")
        self.assertEqual([record["kind"] for record in first["records"]], ["post", "context"])
        contacts.clear()
        second = collect(URL, {}, first, factory=lambda url: child, reserve_source=lambda url: contacts.append(url) or True)
        self.assertEqual(contacts, [CHILD])
        self.assertEqual(initialized, [CHILD])
        self.assertEqual(second["pending"], [])
        self.assertEqual(second["records"][:2], first["records"])

    def test_pinned_reddit_gallery_shares_post_and_discards_redundant_previews(self):
        # Normal gallery-dl discovery compiles the Reddit classes used by items().
        self.assertIsNotNone(extractor.find("https://www.reddit.com/comments/abc123"))
        target = RedditPost.from_url(URL)
        target.data = {**post_data(), "created_utc": 1790899200, "url": "https://www.reddit.com/gallery/abc123",
            "gallery_data": {"items": [{"media_id": key} for key in ("abc", "def")]},
            "media_metadata": {key: {"status": "valid", "id": key, "e": "Image", "m": "image/jpg",
                "s": {"u": "https://preview.redd.it/" + key + ".jpg?width=3000", "x": 3000, "y": 4000},
                "p": [{"u": "https://preview.redd.it/" + key + ".jpg?width=108", "x": 108, "y": 144}]}
                for key in ("abc", "def")}}
        with patch("requests.sessions.Session.send", side_effect=AssertionError("unexpected network")):
            result = collect(URL, {}, factory=lambda _: target)
        self.assertNotIn("error", result)
        bundle = Bundle(URL, SUPPORTED_VERSION, result)
        self.assertEqual(len(result["records"]), 3)
        self.assertEqual([source.attachment(bundle.metadata(i))["value"] for i in (1, 2)], ["abc", "def"])
        self.assertNotIn(b"width=108", encode(result))
        self.assertEqual(encode(result).count(b"Original caption"), 1)

    def test_pinned_twitter_transformation_keeps_both_original_media_identifiers(self):
        target = TwitterPost.from_url(URL)
        target.data = {"rest_id": "1973547500000000000", "legacy": {
            "id_str": "1973547500000000000", "lang": "en", "full_text": "Album caption", "entities": {},
            "extended_entities": {"media": [
                {"id_str": "101", "type": "photo", "media_url_https": "https://pbs.twimg.com/media/first.jpg",
                 "original_info": {"width": 100, "height": 200}},
                {"id_str": "102", "type": "video", "media_url_https": "https://pbs.twimg.com/media/second.jpg",
                 "original_info": {"width": 300, "height": 400}, "video_info": {"variants": [
                     {"bitrate": 1000, "url": "https://video.twimg.com/video/full.mp4", "content_type": "video/mp4"}]}}]}},
            "user": {"id_str": "99", "screen_name": "example", "name": "Example", "description": "Bio",
                     "created_at": "Thu Oct 01 00:00:00 +0000 2026", "location": "", "verified": False,
                     "protected": False, "profile_image_url_https": ""}}
        with patch("requests.sessions.Session.send", side_effect=AssertionError("unexpected network")):
            result = collect(URL, {}, factory=lambda _: target)
        self.assertNotIn("error", result)
        bundle = Bundle(URL, SUPPORTED_VERSION, result)
        media = [bundle.metadata(i) for i, item in enumerate(result["records"]) if item["kind"] == "media"]
        self.assertEqual({source.attachment(item)["value"] for item in media}, {"101", "102"})
        self.assertEqual({source.post(item)["value"] for item in media}, {"1973547500000000000"})
        self.assertEqual(encode(result).count(b"Album caption"), 1)

    def test_missing_and_external_children_are_explicit_not_retries(self):
        messages = [(Message.Directory, "", post_data()), (Message.Queue, CHILD, post_data())]
        for child in (None, factory(Post, CHILD, []), factory(Child, CHILD, [], exception.NotFoundError())):
            with self.subTest(child=child):
                result = collect(URL, {}, factory=lambda url: factory(Post, URL, messages) if url == URL else child)
                self.assertEqual(result["pending"], [])
                self.assertEqual(len(result["unresolved"]), 1)
                self.assertNotIn("error", result)

    def test_root_failure_or_empty_result_is_never_reported_complete(self):
        for messages, failure, code in (([], None, "not_found"),
                ([(Message.Directory, "", post_data())], exception.AuthenticationError(), "authentication")):
            with self.subTest(code=code):
                self.assertEqual(collect(URL, {}, factory=lambda url: factory(Post, url, messages, failure)), {"error": code})
        with patch("stash_ingest.metadata_bundle.MAX_RECORDS", 1):
            self.assertEqual(collect(URL, {}, factory=lambda url: factory(Post, url, [
                (Message.Directory, "", post_data()), (Message.Url, CHILD, post_data())])), {"error": "result_too_large"})

    def test_real_extractors_only_admit_single_posts_without_source_requests(self):
        examples = [
            ("https://www.reddit.com/comments/abc123", "reddit"),
            ("https://x.com/Example/status/12345", "twitter"),
            ("https://bsky.app/profile/example.bsky.social/post/3abc", "bluesky"),
            ("https://www.tiktok.com/@example/video/12345", "tiktok"),
            ("https://www.instagram.com/p/ABCxyz/", "instagram"),
            ("https://coomer.st/onlyfans/user/123/post/456", "coomer"),
            ("https://kemono.cr/patreon/user/123/post/456", "kemono"),
            ("https://www.patreon.com/posts/title-12345", "patreon"),
            ("https://fansly.com/post/12345", "fansly"),
        ]
        with patch("requests.sessions.Session.send", side_effect=AssertionError("unexpected network")):
            for url, category in examples:
                with self.subTest(url=url):
                    target = extractor.find(url)
                    self.assertIsNotNone(target)
                    self.assertEqual(target.category, category)
                    self.assertTrue(is_post(target))
            for url in ("https://www.reddit.com/user/example/submitted/", "https://x.com/example",
                        "https://kemono.cr/patreon/user/123", "https://i.redd.it/a.jpg"):
                self.assertEqual(collect(url, {}), {"error": "not_a_post_url"})


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.bundle = Bundle(URL, SUPPORTED_VERSION)
        self.bundle.append("post", {"source_extractor_url": URL, "title": "Caption", "removed": "old"})
        self.bundle.append("media", {"source_extractor_url": URL, "title": "Caption", "n": None}, base=0)

    def test_removal_null_and_exact_duplicate_have_distinct_meaning(self):
        self.assertEqual(self.bundle.value["records"][1]["removed"], ["removed"])
        self.assertIsNone(self.bundle.metadata(1)["n"])
        duplicate = self.bundle.append("media", self.bundle.metadata(1), base=0)
        self.assertEqual(duplicate, 1)
        self.assertEqual(len(self.bundle.value["records"]), 2)

    def test_expanding_parent_context_still_obeys_capture_payload_limit(self):
        bundle = Bundle(URL, SUPPORTED_VERSION)
        parent = bundle.append("post", {"source_extractor_url": URL, "category": "reddit", "title": "x" * 100})
        child = bundle.append("media", {"source_extractor_url": CHILD, "category": "redgifs", "caption": "y" * 100}, parent=parent)
        limit = max(len(encode(bundle.metadata(parent))), len(encode(bundle.metadata(child))))
        with patch("stash_ingest.metadata_bundle.MAX_PAYLOAD_BYTES", limit):
            with self.assertRaises(InvalidData):
                bundle.metadata(child, with_parent=True)

    def test_resume_rejects_wrong_request_secrets_cycles_and_damaged_references(self):
        saved = self.bundle.checkpoint()
        for edit in (
            lambda v: v.update(url=CHILD),
            lambda v: v.update(extractor_version="changed"),
            lambda v: v["records"][0].update(base=1),
            lambda v: v["records"][1].update(parent=1),
            lambda v: v["records"][1].update(observed_at="2026-10-02"),
            lambda v: v["records"][1]["patch"].update(cookies="secret"),
            lambda v: v["records"][1].update(removed=["missing"]),
            lambda v: v.update(pending=[{"url": CHILD, "parent": 0, "depth": 2, "reason": "timeout"}]),
        ):
            value = copy.deepcopy(saved)
            edit(value)
            with self.assertRaises(InvalidData):
                Bundle(URL, SUPPORTED_VERSION, value)


class IsolatedFetchTests(unittest.TestCase):
    def test_real_child_rejects_profile_urls_without_network_or_files(self):
        self.assertEqual(fetch("https://www.reddit.com/user/example/submitted/", {}), {"error": "not_a_post_url"})

    def test_subprocess_limits_timeout_output_and_failure(self):
        self.assertEqual(_exchange([sys.executable, "-c", "import time; time.sleep(20)"], b"{}", 0.1), {"error": "timeout"})
        self.assertEqual(_exchange([sys.executable, "-c", "raise SystemExit(2)"], b"{}", 2), {"error": "worker_failed"})
        with patch("stash_ingest.metadata_fetch.MAX_BYTES", 100):
            self.assertEqual(_exchange([sys.executable, "-c", "print('x'*200)"], b"{}", 2), {"error": "result_too_large"})

    def test_lost_ownership_terminates_the_actual_fetch_process(self):
        processes = []
        launch = subprocess.Popen

        def start(*args, **kwargs):
            process = launch(*args, **kwargs)
            processes.append(process)
            return process

        def check():
            if processes:
                raise SourcePaused("ownership lost")

        with patch("stash_ingest.metadata_fetch.subprocess.Popen", side_effect=start):
            with self.assertRaises(SourcePaused):
                _exchange([sys.executable, "-c", "import time; time.sleep(20)"], b"{}", 5, check)
        self.assertIsNotNone(processes[0].poll())

    def test_cancellation_still_runs_after_child_closes_output(self):
        started = time.monotonic()

        def check():
            if time.monotonic() - started > 0.25:
                raise SourcePaused("ownership lost")

        script = "import os, sys, time; os.close(1); sys.stdin.read(); time.sleep(20)"
        with self.assertRaises(SourcePaused):
            _exchange([sys.executable, "-c", script], b"{}", 5, check)
        self.assertLess(time.monotonic() - started, 3)

    def test_actual_child_protocol_returns_only_retained_metadata(self):
        script = '''
from gallery_dl import extractor, downloader
from gallery_dl.extractor.common import Extractor, Message
from stash_ingest.metadata_fetch import main
import os
class Fixture(Extractor):
    category = "reddit"
    subcategory = "submission"
    pattern = r"https://fixture.invalid/.*"
    def items(self):
        print("private extractor stdout")
        os.write(2, b"private extractor stderr")
        assert self.config("cookies") == {"key":"private-cookie"}
        assert self.config("cookies-update") is False
        try:
            downloader.find("https")
        except RuntimeError:
            pass
        else:
            raise AssertionError("downloader was available")
        yield Message.Directory, "", {"title":"Caption", "cookies":"private-cookie"}
        yield Message.Url, "https://media.invalid/a.jpg", {"title":"Caption"}
extractor.find = Fixture.from_url
main()
'''
        result = _exchange([sys.executable, "-B", "-c", script], encode({"url": URL,
            "settings": {"extractor": {"cookies": {"key": "private-cookie"}}}, "resume": None}), 5)
        self.assertNotIn(b"private", encode(result))
        bundle = Bundle(URL, SUPPORTED_VERSION, result)
        self.assertEqual(len(bundle.value["records"]), 2)
        self.assertEqual(bundle.metadata(1)["_url"], "https://media.invalid/a.jpg")

    def test_actual_paced_child_protocol_denies_and_resumes_linked_service(self):
        script = '''
from gallery_dl import extractor
from gallery_dl.extractor.common import Extractor, Message
from stash_ingest.metadata_fetch import main
class Fixture(Extractor):
    category = "reddit"
    subcategory = "submission"
    pattern = r"https://fixture.invalid/.*"
    def items(self):
        yield Message.Directory, "", {"title":"Caption"}
        yield Message.Queue, "https://fixture.invalid/child", {"title":"Caption"}
class Child(Fixture):
    category = "redgifs"
    subcategory = "image"
    def items(self):
        yield Message.Directory, "", {"id":"child"}
        yield Message.Url, "https://media.invalid/video.mp4", {"id":"child"}
extractor.find = lambda url: (Child if url.endswith("/child") else Fixture).from_url(url)
main()
'''
        contacts = []
        request = {"url": URL, "settings": {}, "resume": None, "source_pacing": True}
        first = _exchange([sys.executable, "-B", "-c", script], encode(request), 5,
                          reserve_source=lambda url: contacts.append(url) or url == URL)
        self.assertEqual(contacts, [URL, CHILD])
        self.assertEqual(first["pending"][0]["reason"], "source_busy")
        self.assertEqual(len(first["records"]), 2)
        contacts.clear()
        request["resume"] = first
        second = _exchange([sys.executable, "-B", "-c", script], encode(request), 5,
                           reserve_source=lambda url: contacts.append(url) or True)
        self.assertEqual(contacts, [CHILD])
        self.assertEqual(second["records"][:2], first["records"])
        self.assertEqual(second["pending"], [])
        self.assertEqual(len(second["records"]), 4)

    def test_paced_exchange_rejects_unframed_or_multiple_results(self):
        for output in ('{}', '{"contact":"file:///private"}', '{"result":{}}\n{"result":{}}'):
            script = "import sys; sys.stdin.buffer.readline(); print(" + repr(output) + ")"
            with self.subTest(output=output), self.assertRaises(InvalidData):
                _exchange([sys.executable, "-B", "-c", script], b"{}", 5, reserve_source=lambda url: True)

    def test_rate_limit_stops_on_first_response_without_leaking_body(self):
        script = '''
from gallery_dl import extractor
from gallery_dl.extractor.common import Extractor
from stash_ingest.metadata_fetch import main
import requests
class Fixture(Extractor):
    category = "reddit"
    subcategory = "submission"
    pattern = r"https://fixture.invalid/.*"
    def items(self):
        self.request("https://fixture.invalid/source")
        return iter(())
def limited(*args, **kwargs):
    response = requests.Response()
    response.status_code = 429
    response._content = b"private server message"
    response._content_consumed = True
    return response
requests.sessions.Session.send = limited
extractor.find = Fixture.from_url
main()
'''
        result = _exchange([sys.executable, "-B", "-c", script], encode({"url": URL,
            "settings": {"extractor": {"sleep-request": 0}}, "resume": None}), 5)
        self.assertEqual(result, {"error": "rate_limited"})


if __name__ == "__main__":
    unittest.main()
