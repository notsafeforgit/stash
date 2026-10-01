import hashlib
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from stash_ingest.encoding import InvalidData, decode
from stash_ingest.filesystem import Root, destination_lock
from stash_ingest.outbox import Outbox
from stash_ingest.producer import Producer
from stash_ingest.runs import SourcePaused
from stash_ingest import source
from helpers import PRODUCER, COLLECTION, ROOT, RUN


class LeaseFixture:
    def __init__(self):
        self.client = SimpleNamespace(producer=PRODUCER, endpoint="http://example.invalid")
        self.run = {"uuid": RUN, "collection_uuid": COLLECTION, "collection_revision": 1,
                    "target_url": "https://fixture.invalid/account", "path_prefix": ".",
                    "root_uuid": ROOT, "operation": "download",
                    "progress": {"items_seen": 0, "files_completed": 0, "cursor": ""}}
        self.active = True
        self.checkpoints = []

    def check(self):
        if not self.active:
            raise SourcePaused("Fixture ownership lost")

    def progress(self, *args):
        self.check()
        self.checkpoints.append(args)


def reddit_data(key="abc123", **changes):
    url = "https://i.redd.it/" + key + ".jpg"
    return {"category": "reddit", "id": "post" + key, "url": url, "_url": url,
            "author": "publisher", "title": "Caption", "filename": key,
            "extension": "jpg", "date": "2026-10-01T00:00:00+00:00", **changes}


class ProducerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.media = self.directory / "media"
        self.media.mkdir()
        self.locks = self.directory / "locks"
        self.locks.mkdir()
        self.lease = LeaseFixture()
        self.root = Root(self.media, Root.probe(self.media))
        self.box = Outbox(self.directory / "outbox.sqlite", self.lease.client.endpoint, PRODUCER)
        self.addCleanup(self.box.close)
        self.producer = Producer(self.box, self.lease, self.root, extractor_version="fixture")

    def test_capture_precedes_file_and_current_file_can_finish_after_lease_loss(self):
        data = reddit_data()
        prepared = self.producer.prepare(data)
        self.assertEqual(self.box.status()["counts"]["pending"], 1)
        path = self.media / "transformed.mkv"
        path.write_bytes(b"final transformed media")
        self.lease.active = False
        self.producer.complete(prepared, path)
        rows = [decode(r[0]) for r in self.box.db.execute("SELECT body FROM events ORDER BY seq")]
        self.assertEqual([r["kind"] for r in rows], ["source.capture", "file.completed"])
        self.assertEqual(rows[1]["relative_path"], "transformed.mkv")
        self.assertEqual(rows[1]["sha256"], hashlib.sha256(path.read_bytes()).hexdigest())
        self.assertEqual(rows[1]["source"]["capture_event_uuid"], prepared.event_uuid)
        with self.assertRaises(SourcePaused):
            self.producer.prepare(reddit_data("next"))

    def test_root_replacement_partial_files_and_escapes_are_rejected(self):
        outside = self.directory / "outside.jpg"
        outside.write_bytes(b"outside")
        (self.media / "escape.jpg").symlink_to(outside)
        with self.assertRaises(InvalidData):
            self.root.completed(self.media / "escape.jpg")
        (self.media / "image.jpg.part").write_bytes(b"partial")
        with self.assertRaises(InvalidData):
            self.root.completed(self.media / "image.jpg.part")
        self.media.rename(self.directory / "original")
        self.media.mkdir()
        with self.assertRaises(InvalidData):
            self.root.verify()

    def test_shared_destination_lock_fences_concurrent_workers(self):
        with destination_lock(self.locks, ROOT, "same-stem", self.producer.check):
            with self.assertRaises(InvalidData):
                with destination_lock(self.locks, ROOT, "same-stem", self.producer.check):
                    self.fail("Second writer acquired the destination")
        with destination_lock(self.locks, ROOT, "same-stem", self.producer.check):
            pass

    def test_resume_retains_old_checkpoint_until_the_saved_attachment(self):
        first = self.producer.prepare(reddit_data("first"))
        saved, _ = self.producer.cursor(first)
        self.lease.run["progress"] = {"items_seen": 4, "files_completed": 3, "cursor": saved}
        resumed = Producer(self.box, self.lease, self.root, extractor_version="fixture")
        earlier = resumed.prepare(reddit_data("earlier"))
        key, replay = resumed.cursor(earlier)
        resumed.checkpoint(key, replay, True)
        self.assertEqual(self.lease.checkpoints, [])
        with self.assertRaises(SourcePaused):
            resumed.traversed()
        key, replay = resumed.cursor(first)
        self.assertTrue(replay)
        resumed.checkpoint(key, replay, True)
        after = resumed.prepare(reddit_data("after"))
        key, replay = resumed.cursor(after)
        resumed.checkpoint(key, replay, True)
        self.assertEqual(self.lease.checkpoints, [(5, 4, key)])
        resumed.traversed()

    def test_attachment_identity_ignores_output_number_and_rejects_ambiguity(self):
        data = reddit_data("second", id="gallery", num=1, gallery_data={"items": [
            {"media_id": "missing"}, {"media_id": "second"}]})
        self.assertEqual(source.attachment(data), {"namespace": "native:reddit", "value": "second"})
        twitter = {"category": "twitter", "tweet_id": 9007199254740993, "num": 99,
                   "_url": "https://pbs.twimg.com/media/photo?format=jpg&name=orig",
                   "extended_entities": {"media": [{"id_str": "11", "media_url_https": "https://pbs.twimg.com/media/photo.jpg"}]}}
        self.assertEqual(source.attachment(twitter)["value"], "11")
        twitter["extended_entities"]["media"].append({"id_str": "12", "media_url_https": "https://pbs.twimg.com/media/photo.jpg"})
        with self.assertRaises(source.UnsupportedSource):
            source.attachment(twitter)
        twitter["media_id"] = "12"
        self.assertEqual(source.attachment(twitter)["value"], "12")


if __name__ == "__main__":
    unittest.main()
