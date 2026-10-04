import copy
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

from gallery_dl import config
from gallery_dl.extractor.common import Extractor, Message

from stash_ingest.discovery_fetch import MAX_RECORDS, collect, fetch, profile_platform, validate_page
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.gallery import SUPPORTED_VERSION
from stash_ingest.metadata_bundle import Bundle
from stash_ingest.metadata_fetch import RateLimited

TWITTER = "https://x.com/id:123/timeline"
REDDIT = "https://www.reddit.com/user/juniper/submitted/?sort=new"


class Timeline(Extractor):
    category = "twitter"
    subcategory = "timeline"
    pattern = r"https://x\.com/[^/]+/timeline"
    following = "1/next"
    messages = ()
    failure = None

    def _update_cursor(self, value):
        self._cursor = value
        return value

    def items(self):
        self.seen_cursor = self.config("cursor")
        yield from copy.deepcopy(self.messages)
        if self.failure:
            raise self.failure
        self._update_cursor(self.following)


class RedditListing(Extractor):
    category = "reddit"
    subcategory = "user"
    pattern = r"https://www\.reddit\.com/user/[^/]+/submitted/.*"
    following = "t3_next"
    messages = ()

    def _init(self):
        self.calls = 0
        self.api = SimpleNamespace(_call=self.call)

    def call(self, endpoint, params=None):
        self.calls += 1
        if self.calls > 1:
            raise AssertionError("A second listing request was made")
        return {"data": {"after": self.following}}

    def items(self):
        self.api._call("/user/juniper/submitted/.json")
        yield from copy.deepcopy(self.messages)
        if self.following:
            self.api._call("/user/juniper/submitted/.json", {"after": self.following})


def make(cls, url, messages, **attrs):
    target = cls.from_url(url)
    target.messages = messages
    for key, value in attrs.items():
        setattr(target, key, value)
    return target


def reconstruct(page):
    value = Bundle(page["url"], page["extractor_version"]).checkpoint()
    value["records"] = page["records"]
    return Bundle(page["url"], page["extractor_version"], value, max_records=MAX_RECORDS)


