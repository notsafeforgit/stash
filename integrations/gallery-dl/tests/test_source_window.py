from datetime import datetime, timezone
import unittest
from types import SimpleNamespace

from stash_ingest.encoding import InvalidData
from stash_ingest.source import UnsupportedSource
from stash_ingest.source_window import EPOCH, SourceWindow, published


def snowflake(stamp):
    delta = datetime.fromisoformat(stamp) - EPOCH
    milliseconds = (delta.days * 86400 + delta.seconds) * 1000 + delta.microseconds // 1000
    return str((milliseconds - 1_288_834_974_657) << 22)


class SourceWindowTests(unittest.TestCase):
    def test_configured_traversal_keeps_missing_dates_and_reviewed_stop_rules(self):
        scope = SourceWindow({'since': None, 'until': '2026-10-01T00:00:00Z', 'basis': 'traversal'})
        settings = {'init': 'eager', 'date-after': '2020-01-01', 'date-before': '2027-01-01',
                    'date-min': 123, 'date-max': 456, 'skip': 'abort:4', 'image-filter': 'extension == "mp4"'}
        for inherited in (False, True):
            extractor = SimpleNamespace(config=lambda key, default=None: settings.get(key, default))
            scope.configure(extractor, inherited=inherited)
            for key, value in settings.items():
                self.assertEqual(extractor.config(key), 'lazy' if key == 'init' else value)
        for value in (None, datetime(1990, 1, 1, tzinfo=timezone.utc), datetime(2028, 1, 1, tzinfo=timezone.utc)):
            self.assertTrue(scope.contains(value))
        for data in ({}, {'upload_date': '20260901'}, {'timestamp': 123}):
            original = dict(data)
            self.assertIsNone(scope.published(data, 'ytdl'))
            self.assertEqual(data, original, 'request time is never a publication timestamp')

    def test_half_open_boundaries_keep_milliseconds_in_both_twitter_formats(self):
        scope = SourceWindow({"since": "2026-10-01T00:00:00.100Z", "until": "2026-10-01T00:00:00.300Z"})
        for fraction, matches in (("099", False), ("100", True), ("200", True), ("299", True), ("300", False)):
            stamp = "2026-10-01T00:00:00." + fraction + "+00:00"
            key = snowflake(stamp)
            for metadata in ({"rest_id": key, "legacy": {"id_str": key}},
                             {"tweet_id": int(key), "date": datetime(2026, 10, 1)}):
                with self.subTest(fraction=fraction, metadata=metadata):
                    point = published(metadata, "twitter")
                    self.assertEqual(point, datetime.fromisoformat(stamp))
                    self.assertEqual(scope.contains(point), matches)

    def test_reddit_raw_time_and_parent_post_take_precedence_over_derived_dates(self):
        expected = datetime(2026, 10, 1, 0, 0, 0, 100000, timezone.utc)
        data = {"date": "2026-09-01T00:00:00Z", "created_utc": expected.timestamp()}
        self.assertEqual(published(data, "reddit"), expected)
        child = {"date": "2020-01-01T00:00:00Z", "_reddit": {"id": "post", **data}}
        self.assertEqual(published(child, "redgifs"), expected)

    def test_pinned_runtime_naive_dates_are_utc_and_ancient_tweets_use_their_own_date(self):
        expected = datetime(2009, 1, 1, tzinfo=timezone.utc)
        self.assertEqual(published({"id_str": "123", "created_at": "Thu Jan 01 00:00:00 +0000 2009"}, "twitter"), expected)
        self.assertEqual(published({"date": expected.replace(tzinfo=None)}, "reddit"), expected)
        self.assertEqual(published({"date": "2009-01-01T03:00:00+03:00"}, "reddit"), expected)
        self.assertTrue(SourceWindow({"since": None, "until": "2010-01-01T00:00:00Z"}).contains(expected))

    def test_unknown_or_bad_source_time_cannot_be_treated_as_an_empty_success(self):
        cases = [({}, "reddit"), ({"created_utc": float("nan")}, "reddit"),
                 ({"created_utc": True, "date": "2026-10-01T00:00:00Z"}, "reddit"),
                 ({"date": "2026-10-01"}, "reddit"), ({"date": datetime(1, 1, 1)}, "reddit"),
                 ({"rest_id": "9" * 100}, "twitter"), ({"rest_id": "123"}, "twitter"),
                 ({"rest_id": "123", "legacy": {"id_str": "124"}}, "twitter"),
                 ({"created_utc": 1}, "unimplemented")]
        for data, category in cases:
            with self.subTest(data=data, category=category), self.assertRaises(InvalidData):
                published(data, category)
        with self.assertRaises(UnsupportedSource):
            published({"date": "2026-10-01T00:00:00Z"}, "unimplemented")


if __name__ == "__main__":
    unittest.main()
