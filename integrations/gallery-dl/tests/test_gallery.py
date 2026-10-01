"""Real gallery-dl scheduling and postprocessing with local fixture downloads."""

import copy
import hashlib
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
from requests.exceptions import ConnectionError

from gallery_dl import config
from gallery_dl.extractor.common import Extractor, Message
from gallery_dl.extractor.twitter import TwitterExtractor

from stash_ingest.encoding import InvalidData, decode
from stash_ingest.filesystem import Root
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.producer import Producer
from stash_ingest.runs import SourcePaused
from test_producer import LeaseFixture, reddit_data
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

    def task(self, *records):
        extractor = Fixture.from_url(self.lease.run["target_url"])
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

    def test_actual_twitter_transformation_retains_original_membership_and_each_media_id(self):
        config.set(("extractor",), "filename", "{media_id}_{num}.{extension}")
        config.set(("extractor",), "archive-format", "{media_id}_{num}")
        config.set(("extractor",), "previews", True)
        for transform in (True, False):
            with self.subTest(transform=transform):
                config.set(("extractor",), "transform", transform)
                extractor = TwitterFixture.from_url(self.lease.run["target_url"])
                extractor.records = [{"rest_id": "9007199254740993", "legacy": {
                    "id_str": "9007199254740993", "lang": "en", "full_text": "Album caption", "entities": {},
                    "extended_entities": {"media": [
                        {"id_str": "101", "type": "photo", "media_url_https": "https://pbs.twimg.com/media/first.jpg",
                         "original_info": {"width": 100, "height": 200}},
                        {"id_str": "102", "type": "video", "media_url_https": "https://pbs.twimg.com/media/second.jpg",
                         "original_info": {"width": 300, "height": 400}, "video_info": {"variants": [
                             {"bitrate": 1000, "url": "https://video.twimg.com/video/full.mp4"}]}}]}},
                    "user": {"id_str": "99", "screen_name": "example", "name": "Example", "description": "Bio",
                             "created_at": "Thu Oct 01 00:00:00 +0000 2026", "location": "", "verified": False,
                             "protected": False, "profile_image_url_https": ""}}]
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