class DiscoveryFetchTests(unittest.TestCase):
    def tearDown(self):
        config.clear()

    def test_twitter_resume_keeps_shared_metadata_and_does_not_download_children(self):
        source = {"tweet_id": 9223372036854775815, "title": "A shared original caption",
                  "author": {"name": "juniper", "id": "123"}, "cookies": "private-cookie"}
        messages = [(Message.Directory, "", source),
                    (Message.Url, "https://media.invalid/1.jpg", {**source, "num": 1}),
                    (Message.Url, "https://media.invalid/2.jpg", {**source, "num": 2}),
                    (Message.Queue, "https://imgur.com/linked", source)]
        created = []

        def factory(url):
            created.append(make(Timeline, url, messages))
            return created[-1]

        with patch("gallery_dl.job.Job", side_effect=AssertionError("job constructed")), \
                patch("gallery_dl.downloader.find", side_effect=AssertionError("download")), \
                patch("gallery_dl.postprocessor.find", side_effect=AssertionError("postprocessor")), \
                patch("gallery_dl.archive.DownloadArchive", side_effect=AssertionError("archive")):
            result = collect(TWITTER, {}, {"cursor": "1/saved"}, factory=factory)
        self.assertNotIn("error", result)
        self.assertEqual(len(created), 1)
        self.assertEqual(created[0].seen_cursor, "1/saved")
        self.assertFalse(result["complete"])
        self.assertEqual(result["next_cursor"], {"cursor": "1/next"})
        self.assertEqual(result["cursor"], {"cursor": "1/saved"})
        self.assertEqual(encode(result).count(b"A shared original caption"), 1)
        self.assertNotIn(b"private-cookie", encode(result))
        bundle = reconstruct(result)
        self.assertEqual(bundle.metadata(2)["tweet_id"], 9223372036854775815)
        self.assertEqual(bundle.metadata(3)["discovery_external_reference"], "https://imgur.com/linked")
        self.assertTrue(all(row["observed_at"] for row in result["records"]))

    def test_reddit_resumes_after_cursor_and_stops_before_second_page(self):
        requests, targets = [], []

        def factory(url):
            requests.append(url)
            targets.append(make(RedditListing, url, [(Message.Directory, "", {"id": "abc123", "title": "A post"})]))
            return targets[-1]

        result = collect(REDDIT, {}, {"after": "t3_saved"}, factory=factory)
        self.assertNotIn("error", result)
        self.assertEqual(parse_qs(urlsplit(requests[0]).query), {"sort": ["new"], "after": ["t3_saved"]})
        self.assertEqual(targets[0].calls, 1)
        self.assertFalse(result["complete"])
        self.assertEqual(result["next_cursor"], {"after": "t3_next"})
        self.assertEqual(reconstruct(result).metadata(0)["source_extractor_url"], REDDIT)

    def test_listing_pages_keep_the_historical_record_bound_without_widening_post_fetches(self):
        messages = [(Message.Directory, "", {"tweet_id": str(9000000000000000000 + i)}) for i in range(1100)]
        page = collect(TWITTER, {}, factory=lambda u: make(Timeline, u, messages, following=None))
        self.assertNotIn("error", page)
        self.assertEqual(len(page["records"]), 1100)
        self.assertTrue(page["complete"])
        self.assertEqual(validate_page(page, TWITTER, SUPPORTED_VERSION), page)
        transcript = Bundle(TWITTER, SUPPORTED_VERSION).checkpoint()
        transcript["records"] = page["records"]
        with self.assertRaises(InvalidData):
            Bundle(TWITTER, SUPPORTED_VERSION, transcript)
        with patch("stash_ingest.discovery_fetch.MAX_RECORDS", 2):
            limited = collect(TWITTER, {}, factory=lambda u: make(Timeline, u, messages[:3]))
        self.assertEqual(limited, {"error": "result_too_large"})

    def test_empty_final_pages_are_distinct_from_stalled_pages_and_fetch_errors(self):
        for cls, url, cursor in ((Timeline, TWITTER, {"cursor": "1/saved"}),
                                 (RedditListing, REDDIT, {"after": "t3_saved"})):
            with self.subTest(platform=cls.category):
                final = collect(url, {}, cursor, factory=lambda u: make(cls, u, [], following=None))
                self.assertTrue(final["complete"])
                self.assertIsNone(final["next_cursor"])
                self.assertEqual(final["records"], [])
                stalled = collect(url, {}, cursor, factory=lambda u: make(cls, u, [], following=next(iter(cursor.values()))))
                self.assertEqual(stalled, {"error": "pagination_stalled"})
        failed = collect(TWITTER, {}, factory=lambda u: make(Timeline, u, [], failure=RateLimited()))
        self.assertEqual(failed, {"error": "rate_limited"})

    def test_pages_are_bound_to_the_request_and_cannot_forge_completion_or_retain_seeds(self):
        page = collect(TWITTER, {}, factory=lambda u: make(Timeline, u, [(Message.Directory, "", {"tweet_id": "123"})]))
        self.assertNotIn("error", page)
        for fields in ({"complete": True}, {"complete": 1}, {"next_cursor": None},
                       {"cursor": {"cursor": "different"}}, {"url": "https://x.com/other/timeline"},
                       {"retention_policy": "unknown"}, {"extra": True}):
            with self.subTest(fields=fields), self.assertRaises(InvalidData):
                validate_page({**page, **fields}, TWITTER, SUPPORTED_VERSION)
        forged = copy.deepcopy(page)
        forged["records"][0]["retained_capture"] = "11111111-1111-1111-1111-111111111111"
        with self.assertRaises(InvalidData):
            validate_page(forged, TWITTER, SUPPORTED_VERSION)

    def test_invalid_urls_and_cursors_stop_before_extractor_or_network_access(self):
        for url in ("http://x.com/id:123/timeline", "https://x.com/id:123/timeline?cursor=secret",
                    "https://x.com/id:123/timeline#fragment", "https://www.reddit.com/user/a/submitted/?sort=top",
                    "https://www.reddit.com/r/community/", "https://user:pass@x.com/id:123/timeline",
                    "https://x.com:444/id:123/timeline", "https://other.invalid/id:123/timeline"):
            with self.subTest(url=url), self.assertRaises(InvalidData):
                profile_platform(url)
        with patch("gallery_dl.extractor.find", side_effect=AssertionError("extractor constructed")):
            for cursor in ({"after": "wrong-service"}, {"cursor": ""}, {"cursor": "x\n"},
                           {"cursor": "x" * 8193}, {"cursor": 123}, {"cursor": "\ud800"}):
                self.assertEqual(collect(TWITTER, {}, cursor), {"error": "invalid_checkpoint"})
            self.assertEqual(collect(TWITTER, {}, reserve_source=lambda _: False), {"error": "source_busy"})

    def test_real_isolated_child_reserves_service_before_initialization(self):
        contacted = []
        result = fetch(TWITTER, {"extractor": {"twitter": {"cookies": {"auth_token": "private-fixture"}}}},
                       reserve_source=lambda url: contacted.append(url) or False, timeout=15)
        self.assertEqual(result, {"error": "source_busy"})
        self.assertEqual(contacted, [TWITTER])
        self.assertNotIn(b"private-fixture", encode(result))


if __name__ == "__main__":
    unittest.main()
