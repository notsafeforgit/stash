"""Pinned Bluesky/TikTok download and source-album contracts without network."""

import copy
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import unittest
from unittest.mock import patch

from gallery_dl import config, extractor
from gallery_dl.extractor import bluesky, tiktok

from stash_ingest import social_media, source
from stash_ingest.encoding import InvalidData
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.metadata_fetch import collect
from stash_ingest.retention import retain
from stash_ingest.runs import SourceFailure
from stash_ingest.source_window import SourceWindow
import test_gallery


def blob(key, kind='image'):
    return {kind: {'ref': {'$link': key}, 'mimeType': kind + ('/jpeg' if kind == 'image' else '/mp4')},
            'alt': 'Alt text for ' + key}


def bluesky_post(key='3abc', *, created='2026-10-01T12:00:00.123456Z', embed=None):
    return {'uri': 'at://did:plc:example/app.bsky.feed.post/' + key,
            'author': {'did': 'did:plc:example', 'handle': 'example.test'},
            'record': {'text': 'Original caption', 'createdAt': created,
                       'embed': embed if embed is not None else {'images': [blob('blobA'), None, blob('blobB')]}}}


def photo(key):
    return {'imageURL': {'urlList': ['https://media.example.test/' + key + '~original.jpeg?signature=one']},
            'imageWidth': 1024, 'imageHeight': 2048}


def tiktok_post(**changes):
    return {'id': '9007199254740993', 'createTime': '1790856000', 'desc': 'Original caption',
            'author': {'id': '456', 'uniqueId': 'example'},
            'imagePost': {'images': [photo('photoA'), None, photo('photoB')]}, **changes}


class BlueskyFixture(bluesky.BlueskyPostExtractor):
    def posts(self):
        return iter(copy.deepcopy(self.records))


class TiktokFixture(tiktok.TiktokPostExtractor):
    def posts(self):
        return [f"https://www.tiktok.com/@example/video/{post['id']}" for post in self.records]

    def _extract_rehydration_data(self, url):
        post = next(post for post in self.records if str(post['id']) == url.rsplit('/', 1)[-1])
        return {'webapp.video-detail': {'statusCode': 0, 'itemInfo': {'itemStruct': copy.deepcopy(post)}}}


class SocialContractTests(unittest.TestCase):
    def test_shared_go_membership_contract_survives_retention(self):
        path = Path(__file__).resolve().parents[3] / 'pkg/archive/testdata/captured-social-media-v1.json'
        for case in json.loads(path.read_text())['cases']:
            for value in (case['source'], retain(case['source'])):
                with self.subTest(name=case['name']):
                    if case['error']:
                        with self.assertRaises(InvalidData):
                            social_media.manifest(value, value['category'])
                        continue
                    evidence = social_media.manifest(value, value['category'])
                    if case['items'] is None:
                        self.assertIsNone(evidence)
                    else:
                        self.assertEqual(evidence['items'], case['items'])
                        if case['attachment'] is not None:
                            self.assertEqual(source.attachment(value), case['attachment'])


