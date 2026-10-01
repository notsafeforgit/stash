"""Real gallery-dl scheduling and postprocessing with local fixture downloads."""

import copy
from datetime import datetime
import hashlib
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
from requests.exceptions import ConnectionError

from gallery_dl import config
from gallery_dl import extractor as gdl_extractors
from gallery_dl.extractor.common import Extractor, Message
from gallery_dl.extractor.twitter import TwitterExtractor
from gallery_dl.extractor.reddit import RedditExtractor

from stash_ingest.encoding import InvalidData, decode
from stash_ingest.filesystem import Root
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.producer import Producer
from stash_ingest.runs import SourcePaused
from stash_ingest.source_window import SourceWindow
from stash_ingest.scan_resume import legacy_cursor, PREFIX as LEGACY_CURSOR_PREFIX
from test_producer import LeaseFixture, reddit_data
from test_source_window import snowflake
from helpers import PRODUCER


class Fixture(Extractor):
    category = "reddit"
    subcategory = "user"
    pattern = r"https://fixture.invalid/.*"
    archive_fmt = "{id}_{filename}"

    def items(self):
        for item in self.records:
            self.visited.append(item["id"])
            data = copy.deepcopy(item)
            yield Message.Directory, "", data
            yield Message.Url, data["_url"], data


class TwitterFixture(TwitterExtractor):
    subcategory = "user"
    pattern = r"https://fixture.invalid/(account)"

    def login(self):
        pass

    def metadata(self):
        return {}

    def tweets(self):
        return iter(copy.deepcopy(self.records))


class RedditPaginationFixture(RedditExtractor):
    subcategory = "user"
    pattern = Fixture.pattern

    def submissions(self):
        def call(endpoint, params):
            self.visited.extend(item["id"] for item in self.records)
            return {"data": {"children": [{"kind": "t3", "data": copy.deepcopy(item)}
                                          for item in self.records], "after": None}}
        self.api._call = call
        return self.api._pagination("/fixture", {})


class ChildFixture(Fixture):
    category = "redgifs"

    def items(self):
        # A linked child's upload date does not determine which Reddit source
        # window owns its content. This fixture only exercises traversal here;
        # native external-host attachment association remains separate work.
        data = reddit_data(date="2020-01-01T00:00:00Z")
        yield Message.Directory, "", data
        yield Message.Url, data["_url"], data


class ParentFixture(Fixture):
    def items(self):
        for item in self.records:
            data = copy.deepcopy(item)
            yield Message.Directory, "", data
            yield Message.Queue, "https://fixture.invalid/child", {**data, "_extractor": ChildFixture}


class GalleryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.media = self.directory / "media"
        self.media.mkdir()
        self.locks = self.directory / "locks"
        self.locks.mkdir()
        self.lease = LeaseFixture()
        self.lease.run["path_prefix"] = "Account"
        self.root = Root(self.media, Root.probe(self.media))
        self.box = Outbox(self.directory / "outbox.sqlite", self.lease.client.endpoint, PRODUCER)
        self.addCleanup(self.box.close)
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="1.32.15-dev")
        config.clear()
        self.addCleanup(config.clear)
        for key, value in {"base-directory": str(self.media), "directory": ["Account"],
                           "filename": "{id}_{filename}.{extension}",
                           "archive": str(self.directory / "downloads.sqlite"),
                           "sleep": 0, "sleep-request": 0, "sleep-extractor": 0}.items():
            config.set(("extractor",), key, value)
        config.set(("output",), "mode", "null")
        network = patch("requests.sessions.Session.request", side_effect=AssertionError("No website requests"))
        network.start()
        self.addCleanup(network.stop)

    def task(self, *records, fixture=Fixture):
        extractor = fixture.from_url(self.lease.run["target_url"])
        extractor.records = records or (reddit_data(),)
        extractor.visited = []
        task = NativeDownloadJob(extractor, producer=self.producer, lock_directory=self.locks)

        def download(url):
            # The source is durable before the first byte is written.
            self.assertEqual(self.events()[-1]["kind"], "source.capture")
            pf = task.pathfmt
            pf.part_enable()
            with pf.open("wb") as output:
                output.write(b"fixture media " + url.encode())
            return True

        task.download = download
        return task

    def events(self):
        return [decode(row[0]) for row in self.box.db.execute("SELECT body FROM events ORDER BY seq")]

    def archive_count(self):
        with sqlite3.connect(self.directory / "downloads.sqlite") as db:
            return db.execute("SELECT count(*) FROM archive").fetchone()[0]

    def narrow_window(self):
        self.lease.run["window"] = {"since": "2026-10-01T00:00:00.100Z", "until": "2026-10-01T00:00:00.300Z"}
        self.producer.window = SourceWindow(self.lease.run["window"])

    def test_claimed_window_overrides_inherited_dates_without_stopping_at_old_posts(self):
        self.narrow_window()
        # Normal CLI discovery compiles the Reddit classes used by items().
        self.assertIsNotNone(gdl_extractors.find("https://www.reddit.com/user/fixture/submitted/"))
        config.set(("extractor",), "archive-format", "{id}_{filename}")
        for key, value in {"date-min": "2028-01-01T00:00:00", "date-max": "2020-01-01T00:00:00",
                           "date-after": "2028-01-01", "date-before": "2020-01-01",
                           "post-filter": 'id != "postblocked"'}.items():
            config.set(("extractor", "reddit", "user"), key, value)
        stamps = (("newer", "2026-10-01T00:00:00.400Z"), ("until", "2026-10-01T00:00:00.300Z"),
                  ("since", "2026-10-01T00:00:00.100Z"), ("pinned", "2020-01-01T00:00:00Z"),
                  ("inside", "2026-10-01T00:00:00.200Z"), ("blocked", "2026-10-01T00:00:00.200Z"))
        records = [reddit_data(key, date=stamp, created_utc=datetime.fromisoformat(stamp).timestamp(),
                               num_comments=0, is_video=False, is_self=False, selftext_html=None) for key, stamp in stamps]
        for fixture in (Fixture, RedditPaginationFixture):
            with self.subTest(fixture=fixture.__name__):
                task = self.task(*records, fixture=fixture)
                lower, upper = task.extractor._get_date_min_max(0, 253402210800)
                self.assertEqual(lower, datetime.fromisoformat(self.lease.run["window"]["since"]).timestamp())
                self.assertEqual(upper, datetime.fromisoformat(self.lease.run["window"]["until"]).timestamp())
                before = len(self.events())
                self.assertEqual(task.run(), 0)
                captured = [e["post"]["value"] for e in self.events()[before:] if e["kind"] == "source.capture"]
                self.assertEqual(captured, ["postsince", "postinside"])
                self.assertEqual(task.extractor.visited, [r["id"] for r in records])
        self.assertEqual(self.archive_count(), 2)
        self.assertEqual(config.get(("extractor", "reddit", "user"), "date-max"), "2020-01-01T00:00:00")

    def test_invalid_source_date_stops_before_files_or_capture_are_claimed(self):
        task = self.task(reddit_data(date="unknown"))
        task.download = lambda _: self.fail("Undated source was downloaded")
        self.assertNotEqual(task.run(), 0)
        self.assertEqual(self.events(), [])
        self.assertFalse((self.media / "Account").exists())

    def test_all_outside_the_window_has_no_file_or_postprocessor_effects(self):
        self.narrow_window()
        config.set(("extractor",), "keywords", False)
        config.set(("extractor",), "init", True)
        config.set(("extractor",), "postprocessors", [
            {"name": "exec", "event": event, "command": ["fixture"]} for event in ("init", "post")])
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=AssertionError("Outside-window processor")):
            task = self.task(reddit_data(date="2020-01-01T00:00:00Z"))
            self.assertEqual(task.run(), 0)
        self.assertEqual(self.events(), [])
        self.assertFalse((self.media / "Account").exists())

    def test_wrong_initial_or_later_directory_cannot_run_postprocessor_callbacks(self):
        config.set(("extractor",), "directory", ["{destination}"])
        config.set(("extractor",), "postprocessors", [
            {"name": "exec", "event": event, "command": ["fixture", event]} for event in ("init", "post")])
        calls = []
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=lambda args, shell: calls.append(args) or 0):
            task = self.task(reddit_data(destination="AccountOther"))
            self.assertNotEqual(task.run(), 0)
            self.assertEqual(calls, [])
            task = self.task(reddit_data(destination="Account"), reddit_data("second", destination="AccountOther"))
            self.assertNotEqual(task.run(), 0)
        self.assertEqual([call[-1] for call in calls], ["init", "post"])
        self.assertEqual(len(self.events()), 2)
        self.assertFalse((self.media / "AccountOther").exists())

    def test_source_date_keywords_cannot_override_the_window_before_extraction(self):
        config.set(("extractor",), "keywords-global", {"date": "2026-10-01T00:00:00.200Z"})
        task = self.task(reddit_data(date="2020-01-01T00:00:00Z"))
        with self.assertRaises(InvalidData):
            task.run()
        self.assertEqual(task.extractor.visited, [])
        self.assertEqual(self.events(), [])

    def test_child_traversal_uses_parent_post_window_and_ignores_child_date_limits(self):
        self.narrow_window()
        config.set(("extractor", "reddit>redgifs"), "date-min", "2028-01-01T00:00:00")
        config.set(("extractor", "reddit>redgifs"), "date-max", "2020-01-01T00:00:00")
        config.set(("extractor", "reddit>redgifs"), "date-before", "2010-01-01")
        task = self.task(reddit_data(date="2026-10-01T00:00:00.200Z"), fixture=ParentFixture)
        seen = []
        original = NativeDownloadJob.handle_url

        def inspect(child, url, data):
            if child._native_parent is None:
                return original(child, url, data)
            seen.append((child.extractor.config("date-min", 0), child.extractor.config("date-max", 999)))

        with patch.object(NativeDownloadJob, "handle_url", inspect):
            self.assertEqual(task.run(), 0)
        self.assertEqual(seen, [(0, 999)])
        with self.assertRaises(InvalidData):
            NativeDownloadJob(ChildFixture.from_url("https://fixture.invalid/child"), task)

    def test_download_and_existing_file_skip_keep_exact_capture_dependency(self):
        first = self.task()
        self.assertEqual(first.run(), 0)
        events = self.events()
        self.assertEqual([e["kind"] for e in events], ["source.capture", "file.completed"])
        media = self.media / events[1]["relative_path"]
        self.assertEqual(events[1]["sha256"], hashlib.sha256(media.read_bytes()).hexdigest())
        self.assertFalse(media.with_suffix(".jpg.part").exists())
        self.assertEqual(self.archive_count(), 1)
        second = self.task(reddit_data(title="Edited caption"))
        second.download = lambda _: self.fail("Existing bytes were downloaded again")
        self.assertEqual(second.run(), 0)
        events = self.events()
        self.assertEqual(events[2]["source"]["title"], "Edited caption")
        self.assertEqual(events[3]["source"]["capture_event_uuid"], events[2]["event_uuid"])
        self.assertEqual(events[3]["relative_path"], events[1]["relative_path"])

    def test_long_unicode_title_preserves_source_id_and_fits_actual_paths(self):
        config.set(("extractor",), "filename", "{title}_{id}_{filename}.{extension}")
        title = "漢字🙂é" * 200
        task = self.task(reddit_data(title=title))
        self.assertEqual(task.run(), 0)
        capture, completed = self.events()
        self.assertEqual(capture["source"]["title"], title)
        self.assertTrue(completed["relative_path"].endswith("_postabc123_abc123.jpg"))
        self.assertLessEqual(len(Path(completed["relative_path"]).name.encode()), 255)

    def test_archive_skip_without_a_resolved_file_queues_source_only(self):
        task = self.task()
        self.assertEqual(task.run(), 0)
        (self.media / self.events()[-1]["relative_path"]).unlink()
        task = self.task(reddit_data(extension=""))
        task.download = lambda _: self.fail("Archive skip attempted a download")
        self.assertEqual(task.run(), 0)
        self.assertEqual([e["kind"] for e in self.events()], ["source.capture", "file.completed", "source.capture"])

    def test_postprocessor_failure_does_not_ack_archive_and_retry_repairs_existing_file(self):
        config.set(("extractor",), "postprocessors", [{"name": "exec", "event": "after", "command": ["fixture", "{_path}"]}])
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", return_value=1):
            task = self.task()
            self.assertNotEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 0)
        self.assertEqual(len(self.events()), 1)
        repaired = []
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=lambda *args: repaired.append(args) or 0):
            task = self.task()
            task.download = lambda _: self.fail("Retry should repair existing output")
            self.assertEqual(task.run(), 0)
        self.assertEqual(len(repaired), 1)
        self.assertEqual(self.archive_count(), 1)
        self.assertEqual(self.events()[-1]["kind"], "file.completed")

    def test_outbox_exhaustion_preserves_file_without_archive_ack(self):
        self.box.max_events = 1
        task = self.task()
        self.assertNotEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 0)
        self.assertTrue((self.media / "Account/postabc123_abc123.jpg").is_file())
        self.box.max_events = 10
        task = self.task()
        task.download = lambda _: self.fail("Existing file must be recovered")
        self.assertEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 1)

    def test_lost_lease_finishes_current_file_and_stops_before_next_source_item(self):
        config.set(("extractor",), "async", True)
        task = self.task(reddit_data(), reddit_data("second"))
        original = task.download

        def download(url):
            result = original(url)
            self.lease.active = False
            return result

        task.download = download
        self.assertNotEqual(task.run(), 0)
        self.assertEqual(task.extractor.visited, ["postabc123"])
        self.assertEqual([e["kind"] for e in self.events()], ["source.capture", "file.completed"])
        self.assertEqual(self.lease.checkpoints, [])
        with self.assertRaises(SourcePaused):
            task.extractor.request("https://fixture.invalid/next")

    def test_source_http_retry_checks_lease_again_inside_the_request_loop(self):
        config.set(("extractor",), "sleep-retries", "0")
        task = self.task()
        task._init()

        def failed_attempt(*args, **kwargs):
            self.lease.active = False
            raise ConnectionError("Fixture connection failure")

        with patch.object(task.extractor.session, "request", side_effect=failed_attempt) as request:
            with self.assertRaises(SourcePaused):
                task.extractor.request("https://fixture.invalid/page", retries=3, interval=False)
            self.assertEqual(request.call_count, 1)

    def test_claimed_target_and_prefix_are_checked_before_download(self):
        extractor = Fixture.from_url("https://fixture.invalid/other")
        with self.assertRaises(InvalidData):
            NativeDownloadJob(extractor, producer=self.producer, lock_directory=self.locks)
        config.set(("extractor",), "directory", ["AccountOther"])
        task = self.task()
        task.download = lambda _: self.fail("Wrong destination was opened")
        self.assertNotEqual(task.run(), 0)
        self.assertFalse((self.media / "AccountOther").exists())
        self.assertEqual(self.events(), [])

    def test_async_and_legacy_postprocessors_are_rejected_before_execution(self):
        for options in ({"name": "exec", "async": True, "command": ["fixture"]},
                        {"name": "python", "function": "/fixture/gallery_catalog_hook.py:prepare"},
                        {"name": "exec"}, {"name": "missing-fixture-processor"}):
            with self.subTest(options=options):
                config.set(("extractor",), "postprocessors", [options])
                task = self.task()
                task.download = lambda _: self.fail("Invalid postprocessing was used")
                self.assertNotEqual(task.run(), 0)
                self.assertEqual(self.events(), [])

    def test_named_async_processor_is_rejected_before_initialization(self):
        config.set(("postprocessor",), "fixture", {"name": "exec", "async": True, "command": ["fixture"]})
        config.set(("extractor",), "postprocessors", [{"type": "fixture"}])
        self.assertNotEqual(self.task().run(), 0)
        self.assertEqual(self.events(), [])

    def test_gif_conversion_reports_the_final_mkv_path_and_archive_skip_recovers_it(self):
        config.set(("extractor",), "postprocessors", [{"name": "exec", "event": "after",
                    "command": ["python3", "/fixture/gif_to_av1_qsv.py", "{_path}"]}])

        def convert(args, shell):
            original = Path(args[-1])
            original.rename(original.with_suffix(".mkv"))
            return 0

        data = reddit_data(extension="gif", url="https://i.redd.it/abc123.gif", _url="https://i.redd.it/abc123.gif")
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=convert):
            self.assertEqual(self.task(data).run(), 0)
        file = self.events()[-1]
        self.assertEqual(file["media_kind"], "scene")
        self.assertTrue(file["relative_path"].endswith(".mkv"))
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=AssertionError("Already converted")):
            task = self.task(data)
            task.download = lambda _: self.fail("Archived GIF was downloaded again")
            self.assertEqual(task.run(), 0)
        self.assertEqual(self.events()[-1]["relative_path"], file["relative_path"])

    def test_resume_missing_checkpoint_cannot_report_success(self):
        self.producer.resume_cursor = "missing"
        task = self.task()
        with self.assertRaises(SourcePaused):
            task.run()
        self.assertEqual(self.lease.checkpoints, [])

    def test_legacy_checkpoint_replays_archived_prefix_then_restores_stop_rule(self):
        records = [reddit_data(str(i)) for i in range(1, 13)]
        # Prime earlier files and older history; the sixth file was interrupted.
        prime = self.task(*records[:5], *records[6:10])
        self.assertEqual(prime.run(), 0)
        config.set(("extractor",), "skip", "abort:2")
        # Capture the old archive-key hash for the missing sixth file, before
        # attempting its download. This is the legacy prepare-hook boundary.
        task = self.task(records[5])
        saved = []

        def interrupt(url):
            saved.append(legacy_cursor(task, task.pathfmt))
            raise RuntimeError("fixture interruption")

        task.download = interrupt
        self.assertNotEqual(task.run(), 0)
        self.lease.checkpoints.clear()
        self.lease.run["progress"] = {"items_seen": 6, "files_completed": 0, "cursor": saved[0]}
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="fixture")
        resumed = self.task(*records)
        self.assertEqual(resumed.run(), 0)
        self.assertEqual(resumed.extractor.visited, [r["id"] for r in records[:8]])
        self.assertTrue((self.media / "Account" / "post6_6.jpg").is_file())
        self.assertFalse((self.media / "Account" / "post11_11.jpg").exists())
        self.assertTrue(self.lease.checkpoints)
        self.assertTrue(all(not c[2].startswith(LEGACY_CURSOR_PREFIX) for c in self.lease.checkpoints))

    def test_expanded_recovery_window_never_restores_archive_stop_after_checkpoints(self):
        records = [reddit_data(str(i)) for i in range(1, 5)]
        self.assertEqual(self.task(*records[:3]).run(), 0)
        config.set(("extractor",), "skip", "abort:1")
        self.lease.run["recovery"] = {"activation_uuid": "fixture", "replay_archive": True}
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="fixture")
        resumed = self.task(*records)
        self.assertEqual(resumed.run(), 0)
        self.assertEqual(resumed.extractor.visited, [r["id"] for r in records])
        self.assertTrue((self.media / "Account" / "post4_4.jpg").is_file())

    def test_full_history_policy_ignores_legacy_stop_checkpoint(self):
        self.lease.run["progress"] = {"items_seen": 3, "files_completed": 0, "cursor": LEGACY_CURSOR_PREFIX + "f" * 64}
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="fixture")
        self.assertEqual(self.task().run(), 0)
        self.assertTrue(self.lease.checkpoints)

    def test_missing_legacy_checkpoint_keeps_existing_native_progress(self):
        config.set(("extractor",), "skip", "abort:1")
        self.lease.run["progress"] = {"items_seen": 3, "files_completed": 0, "cursor": LEGACY_CURSOR_PREFIX + "f" * 64}
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="fixture")
        with self.assertRaises(SourcePaused):
            self.task().run()
        self.assertEqual(self.lease.checkpoints, [])

    def test_actual_twitter_transformation_retains_original_membership_and_each_media_id(self):
        self.narrow_window()
        lower = snowflake("2026-10-01T00:00:00.100Z")
        upper = snowflake("2026-10-01T00:00:00.300Z")
        config.set(("extractor",), "filename", "{media_id}_{num}.{extension}")
        config.set(("extractor",), "archive-format", "{media_id}_{num}")
        config.set(("extractor",), "previews", True)
        for transform in (True, False):
            with self.subTest(transform=transform):
                config.set(("extractor",), "transform", transform)
                extractor = TwitterFixture.from_url(self.lease.run["target_url"])
                extractor.records = [{"rest_id": lower, "legacy": {
                    "id_str": lower, "lang": "en", "full_text": "Album caption", "entities": {},
                    "extended_entities": {"media": [
                        {"id_str": "101", "type": "photo", "media_url_https": "https://pbs.twimg.com/media/first.jpg",
                         "original_info": {"width": 100, "height": 200}},
                        {"id_str": "102", "type": "video", "media_url_https": "https://pbs.twimg.com/media/second.jpg",
                         "original_info": {"width": 300, "height": 400}, "video_info": {"variants": [
                             {"bitrate": 1000, "url": "https://video.twimg.com/video/full.mp4"}]}}]}},
                    "user": {"id_str": "99", "screen_name": "example", "name": "Example", "description": "Bio",
                             "created_at": "Thu Oct 01 00:00:00 +0000 2026", "location": "", "verified": False,
                             "protected": False, "profile_image_url_https": ""}}]
                outside = copy.deepcopy(extractor.records[0])
                outside["rest_id"] = outside["legacy"]["id_str"] = upper
                extractor.records.insert(0, outside)
                task = NativeDownloadJob(extractor, producer=self.producer, lock_directory=self.locks)

                def download(url):
                    pf = task.pathfmt
                    pf.part_enable()
                    with pf.open("wb") as output:
                        output.write(url.encode())
                    return True

                task.download = download
                start = len(self.events())
                self.assertEqual(task.run(), 0)
                events = self.events()[start:]
                captures = [event for event in events if event["kind"] == "source.capture"]
                files = [event for event in events if event["kind"] == "file.completed"]
                self.assertEqual(len(captures), 3)
                self.assertEqual([event["source"]["attachment"]["value"] for event in files], ["101", "102", "102"])
                for capture in captures:
                    data = capture["source"].get("legacy", capture["source"])
                    self.assertEqual([item["type"] for item in data["extended_entities"]["media"]], ["photo", "video"])


if __name__ == "__main__":
    unittest.main()
