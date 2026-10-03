import copy
import unittest

from gallery_dl import config, exception
from gallery_dl.extractor.common import Message

from stash_ingest.encoding import InvalidData, encode
from stash_ingest.gallery import SUPPORTED_VERSION
from stash_ingest.metadata_bundle import Bundle, RETAINED_SCHEMA
from stash_ingest.metadata_fetch import collect
from stash_ingest.retention import POLICY
from test_metadata_fetch import Child, factory


URL = "https://www.reddit.com/comments/saved"
CHILD = "https://fixture.invalid/child"


def seed():
    return {"schema": RETAINED_SCHEMA, "url": URL, "retention_policy": POLICY,
            "extractor_version": SUPPORTED_VERSION, "records": [
                {"kind": "context", "base": None, "parent": None, "removed": [], "observed_at": None,
                 "retained_capture": "61637224-d6b2-4981-95e8-27cfd21d3000",
                 "patch": {"category": "reddit", "id": "saved", "title": "Old caption",
                           "author": {"id": 9007199254740993},
                           "media_metadata": {"one": {"p": ["old preview"]}}}}],
            "pending": [{"url": CHILD, "parent": 0, "depth": 1, "reason": "legacy_pending"}], "unresolved": []}


class RetainedFetchTests(unittest.TestCase):
    def tearDown(self):
        config.clear()

    def test_retry_fetches_only_saved_child_and_keeps_original_context(self):
        saved = seed()
        calls, reserved = [], []

        def find(url):
            calls.append(url)
            self.assertEqual(url, CHILD, "root must not be fetched again")
            target = factory(Child, url, [(Message.Url, "https://media.invalid/full.jpg",
                                           {"id": "child", "cookies": "private-value"})])
            initialize = target.initialize

            def init():
                self.assertEqual(target.config("user-agent"), "inherited-reddit-agent")
                return initialize()

            target.initialize = init
            return target

        settings = {"extractor": {"sleep-request": 0, "reddit>redgifs": {"user-agent": "inherited-reddit-agent"}}}
        result = collect(URL, settings, saved, factory=find, reserve_source=lambda url: reserved.append(url) or True)
        self.assertNotIn("error", result)
        self.assertEqual(calls, [CHILD])
        self.assertEqual(reserved, [CHILD])
        self.assertEqual(result["records"][:1], saved["records"])
        self.assertEqual(result["pending"], [])
        bundle = Bundle(URL, SUPPORTED_VERSION, result)
        self.assertEqual(len(bundle.value["records"]), 2)
        child = bundle.metadata(1, with_parent=True)
        self.assertEqual(encode(child["_reddit"]), encode(saved["records"][0]["patch"]))
        self.assertNotIn("cookies", child)
        self.assertIsNotNone(result["records"][1]["observed_at"])
        self.assertNotIn("retained_capture", result["records"][1])

    def test_failure_keeps_context_and_replaces_the_historical_pending_reason(self):
        for exc, location, reason in ((exception.NotFoundError(), "unresolved", "not_found"),
                                      (RuntimeError(), "pending", "extraction_failed")):
            with self.subTest(reason=reason):
                saved = seed()
                result = collect(URL, {}, saved, factory=lambda url: factory(Child, url, [], exc))
                self.assertNotIn("error", result)
                self.assertEqual(result["records"], saved["records"])
                self.assertEqual(result[location][0]["reason"], reason)
                self.assertIsNone(result["records"][0]["observed_at"])
                Bundle(URL, SUPPORTED_VERSION, result)

    def test_existing_parent_shortcut_and_nested_context_are_preserved(self):
        saved = seed()
        root = saved["records"][0]["patch"]
        parent = {"category": "imgur", "id": "oldchild", "_parent": root, "_reddit": root}
        saved["records"][0]["patch"] = parent
        saved["pending"][0]["depth"] = 2
        bundle = Bundle(URL, SUPPORTED_VERSION, saved)
        self.assertEqual(bundle.depth(0), 1)
        self.assertEqual(bundle.ancestors(0), ["imgur", "reddit"])
        result = collect(URL, {}, saved, factory=lambda url: factory(Child, url, [
            (Message.Url, "https://media.invalid/full.jpg", {"id": "child"})]))
        final = Bundle(URL, SUPPORTED_VERSION, result)
        self.assertEqual(final.depth(1), 2)
        self.assertEqual(final.metadata(1, with_parent=True)["_parent"], parent)

    def test_invalid_retained_prefix_is_rejected_before_network(self):
        for change in (
            lambda v: v.update(schema=[]),
            lambda v: v.update(schema="stash-metadata-fetch-v1"),
            lambda v: v["records"][0].update(observed_at="2026-10-03T12:00:00Z"),
            lambda v: v["records"][0].update(retained_capture=None),
            lambda v: v["records"][0].update(kind="media"),
            lambda v: v["records"].append(copy.deepcopy(v["records"][0])),
            lambda v: v["pending"][0].update(depth=2),
            lambda v: v.update(unresolved=v.pop("pending"), pending=[]),
            lambda v: v["records"][0]["patch"].update(_parent={"category": "imgur"}, _reddit={"category": "reddit"}),
        ):
            saved = seed()
            change(saved)
            with self.assertRaises(InvalidData):
                Bundle(URL, SUPPORTED_VERSION, saved)
            self.assertEqual(collect(URL, {}, saved, factory=lambda url: self.fail("invalid context fetched")),
                             {"error": "invalid_checkpoint"})

    def test_new_records_cannot_invent_root_or_copy_unobserved_fields(self):
        for kwargs in ({}, {"parent": 0, "base": 0}):
            bundle = Bundle(URL, SUPPORTED_VERSION, seed())
            with self.assertRaises(InvalidData):
                bundle.append("media", {"category": "redgifs", "source_extractor_url": CHILD}, **kwargs)

        bundle = Bundle(URL, SUPPORTED_VERSION, seed())
        with self.assertRaises(InvalidData):
            bundle.append("media", {"category": "redgifs", "source_extractor_url": CHILD,
                                    "_parent": {"category": "reddit", "id": "unreviewed"}}, parent=0)


if __name__ == "__main__":
    unittest.main()