class SocialGalleryTests(unittest.TestCase):
    def setUp(self):
        test_gallery.GalleryTests.setUp(self)
        self.lease.run['target_url'] = 'https://bsky.app/profile/did:plc:example/post/3abc'
        config.set(('extractor',), 'filename', '{post_id|id}_{num}.{extension}')
        config.set(('extractor',), 'archive-format', '{category}_{post_id|id}_{num}')
        config.set(('extractor', 'tiktok'), 'audio', False)
        endpoint = patch.object(bluesky.BlueskyAPI, 'service_endpoint', lambda self, did: 'https://pds.example.test')
        endpoint.start()
        self.addCleanup(endpoint.stop)

    task = test_gallery.GalleryTests.task
    events = test_gallery.GalleryTests.events

    def archive_count(self):
        with closing(sqlite3.connect(self.directory / 'downloads.sqlite')) as db:
            return db.execute('SELECT count(*) FROM archive').fetchone()[0]

    def test_bluesky_missing_and_repeated_source_slots(self):
        record = bluesky_post(embed={'images': [blob('blobA'), None, blob('blobB'), blob('blobA')]})
        self.assertEqual(self.task(record, fixture=BlueskyFixture).run(), 0)
        files = [e for e in self.events() if e['kind'] == 'file.completed']
        self.assertEqual([e['source']['attachment']['value'] for e in files], ['blobA', 'blobB', 'blobA'])
        for event in self.events():
            if event['kind'] == 'source.capture':
                self.assertEqual(event['source']['bluesky_media']['items'], [
                    {'id': 'blobA', 'kind': 'image'}, None, {'id': 'blobB', 'kind': 'image'}, {'id': 'blobA', 'kind': 'image'}])
                self.assertEqual(event['metadata']['original_text'], 'Original caption')
        self.assertEqual(self.archive_count(), 3)

    def test_bluesky_video_selection_keeps_full_membership(self):
        config.set(('extractor', 'bluesky'), 'videos', False)
        record = bluesky_post(embed={'items': [blob('blobA'), blob('blobV', 'video')]})
        self.assertEqual(self.task(record, fixture=BlueskyFixture).run(), 0)
        self.assertEqual(self.archive_count(), 1)
        capture = next(e for e in self.events() if e['kind'] == 'source.capture')
        self.assertEqual(capture['source']['bluesky_media']['items'], [
            {'id': 'blobA', 'kind': 'image'}, {'id': 'blobV', 'kind': 'video'}])

    def test_bluesky_fractional_window_and_quoted_post_identity(self):
        config.set(('extractor', 'bluesky'), 'quoted', True)
        self.producer.window = SourceWindow({'since': '2026-10-01T12:00:00.123Z', 'until': '2026-10-01T12:00:00.124Z'})
        outer = bluesky_post(embed={'images': [blob('blobA')]})
        quote = bluesky_post('3quoted', embed={'images': [blob('blobQ')]})
        outer['embed'] = {'record': {'record': {**quote, 'value': quote.pop('record')}}}
        old = bluesky_post('3old', created='2026-09-01T12:00:00Z')
        until = bluesky_post('3until', created='2026-10-01T12:00:00.124Z')
        self.assertEqual(self.task(old, outer, until, fixture=BlueskyFixture).run(), 0)
        self.assertEqual([e['post']['value'] for e in self.events() if e['kind'] == 'source.capture'],
                         ['did:plc:example/3abc', 'did:plc:example/3quoted'])

    def tiktok_task(self, *records):
        self.lease.run['target_url'] = 'https://www.tiktok.com/@example/video/9007199254740993'
        return self.task(*records, fixture=TiktokFixture)

    def test_tiktok_photos_preserve_missing_slots_numbers_and_raw_source(self):
        record = tiktok_post()
        self.assertEqual(self.tiktok_task(record).run(), 0)
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        files = [e for e in self.events() if e['kind'] == 'file.completed']
        self.assertEqual([e['source']['num'] for e in captures], [1, 3])
        self.assertEqual([e['source']['attachment']['value'] for e in files], ['image:photoA', 'image:photoB'])
        for event in captures:
            self.assertEqual(event['source']['imagePost'], record['imagePost'])
            self.assertEqual(event['source']['tiktok_media']['items'], [
                {'id': 'image:photoA', 'kind': 'image'}, None, {'id': 'image:photoB', 'kind': 'image'}])
        self.assertEqual(self.archive_count(), 2)

    def test_tiktok_video_uses_post_identity_with_original_publication_window(self):
        record = tiktok_post()
        del record['imagePost']
        record['video'] = {'playAddr': 'https://media.example.test/movie.mp4', 'duration': 10, 'width': 1080, 'height': 1920}
        self.producer.window = SourceWindow({'since': '2026-10-01T11:59:59Z', 'until': '2026-10-01T12:00:01Z'})
        self.assertEqual(self.tiktok_task(record).run(), 0)
        files = [e for e in self.events() if e['kind'] == 'file.completed']
        self.assertEqual([e['source']['attachment']['value'] for e in files], ['video:9007199254740993'])
        self.assertEqual(files[0]['media_kind'], 'scene')

    def test_tiktok_extraction_errors_cannot_claim_source_success(self):
        record = tiktok_post(imagePost={'images': [photo('photoA')]})
        task = self.tiktok_task(record)
        task.extractor._extract_rehydration_data = lambda _: {'webapp.video-detail': {'statusCode': 10204}}
        with self.assertRaises(SourceFailure) as failure:
            task.run()
        self.assertEqual(failure.exception.code, 'extraction_failed')
        self.assertEqual(self.events(), [])
        self.assertEqual(self.lease.checkpoints, [])

    def test_tiktok_metadata_errors_remain_failed(self):
        target = extractor.find('https://www.tiktok.com/@example/video/123')
        target._extract_rehydration_data = lambda _: {'webapp.video-detail': {'statusCode': 10204}}
        self.assertEqual(collect(target.url, {}, factory=lambda _: target)['error'], 'extraction_failed')

    def test_auxiliary_outputs_and_source_keyword_overrides_are_rejected(self):
        config.set(('extractor', 'tiktok'), 'audio', True)
        with self.assertRaisesRegex(InvalidData, 'audio, covers and subtitles'):
            self.tiktok_task(tiktok_post()).run()
        config.set(('extractor', 'tiktok'), 'audio', False)
        config.set(('extractor', 'tiktok'), 'keywords', {'createTime': '1'})
        with self.assertRaisesRegex(InvalidData, 'keywords'):
            self.tiktok_task(tiktok_post()).run()
        self.assertEqual(self.events(), [])

    def test_profiles_and_shortlinks_route_without_losing_original_windows(self):
        def download(job, url):
            job.pathfmt.part_enable()
            with job.pathfmt.open('wb') as output:
                output.write(b'fixture ' + url.encode())
            return True

        for url in ('https://bsky.app/profile/example.test', 'https://www.tiktok.com/@example', 'https://vm.tiktok.com/abc123'):
            self.lease.run['target_url'] = url
            self.producer.window = SourceWindow({'since': '2026-10-01T11:59:59Z', 'until': '2026-10-01T12:00:01Z'})
            with patch.object(bluesky.BlueskyMediaExtractor, 'posts', lambda _: iter([bluesky_post()])), \
                 patch.object(tiktok.TiktokPostsExtractor, 'posts', lambda _: iter(['https://www.tiktok.com/@example/video/9007199254740993'])), \
                 patch.object(tiktok.TiktokExtractor, '_extract_rehydration_data', lambda *_: {'webapp.video-detail': {'statusCode': 0, 'itemInfo': {'itemStruct': tiktok_post()}}}), \
                 patch.object(tiktok.TiktokVmpostExtractor, 'request_location', return_value='https://www.tiktok.com/@example/video/9007199254740993'), \
                 patch.object(NativeDownloadJob, 'download', download):
                task = NativeDownloadJob(extractor.find(url), producer=self.producer, lock_directory=self.locks)
                self.assertEqual(task.run(), 0)
        self.assertEqual({e['post']['namespace'] for e in self.events() if e['kind'] == 'source.capture'}, {'native:bluesky', 'native:tiktok'})

    def test_explicit_unsupported_profile_includes_fail_before_source_access(self):
        self.lease.run['target_url'] = 'https://www.tiktok.com/@example'
        for value in ('avatar,posts', ['following'], [{}]):
            config.set(('extractor', 'tiktok'), 'include', value)
            task = NativeDownloadJob(extractor.find(self.lease.run['target_url']), producer=self.producer, lock_directory=self.locks)
            with self.assertRaisesRegex(InvalidData, 'post collections only'):
                task.run()
        self.assertEqual(self.events(), [])


if __name__ == '__main__':
    unittest.main()
