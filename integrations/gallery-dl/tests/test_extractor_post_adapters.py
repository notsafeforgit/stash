"""Exercise the pinned extractors' real metadata transformations offline."""

import copy
from datetime import datetime, timezone
import unittest
from unittest.mock import patch

from gallery_dl import config, extractor

from stash_ingest import source
from stash_ingest.gallery import SUPPORTED_VERSION
from stash_ingest.metadata_bundle import Bundle
from stash_ingest.metadata_fetch import collect


STAMP = int(datetime(2026, 10, 1, 12, tzinfo=timezone.utc).timestamp())


class ExtractorPostAdapterTests(unittest.TestCase):
    def setUp(self):
        config.clear()
        self.addCleanup(config.clear)
        network = patch("requests.sessions.Session.send", side_effect=AssertionError("Unexpected website request"))
        network.start()
        self.addCleanup(network.stop)

    def check_post(self, target, namespace, value, count):
        result = collect(target.url, {}, factory=lambda _: target)
        self.assertNotIn("error", result)
        bundle = Bundle(target.url, SUPPORTED_VERSION, result)
        media = []
        self.assertEqual(len(result["records"]), count + 1)
        for i, record in enumerate(result["records"]):
            data = bundle.metadata(i, with_parent=True)
            self.assertEqual(source.post(data), {"namespace": namespace, "value": value})
            self.assertEqual(source.metadata(data)["original_text"], "Original caption")
            self.assertTrue(source.metadata(data)["published_at"].startswith("2026-10-01T12:00:00"))
            if record["kind"] == "media":
                media.append(data)
        self.assertEqual(len(media), count)
        return media

    def test_bluesky_blob_alt_text_does_not_replace_the_post(self):
        target = extractor.find("https://bsky.app/profile/did:plc:example/post/3abc")
        post = {"uri": "at://did:plc:example/app.bsky.feed.post/3abc",
                "author": {"did": "did:plc:example", "handle": "example.test"},
                "record": {"text": "Original caption", "createdAt": "2026-10-01T12:00:00.123Z",
                           "embed": {"images": [
                               {"alt": "First image", "image": {"ref": {"$link": "blob1"}, "mimeType": "image/jpeg"}},
                               {"alt": "Second image", "image": {"ref": {"$link": "blob2"}, "mimeType": "image/jpeg"}}]}}}
        target.posts = lambda: iter([copy.deepcopy(post)])
        with patch("gallery_dl.extractor.bluesky.BlueskyAPI.service_endpoint", lambda self, did: "https://pds.example.test"):
            media = self.check_post(target, "native:bluesky", "did:plc:example/3abc", 2)
        self.assertEqual([item["description"] for item in media], ["First image", "Second image"])

    def test_instagram_carousel_keeps_post_id_and_date(self):
        target = extractor.find("https://www.instagram.com/p/Example/")
        target.login = lambda: None
        target.metadata = lambda: {}
        post = {"pk": "123", "code": "Example", "caption": {"text": "Original caption"},
                "user": {"pk": "456", "username": "example"}, "taken_at": STAMP,
                "carousel_media": [
                    {"pk": str(700+i), "code": "Image"+str(i), "taken_at": STAMP-100,
                     "image_versions2": {"candidates": [{"url": "https://media.example.test/"+str(i)+".jpg", "width": 1024, "height": 1024}]}}
                    for i in (1, 2)]}
        target.posts = lambda: iter([copy.deepcopy(post)])
        media = self.check_post(target, "native:instagram", "123", 2)
        self.assertEqual([str(item["media_id"]) for item in media], ["701", "702"])
        self.assertNotEqual(media[0]["post_date"], media[0]["date"])

    def test_tiktok_image_ids_do_not_replace_the_post(self):
        url = "https://www.tiktok.com/@example/video/9007199254740993"
        target = extractor.find(url)
        target.posts = lambda: iter([url])
        post = {"id": 9007199254740993, "desc": "Original caption", "createTime": STAMP,
                "author": {"id": "456", "uniqueId": "example"},
                "imagePost": {"images": [
                    {"imageURL": {"urlList": ["https://media.example.test/"+str(i)+".jpg"]}, "imageWidth": 1024, "imageHeight": 1024}
                    for i in (1, 2)]}}
        target._extract_rehydration_data = lambda _: {"webapp.video-detail": {"statusCode": 0, "itemInfo": {"itemStruct": copy.deepcopy(post)}}}
        self.check_post(target, "native:tiktok", "9007199254740993", 2)

    def test_fansly_file_dates_do_not_replace_publication(self):
        target = extractor.find("https://fansly.com/post/123")
        post = {"id": "123", "createdAt": STAMP, "content": "Original caption", "account": {"id": "456", "username": "example"},
                "attachments": [{"id": "attachment1", "media": {"id": "file1", "createdAt": STAMP-100, "updatedAt": STAMP-50,
                    "variants": [{"id": "variant1", "width": 1024, "type": 1, "mimetype": "image/jpeg", "locations": [{"location": "https://media.example.test/image.jpg"}]}]}}]}
        target.posts = lambda: iter([copy.deepcopy(post)])
        media = self.check_post(target, "native:fansly", "123", 1)
        self.assertNotEqual(media[0]["date"], media[0]["file"]["date"])

    def test_mirror_file_ids_keep_the_mirrored_account(self):
        for category, domain, service in (("coomer", "coomer.st", "onlyfans"), ("kemono", "kemono.cr", "patreon")):
            with self.subTest(category=category):
                target = extractor.find(f"https://{domain}/{service}/user/456/post/123")
                post = {"id": "123", "service": service, "user": "456", "content": "Original caption", "published": "2026-10-01T12:00:00",
                        "file": {"id": "attachment1", "path": "/aa/bb/"+"c"*64+".jpg", "name": "image.jpg"}, "attachments": []}
                target.posts = lambda: iter([copy.deepcopy(post)])
                with patch("gallery_dl.extractor.kemono.KemonoAPI.creator_profile", return_value={"id": "456", "service": service, "name": "Example"}):
                    media = self.check_post(target, "mirror:"+category+":"+service, "456/123", 1)
                self.assertEqual(media[0]["file_id"], "attachment1")

    def test_patreon_api_relationships_preserve_post_and_creator(self):
        target = extractor.find("https://www.patreon.com/posts/123")
        post = {"id": "123", "attributes": {"title": "Post title", "content": "Original caption", "published_at": "2026-10-01T12:00:00Z"},
                "relationships": {"user": {"links": {"related": "https://www.patreon.com/api/user/456"}, "data": {"id": "456"}},
                                  "attachments_media": {"data": [{"type": "media", "id": "attachment1"}]}}}
        included = {"media": {"attachment1": {"download_url": "https://media.example.test/image.jpg", "file_name": "image.jpg"}}}
        target._user = lambda _: {"id": "456", "name": "Example"}
        target.posts = lambda: iter([target._process(copy.deepcopy(post), copy.deepcopy(included))])
        media = self.check_post(target, "native:patreon", "123", 1)
        self.assertEqual(media[0]["creator"]["id"], "456")


if __name__ == "__main__":
    unittest.main()
