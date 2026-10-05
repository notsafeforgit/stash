"""Pinned Instagram transformations, source identity, windows and file intake."""

import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import unittest
from unittest.mock import patch

from gallery_dl import config, extractor
from gallery_dl.extractor.instagram import InstagramExtractor
from stash_ingest import source
from stash_ingest.encoding import InvalidData
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.retention import retain
from stash_ingest.source_window import published, SourceWindow
import test_gallery

STAMP = int(datetime(2026, 10, 1, 12, tzinfo=timezone.utc).timestamp())


def media(number, *, video=False, stamp=STAMP):
    value = {'pk': str(number), 'code': 'item' + str(number), 'taken_at': stamp,
             'media_type': 2 if video else 1,
             'image_versions2': {'candidates': [{'url': 'https://media.example/' + str(number) + '.jpg', 'width': 100, 'height': 100}]}}
    if video:
        value['video_versions'] = [{'url': 'https://media.example/' + str(number) + '.mp4', 'width': 100, 'height': 100}]
    return value


def carousel():
    return {'pk': '123', 'code': 'post', 'caption': {'text': 'Original caption'},
            'user': {'pk': '456', 'username': 'example'}, 'taken_at': STAMP,
            'carousel_media': [media(701, stamp=STAMP-100), None, media(702, video=True, stamp=STAMP-200)]}


def stories(*, highlight=False):
    return {'id': 'highlight:987' if highlight else '456', 'seen': STAMP + 86400,
            'user': {'pk': '456', 'username': 'example'},
            'items': [media(701, stamp=STAMP-1), media(702, video=True), media(703, stamp=STAMP+1)]}


class InstagramFixture(InstagramExtractor):
    subcategory = 'posts'
    pattern = r'https://fixture.invalid/(account)'

    def login(self):
        pass

    def metadata(self):
        return {}

    def posts(self):
        return iter(copy.deepcopy(self.records))


class InstagramTests(unittest.TestCase):
    def test_shared_go_contract_including_retention_and_attachment_membership(self):
        fixtures = json.loads((Path(__file__).resolve().parents[3] / 'pkg/archive/testdata/captured-instagram-media-v1.json').read_text())
        for fixture in fixtures['cases']:
            for data in (fixture['source'], retain(fixture['source'])):
                with self.subTest(name=fixture['name']):
                    if fixture['error'] or fixture['post'] is None:
                        with self.assertRaises(InvalidData):
                            source.post(data)
                        continue
                    self.assertEqual(source.post(data), fixture['post'])
                    if fixture['attachment'] is None:
                        with self.assertRaises(InvalidData):
                            source.attachment(data)
                    else:
                        self.assertEqual(source.attachment(data), {'namespace': 'native:instagram', 'value': fixture['attachment']})
                        key = 'date' if data['type'] in ('story', 'highlight') else 'post_date'
                        self.assertEqual(source.metadata(data)['published_at'], data[key])


