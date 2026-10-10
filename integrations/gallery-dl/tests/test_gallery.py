"""Real gallery-dl scheduling and postprocessing with local fixture downloads."""

import copy
import fcntl
from contextlib import closing
from datetime import datetime
import hashlib
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import Mock, patch
from requests.exceptions import ConnectionError, Timeout

from gallery_dl import config, exception
from gallery_dl import archive as gdl_archive
from gallery_dl import extractor as gdl_extractors
from gallery_dl.extractor.common import Extractor, Message
from gallery_dl.extractor import twitter
from gallery_dl.extractor.twitter import TwitterExtractor
from gallery_dl.extractor.reddit import RedditExtractor
from gallery_dl.extractor.redgifs import RedgifsAPI, RedgifsImageExtractor

from stash_ingest.encoding import InvalidData, decode
from stash_ingest.filesystem import Root
from stash_ingest.gallery import NativeDownloadJob, twitter_collection_queue
from stash_ingest.outbox import Outbox
from stash_ingest.producer import Producer
from stash_ingest.publication_lock import ACTIVE, PublicationBarrier
from stash_ingest.runs import SourceFailure, SourcePaused
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


class LinkedRedgifsFixture(Fixture):
    def items(self):
        for item in self.records:
            data = copy.deepcopy(item)
            yield Message.Directory, "", data
            yield Message.Queue, data["url"], {**data, "_extractor": RedgifsImageExtractor}


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
            started = self.events(kinds=None)[-1]
            self.assertEqual((started["kind"], started["state"]), ("attachment.download", "started"))
            pf = task.pathfmt
            pf.part_enable()
            with pf.open("wb") as output:
                output.write(b"fixture media " + url.encode())
            return True

        task.download = download
        return task

    def events(self, *, kinds=("source.capture", "file.completed")):
        # Existing source/file assertions inspect those contracts; lifecycle
        # assertions explicitly request the complete ordered event stream.
        events = [decode(row[0]) for row in self.box.db.execute("SELECT body FROM events ORDER BY seq")]
        return events if kinds is None else [e for e in events if e["kind"] in kinds]

    def archive_count(self):
        with closing(sqlite3.connect(self.directory / "downloads.sqlite")) as db, db:
            return db.execute("SELECT count(*) FROM archive").fetchone()[0]

    def test_download_and_every_postprocessor_phase_hold_publication_lock(self):
        seen = []

        def held(event, _):
            fd = os.open(self.locks / ACTIVE, os.O_RDWR)
            try:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            finally:
                os.close(fd)
            seen.append(event)

        class Processor:
            def __init__(self, task, _):
                for event in ("init", "post", "prepare", "file", "after", "skip", "child", "child-after", "post-after", "finalize"):
                    task.hooks[event].append(lambda pathfmt, event=event: held(event, pathfmt))

        config.set(("extractor",), "postprocessors", [{"name": "publication-fixture"}])
        with patch("gallery_dl.postprocessor.find", return_value=Processor):
            for _ in range(2):
                task = self.task()
                original = task.download

                def download(url):
                    held("download", task.pathfmt)
                    return original(url)

                task.download = download
                self.assertEqual(task.run(), 0)
                # Queue callbacks can run outside a file's enclosing lock.
                for event in ("child", "child-after"):
                    for callback in task.hooks[event]:
                        callback(task.pathfmt)
        self.assertEqual(set(seen), {"init", "post", "prepare", "file", "after", "skip", "child", "child-after", "post-after", "finalize", "download"})
        with PublicationBarrier([self.locks], timeout=1):
            pass

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

    def test_reddit_linked_redgifs_download_and_skip_keep_parent_and_attachment(self):
        self.check_reddit_linked_redgifs_download_and_skip("https://www.redgifs.com/watch/LinkedClip")

    def test_reddit_legacy_redgifs_iframe_download_and_replay(self):
        self.check_reddit_linked_redgifs_download_and_skip("https://v3.redgifs.com/ifr/LinkedClip")

    def check_reddit_linked_redgifs_download_and_skip(self, url):
        self.narrow_window()
        config.set(("extractor", "reddit"), "parent-metadata", "_reddit")
        post = reddit_data(id="redditpost", url=url,
                           date="2026-10-01T00:00:00.200Z")
        clip = {"id": "linkedclip", "createDate": 1577836800, "gallery": None,
                "userName": "different-host-account",
                "urls": {"hd": "https://media.redgifs.com/LinkedClip.mp4",
                         "poster": "https://media.redgifs.com/LinkedClip-poster.jpg"}}
        downloaded = []

        def download(child, url):
            self.assertEqual(self.events(kinds=None)[-1]["state"], "started")
            self.assertEqual(url, clip["urls"]["hd"])
            downloaded.append(url)
            child.pathfmt.part_enable()
            with child.pathfmt.open("wb") as output:
                output.write(b"completed fixture video")
            return True

        with patch.object(RedgifsAPI, "gif", side_effect=lambda *_: copy.deepcopy(clip)), \
                patch.object(NativeDownloadJob, "download", download):
            for _ in range(2):
                task = self.task(post, fixture=LinkedRedgifsFixture)
                self.assertEqual(task.run(), 0)
        self.assertEqual(downloaded, [clip["urls"]["hd"]])
        self.assertEqual(self.archive_count(), 1)
        events = self.events()
        self.assertEqual([event["kind"] for event in events], ["source.capture", "file.completed"] * 2)
        for capture, completed in zip(events[::2], events[1::2]):
            self.assertEqual(capture["post"], {"namespace": "native:reddit", "value": "redditpost"})
            self.assertEqual(capture["source"]["_reddit"]["author"], "publisher")
            self.assertEqual(capture["source"]["_url"], clip["urls"]["hd"])
            self.assertEqual(completed["source"], {
                "capture_event_uuid": capture["event_uuid"],
                "attachment": {"namespace": "native:redgifs", "value": "linkedclip"},
            })
            self.assertEqual(completed["media_kind"], "scene")

    def test_reddit_redgifs_image_permalink_download_and_replay(self):
        self.narrow_window()
        config.set(("extractor", "reddit"), "parent-metadata", "_reddit")
        post = reddit_data(id="imagepost", url="https://i.redgifs.com/i/LinkedPicture.jpg",
                           date="2026-10-01T00:00:00.200Z")
        picture = {"id": "linkedpicture", "createDate": 1577836800, "gallery": None,
                   "urls": {"hd": "https://media.redgifs.com/LinkedPicture-large.jpg",
                            "sd": "https://media.redgifs.com/LinkedPicture-medium.jpg"}}
        downloads = []

        def download(child, url):
            self.assertEqual(self.events(kinds=None)[-1]["state"], "started")
            self.assertEqual(url, picture["urls"]["hd"])
            downloads.append(url)
            child.pathfmt.part_enable()
            with child.pathfmt.open("wb") as output:
                output.write(b"completed fixture image")
            return True

        with patch.object(RedgifsAPI, "gif", side_effect=lambda *_: copy.deepcopy(picture)), \
                patch.object(NativeDownloadJob, "download", download):
            for _ in range(2):
                self.assertEqual(self.task(post, fixture=LinkedRedgifsFixture).run(), 0)
        self.assertEqual(downloads, [picture["urls"]["hd"]])
        self.assertEqual(self.archive_count(), 1)
        events = self.events()
        self.assertEqual([event["kind"] for event in events], ["source.capture", "file.completed"] * 2)
        expected = {"namespace": "native:reddit", "value": "url:" + hashlib.sha256(post["url"].encode()).hexdigest()}
        for capture, completed in zip(events[::2], events[1::2]):
            self.assertEqual(capture["post"], {"namespace": "native:reddit", "value": "imagepost"})
            self.assertEqual(capture["source"]["_reddit"]["url"], post["url"])
            self.assertEqual(completed["source"], {"capture_event_uuid": capture["event_uuid"], "attachment": expected})
            self.assertEqual(completed["media_kind"], "image")

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
        report = self.events(kinds=None)[-1]
        self.assertEqual((report["state"], report["reason_code"]), ("skipped", "archive_entry_without_file"))
        self.assertNotIn("file_event_uuid", report)

    def test_postprocessor_failure_does_not_ack_archive_and_retry_repairs_existing_file(self):
        config.set(("extractor",), "postprocessors", [{"name": "exec", "event": "after", "command": ["fixture", "{_path}"]}])
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", return_value=1):
            task = self.task()
            self.assertNotEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 0)
        self.assertEqual(len(self.events()), 1)
        reports = self.events(kinds=("attachment.download",))
        self.assertEqual([e["state"] for e in reports], ["started", "failed"])
        self.assertEqual(reports[-1]["reason_code"], "postprocess_failed")
        repaired = []
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=lambda *args: repaired.append(args) or 0):
            task = self.task()
            task.download = lambda _: self.fail("Retry should repair existing output")
            self.assertEqual(task.run(), 0)
        self.assertEqual(len(repaired), 1)
        self.assertEqual(self.archive_count(), 1)
        self.assertEqual(self.events()[-1]["kind"], "file.completed")
        self.assertEqual(self.events(kinds=None)[-1]["state"], "downloaded")

    def test_terminal_report_capacity_prevents_archive_ack(self):
        self.box.max_events = 3
        self.assertNotEqual(self.task().run(), 0)
        self.assertEqual(self.archive_count(), 0)
        events = self.events(kinds=None)
        self.assertEqual([e["kind"] for e in events], ["source.capture", "attachment.download", "file.completed"])
        self.assertTrue((self.media / events[-1]["relative_path"]).is_file())
        self.box.max_events = 10
        task = self.task()
        task.download = lambda _: self.fail("Existing completed file must be recovered")
        self.assertEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 1)
        self.assertEqual(self.events(kinds=None)[-1]["state"], "downloaded")

    def test_failed_download_reports_failure_without_claiming_a_file(self):
        task = self.task()
        task.download = lambda _: False
        self.assertNotEqual(task.run(), 0)
        events = self.events(kinds=None)
        self.assertEqual([e["kind"] for e in events], ["source.capture", "attachment.download", "attachment.download"])
        self.assertEqual([e["state"] for e in events[1:]], ["started", "failed"])
        self.assertEqual(events[-1]["reason_code"], "download_failed")
        self.assertEqual(self.archive_count(), 0)

    def test_fallback_urls_share_one_transfer_and_do_not_record_early_failure(self):
        config.set(("extractor",), "fallback", True)
        task = self.task(reddit_data(_fallback=["https://fixture.invalid/fallback.jpg"]))
        original = task.download
        calls = []

        def download(url):
            calls.append(url)
            return False if len(calls) == 1 else original(url)

        task.download = download
        self.assertEqual(task.run(), 0)
        self.assertEqual(len(calls), 2)
        reports = self.events(kinds=("attachment.download",))
        self.assertEqual([e["state"] for e in reports], ["started", "downloaded"])
        self.assertEqual(reports[0]["transfer_sequence"], reports[1]["transfer_sequence"])
        self.assertEqual(reports[0]["capture_event_uuid"], reports[1]["capture_event_uuid"])

    def test_interrupted_download_retains_start_without_a_terminal_claim(self):
        task = self.task()

        def interrupted(url):
            raise KeyboardInterrupt()

        task.download = interrupted
        with self.assertRaises(KeyboardInterrupt):
            task.run()
        reports = self.events(kinds=("attachment.download",))
        self.assertEqual([e["state"] for e in reports], ["started"])
        self.assertEqual(self.archive_count(), 0)

    def test_outbox_exhaustion_preserves_file_without_archive_ack(self):
        self.box.max_events = 2
        task = self.task()
        self.assertNotEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 0)
        self.assertTrue((self.media / "Account/postabc123_abc123.jpg").is_file())
        self.box.max_events = 10
        task = self.task()
        task.download = lambda _: self.fail("Existing file must be recovered")
        self.assertEqual(task.run(), 0)
        self.assertEqual(self.archive_count(), 1)

    def test_archive_add_follows_durable_capture_and_file_events(self):
        original = gdl_archive.DownloadArchive.add
        observed = []

        def add(archive, keywords):
            # The backup snapshots download archives before outboxes. Prove
            # the adapter's prerequisite from an independent SQLite reader,
            # so an uncommitted row on the producer connection is insufficient.
            with closing(sqlite3.connect(self.box.path)) as reader:
                rows = reader.execute('SELECT event_uuid,kind,body FROM events ORDER BY seq').fetchall()
            self.assertEqual([row[1] for row in rows], ['source.capture', 'attachment.download', 'file.completed', 'attachment.download'])
            event = decode(rows[-2][2])
            self.assertEqual(event['source']['capture_event_uuid'], rows[0][0])
            terminal = decode(rows[-1][2])
            self.assertEqual(terminal['state'], 'downloaded')
            self.assertEqual(terminal['file_event_uuid'], rows[-2][0])
            self.assertFalse(self.box.db.in_transaction)
            observed.append(rows[-1][0])
            return original(archive, keywords)

        with patch.object(gdl_archive.DownloadArchive, 'add', add):
            self.assertEqual(self.task().run(), 0)
        self.assertEqual(len(observed), 1)
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

    def test_busy_child_is_reserved_before_initialization_and_stops_other_children(self):
        reserved = []
        def reserve(url):
            reserved.append(url)
            if url.endswith("/child"):
                raise SourceFailure("source_busy", "service:redgifs")
            return "service:reddit"
        self.lease.reserve_source = reserve
        task = self.task(reddit_data(), reddit_data("second"), fixture=ParentFixture)
        with patch.object(ChildFixture, "initialize") as initialize:
            with self.assertRaises(SourceFailure) as failure:
                task.run()
            initialize.assert_not_called()
        self.assertEqual((failure.exception.code, failure.exception.scope), ("source_busy", "service:redgifs"))
        self.assertEqual(reserved, [self.lease.run["target_url"], "https://fixture.invalid/child"])
        self.assertEqual(self.events(), [])

    def test_child_login_failure_is_attributed_to_child_not_parent(self):
        self.lease.reserve_source = lambda url: "service:redgifs" if url.endswith("/child") else "service:reddit"
        task = self.task(reddit_data(), fixture=ParentFixture)
        with patch.object(ChildFixture, "initialize", side_effect=exception.AuthenticationError("private-cookie")):
            with self.assertRaises(SourceFailure) as failure:
                task.run()
        self.assertEqual((failure.exception.code, failure.exception.scope), ("authentication", "service:redgifs"))
        self.assertNotIn("private-cookie", str(failure.exception))
        self.assertEqual(self.events(), [])

    def test_source_rate_limit_and_timeout_stop_internal_http_retries(self):
        for code in ("rate_limited", "timeout"):
            with self.subTest(code=code):
                self.producer.source_failure = None
                task = self.task()
                task._init()
                response = Mock(status_code=429)
                with patch.object(task.extractor.session, "request", return_value=response,
                                  side_effect=Timeout("private-url") if code == "timeout" else None) as request:
                    with self.assertRaises(SourceFailure) as failure:
                        task.extractor.request("https://fixture.invalid/page", retries=3, interval=False)
                    self.assertEqual(failure.exception.code, code)
                    self.assertNotIn("private-url", str(failure.exception))
                    with self.assertRaises(SourceFailure):
                        task.extractor.request("https://fixture.invalid/next")
                    self.assertEqual(request.call_count, 1)
                if code == "rate_limited":
                    response.close.assert_called_once()

    def test_source_http_dependency_reserves_and_attributes_actual_request_host(self):
        for busy in (False, True):
            with self.subTest(busy=busy):
                self.producer.source_failure = None
                reservations = []

                def reserve(url):
                    reservations.append(url)
                    if url == self.lease.run['target_url']:
                        return 'service:reddit'
                    if busy:
                        raise SourceFailure('source_busy', 'host:metadata.invalid')
                    return 'host:metadata.invalid'

                self.lease.reserve_source = reserve
                task = self.task()
                task._init()
                response = Mock(status_code=429)
                with patch.object(task.extractor.session, 'request', return_value=response) as request:
                    with self.assertRaises(SourceFailure) as failure:
                        task.extractor.request('https://metadata.invalid/probe?token=private', interval=False)
                    self.assertEqual(failure.exception.scope, 'host:metadata.invalid')
                    self.assertEqual(failure.exception.code, 'source_busy' if busy else 'rate_limited')
                    self.assertEqual(request.call_count, 0 if busy else 1)
                self.assertEqual(reservations, [self.lease.run['target_url'], 'https://metadata.invalid/probe?token=private'])

    def test_individual_media_failure_does_not_create_a_service_cooldown(self):
        task = self.task()
        task.download = Mock(side_effect=Timeout("missing media"))
        self.assertNotEqual(task.run(), 0)
        self.assertIsNone(self.producer.source_failure)

    def test_source_missing_post_and_controlled_archive_stop_have_different_outcomes(self):
        task = self.task()
        def missing():
            raise exception.NotFoundError("private-url")
            yield
        task.extractor.items = missing
        with self.assertRaises(SourceFailure) as failure:
            task.run()
        self.assertEqual(failure.exception.code, "not_found")
        self.producer.source_failure = None
        task = self.task()
        def stopped():
            raise exception.StopExtraction()
            yield
        task.extractor.items = stopped
        self.assertEqual(task.run(), 0)
        self.assertIsNone(self.producer.source_failure)

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
        self.assertEqual(file["transformation"], {"kind": "gif-to-video",
                         "original_relative_path": file["relative_path"][:-4] + ".gif"})
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=AssertionError("Already converted")):
            task = self.task(data)
            task.download = lambda _: self.fail("Archived GIF was downloaded again")
            self.assertEqual(task.run(), 0)
        self.assertEqual(self.events()[-1]["relative_path"], file["relative_path"])
        self.assertEqual(self.events()[-1]["transformation"], file["transformation"])

    def test_interrupted_gif_completion_recovers_conversion_after_outbox_reopen(self):
        config.set(("extractor",), "postprocessors", [{"name": "exec", "event": "after",
                    "command": ["python3", "/fixture/gif_to_av1_qsv.py", "{_path}"]}])
        data = reddit_data(extension="gif", url="https://i.redd.it/abc123.gif", _url="https://i.redd.it/abc123.gif")

        def convert(args, shell):
            original = Path(args[-1])
            original.rename(original.with_suffix(".mkv"))
            return 0

        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=convert), \
                patch.object(self.producer, "complete", side_effect=OSError("interrupted before local completion")):
            self.assertNotEqual(self.task(data).run(), 0)
        self.assertFalse(any(e["kind"] == "file.completed" for e in self.events()))
        self.box.close()
        self.box = Outbox(self.directory / "outbox.sqlite", self.lease.client.endpoint, PRODUCER)
        self.addCleanup(self.box.close)
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="fixture")
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=AssertionError("Already converted")):
            task = self.task(data)
            task.download = lambda _: self.fail("Converted GIF was downloaded again")
            self.assertEqual(task.run(), 0)
        event = self.events()[-1]
        self.assertEqual(event["transformation"], {"kind": "gif-to-video",
                         "original_relative_path": event["relative_path"][:-4] + ".gif"})

    def test_unconfigured_mkv_sibling_does_not_claim_gif_conversion(self):
        original = self.media / "Account" / "postabc123_abc123.gif"
        original.parent.mkdir()
        original.with_suffix(".mkv").write_bytes(b"unrelated video")
        data = reddit_data(extension="gif", url="https://i.redd.it/abc123.gif", _url="https://i.redd.it/abc123.gif")
        self.assertEqual(self.task(data).run(), 0)
        event = self.events()[-1]
        self.assertEqual(event["media_kind"], "image")
        self.assertNotIn("transformation", event)

    def test_converted_gif_recovery_respects_forced_downloads(self):
        config.set(("extractor",), "skip", False)
        config.set(("extractor",), "postprocessors", [{"name": "exec", "event": "after",
                    "command": ["python3", "/fixture/gif_to_av1_qsv.py", "{_path}"]}])
        original = self.media / "Account" / "postabc123_abc123.gif"
        original.parent.mkdir()
        original.with_suffix(".mkv").write_bytes(b"previous conversion")

        def convert(args, shell):
            self.assertEqual(Path(args[-1]), original)
            original.replace(original.with_suffix(".mkv"))
            return 0

        data = reddit_data(extension="gif", url="https://i.redd.it/abc123.gif", _url="https://i.redd.it/abc123.gif")
        with patch("gallery_dl.postprocessor.exec.ExecPP._exec", side_effect=convert) as converter:
            self.assertEqual(self.task(data).run(), 0)
            self.assertEqual(converter.call_count, 1)
        self.assertEqual(self.events()[-1]["sha256"], hashlib.sha256(original.with_suffix(".mkv").read_bytes()).hexdigest())

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

    def test_twitter_profile_routes_to_posts_with_the_original_window(self):
        self.narrow_window()
        self.lease.run["target_url"] = "https://x.com/example"
        lower = snowflake("2026-10-01T00:00:00.100Z")
        upper = snowflake("2026-10-01T00:00:00.300Z")
        record = {"rest_id": lower, "legacy": {
            "id_str": lower, "lang": "en", "full_text": "Caption", "entities": {}, "user_id_str": "99",
            "extended_entities": {"media": [
                {"id_str": "101", "type": "photo", "media_url_https": "https://pbs.twimg.com/media/first.jpg",
                 "original_info": {"width": 100, "height": 200}}]}},
            "user": {"id_str": "99", "screen_name": "example", "name": "Example", "description": "Bio",
                     "created_at": "Thu Oct 01 00:00:00 +0000 2026", "location": "", "verified": False,
                     "protected": False, "profile_image_url_https": ""}}
        outside = copy.deepcopy(record)
        outside["rest_id"] = outside["legacy"]["id_str"] = upper
        config.set(("extractor",), "filename", "{media_id}_{num}.{extension}")

        def download(child, url):
            child.pathfmt.part_enable()
            with child.pathfmt.open("wb") as output:
                output.write(b"fixture " + url.encode())
            return True

        cases = [("https://x.com/example", include, child) for include, child in (
            ("timeline", twitter.TwitterTimelineExtractor), ("tweets", twitter.TwitterTweetsExtractor),
            ("media", twitter.TwitterMediaExtractor), ("with-replies", twitter.TwitterWithRepliesExtractor),
            ("highlights", twitter.TwitterHighlightsExtractor), ("likes", twitter.TwitterLikesExtractor))]
        cases.append(("https://x.com/i/user/99", "timeline", twitter.TwitterTimelineExtractor))
        for target, include, child in cases:
            with self.subTest(target=target, include=include):
                self.lease.run["target_url"] = target
                config.set(("extractor", "twitter"), "include", [include])
                with patch.object(TwitterExtractor, "login"), \
                     patch.object(TwitterExtractor, "metadata", return_value={}), \
                     patch.object(child, "tweets", lambda _: iter(copy.deepcopy([outside, record]))), \
                     patch.object(NativeDownloadJob, "download", download):
                    # Other fixtures instantiate extractors directly, which
                    # compiles their patterns without registering the module.
                    # Keep this routing test independent of registry load order.
                    task = NativeDownloadJob(twitter.TwitterUserExtractor.from_url(self.lease.run["target_url"]),
                                             producer=self.producer, lock_directory=self.locks)
                    start = len(self.events())
                    self.assertEqual(task.run(), 0)
                events = self.events()[start:]
                self.assertEqual([e["kind"] for e in events], ["source.capture", "file.completed"])
                self.assertEqual(events[0]["post"], {"namespace": "native:twitter", "value": lower})
                self.assertEqual(events[1]["source"]["attachment"]["value"], "101")

    def test_twitter_profile_routing_does_not_exempt_posts_or_unrelated_children(self):
        parent = twitter.TwitterUserExtractor.from_url("https://x.com/example")
        child = twitter.TwitterTimelineExtractor
        data = {"_extractor": child}
        self.assertTrue(twitter_collection_queue(parent, "https://x.com/example/timeline", data))
        self.assertFalse(twitter_collection_queue(parent, "https://example.test/", data))
        self.assertFalse(twitter_collection_queue(parent, "https://x.com/example/timeline", {**data, "tweet_id": 123}))
        self.assertFalse(twitter_collection_queue(parent, "https://x.com/example/photo", {"_extractor": twitter.TwitterAvatarExtractor}))
        self.assertFalse(twitter_collection_queue(child.from_url("https://x.com/example/timeline"),
                                                 "https://x.com/example/timeline", data))

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
                    "conversation_id_str": "1900000000000000000", "in_reply_to_status_id_str": "1900000000000000001",
                    "in_reply_to_user_id_str": "99", "user_id_str": "99",
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
                    self.assertEqual(capture["source"]["reply_user_id"], "99")


if __name__ == "__main__":
    unittest.main()