class InstagramGalleryTests(unittest.TestCase):
    def setUp(self):
        test_gallery.GalleryTests.setUp(self)
        config.set(('extractor',), 'filename', '{media_id}.{extension}')
        config.set(('extractor',), 'archive-format', '{media_id}')
        config.set(('extractor',), 'warn-videos', False)

    task = test_gallery.GalleryTests.task
    events = test_gallery.GalleryTests.events
    archive_count = test_gallery.GalleryTests.archive_count

    def test_carousel_original_order_missing_slot_and_mixed_files_survive_reversed_downloads(self):
        config.set(('extractor',), 'order-files', 'reverse')
        task = self.task(carousel(), fixture=InstagramFixture)
        self.assertEqual(task.run(), 0)
        events = self.events()
        sources = [e for e in events if e['kind'] == 'source.capture']
        files = [e for e in events if e['kind'] == 'file.completed']
        self.assertEqual([e['source']['attachment']['value'] for e in files], ['702', '701'])
        self.assertEqual([e['media_kind'] for e in files], ['scene', 'image'])
        for event in sources:
            self.assertEqual(event['post'], {'namespace': 'native:instagram', 'value': '123'})
            self.assertEqual(event['source']['instagram_media']['items'], [{'id': '701', 'kind': 'image'}, None, {'id': '702', 'kind': 'video'}])
            self.assertEqual(published(event['source'], 'instagram'), datetime.fromtimestamp(STAMP, timezone.utc))
        self.assertEqual(self.archive_count(), 2)

    def test_stories_and_highlights_use_individual_dates_and_the_same_stable_media_id(self):
        for highlight in (False, True):
            self.producer.window = SourceWindow({'since': datetime.fromtimestamp(STAMP, timezone.utc).isoformat(),
                                                'until': datetime.fromtimestamp(STAMP+1, timezone.utc).isoformat()})
            task = self.task(stories(highlight=highlight), fixture=InstagramFixture)
            self.assertEqual(task.run(), 0)
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        self.assertEqual(len(captures), 2)
        self.assertEqual([e['post']['value'] for e in captures], ['702', '702'])
        self.assertEqual({e['source']['instagram_media']['container']['type'] for e in captures}, {'story', 'highlight'})
        self.assertEqual(self.archive_count(), 1)

    def test_story_music_stickers_do_not_become_attachments_or_block_visual_files(self):
        record = stories()
        record['items'] = [media(701)]
        record['items'][0]['story_music_stickers'] = [{'music_asset_info': {
            'id': 888, 'title': 'Music', 'progressive_download_url': 'https://media.example/music.m4a'}}]
        task = self.task(record, fixture=InstagramFixture)
        self.assertEqual(task.run(), 0)
        files = [e for e in self.events() if e['kind'] == 'file.completed']
        self.assertEqual([e['source']['attachment']['value'] for e in files], ['701'])
        self.assertEqual([e['media_kind'] for e in files], ['image'])
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        self.assertEqual(captures[0]['source']['audio_title'], 'Music')
        self.assertEqual(captures[0]['source']['instagram_media']['items'], [{'id': '701', 'kind': 'image'}])

    def test_static_video_policy_retains_source_positions_with_the_downloaded_image_kind(self):
        config.set(('extractor',), 'static-videos', False)
        record = carousel()
        record['carousel_media'][2]['original_media_type'] = 1
        task = self.task(record, fixture=InstagramFixture)
        self.assertEqual(task.run(), 0)
        files = [e for e in self.events() if e['kind'] == 'file.completed']
        self.assertEqual([e['media_kind'] for e in files], ['image', 'image'])
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        self.assertEqual(captures[0]['source']['instagram_media']['items'][2], {'id': '702', 'kind': 'image'})

    def test_invalid_story_dates_fail_before_recording_captures_or_downloading(self):
        for stamp in (None, True, float('nan'), float('inf'), 'unknown'):
            with self.subTest(stamp=stamp):
                record = stories()
                record['items'] = [media(701, stamp=stamp)]
                task = self.task(record, fixture=InstagramFixture)
                task.download = lambda _: self.fail('Undated story was downloaded')
                self.assertNotEqual(task.run(), 0)
                self.assertEqual(self.events(), [])

    def test_profile_routes_undated_children_that_each_enforce_the_original_window(self):
        url = 'https://www.instagram.com/example/'
        self.lease.run['target_url'] = url
        config.set(('extractor', 'instagram'), 'include', 'stories,highlights,posts')
        self.producer.window = SourceWindow({'since': datetime.fromtimestamp(STAMP, timezone.utc).isoformat(),
                                            'until': datetime.fromtimestamp(STAMP+1, timezone.utc).isoformat()})

        def download(job, url):
            self.assertEqual(self.events()[-1]['kind'], 'source.capture')
            job.pathfmt.part_enable()
            with job.pathfmt.open('wb') as output:
                output.write(b'fixture ' + url.encode())
            return True

        with patch.object(InstagramExtractor, 'login', lambda _: None), \
             patch.object(InstagramExtractor, 'metadata', lambda _: {}), \
             patch('gallery_dl.extractor.instagram.InstagramPostsExtractor.posts', lambda _: iter([carousel()])), \
             patch('gallery_dl.extractor.instagram.InstagramStoriesExtractor.posts', lambda _: iter([stories()])), \
             patch('gallery_dl.extractor.instagram.InstagramHighlightsExtractor.posts', lambda _: iter([stories(highlight=True)])), \
             patch.object(NativeDownloadJob, 'download', download):
            task = NativeDownloadJob(extractor.find(url), producer=self.producer, lock_directory=self.locks)
            self.assertEqual(task.run(), 0)
        self.assertEqual({e['post']['value'] for e in self.events() if e['kind'] == 'source.capture'}, {'123', '702'})


if __name__ == '__main__':
    unittest.main()
