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


def post(number, *, items=None, pinned=False):
    value = {**media(number), 'user': {'pk': '456', 'username': 'example'},
             'caption': {'text': 'Original caption'}, 'timeline_pinned_user_ids': ['456'] if pinned else []}
    if items is not None:
        value.update(media_type=8, carousel_media=items)
    return value


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
        for index, record in enumerate(self.records):
            if index % 2 == 0:
                self.visited.append('page ' + str(index // 2))
            yield copy.deepcopy(record)


class InstagramStoriesFixture(InstagramFixture):
    subcategory = 'stories'


class InstagramHighlightsFixture(InstagramFixture):
    subcategory = 'highlights'


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

    def test_routine_posts_stop_before_next_page_after_complete_known_album(self):
        known = post(123, items=[media(701), media(702, video=True)])
        self.assertEqual(self.task(known, fixture=InstagramFixture).run(), 0)
        self.producer.scan_mode = 'routine'
        task = self.task(post(124), known, post(122), fixture=InstagramFixture)
        self.assertEqual(task.run(), 0)
        self.assertEqual(task.extractor.visited, ['page 0'])
        self.assertEqual(self.archive_count(), 3)

    def test_pinned_posts_and_partial_albums_cannot_hide_new_attachments(self):
        pinned = post(123, pinned=True)
        first = [media(n) for n in range(701, 705)]
        known = post(125)
        self.assertEqual(self.task(pinned, post(124, items=first), known, fixture=InstagramFixture).run(), 0)
        self.producer.scan_mode = 'routine'
        config.set(('extractor',), 'skip', 'abort:4')
        task = self.task(pinned, post(124, items=[*first, media(705, video=True)]), known, post(126), fixture=InstagramFixture)
        self.assertEqual(task.run(), 0)
        self.assertEqual(task.extractor.visited, ['page 0', 'page 1'])
        self.assertEqual(self.archive_count(), 7)
        self.assertTrue(any(e['kind'] == 'file.completed' and e['source']['attachment']['value'] == '705' for e in self.events()))
        self.assertFalse(any(e['kind'] == 'source.capture' and e['post']['value'] == '126' for e in self.events()))

    def test_missing_or_filtered_album_items_do_not_establish_known_post_boundary(self):
        known = post(123, items=[media(701), media(702)])
        self.assertEqual(self.task(known, fixture=InstagramFixture).run(), 0)
        self.producer.scan_mode = 'routine'
        config.set(('extractor',), 'image-filter', 'media_id != "703"')
        task = self.task(post(123, items=[media(701), None, media(702)]),
                         post(124, items=[media(701), media(702), media(703)]), post(125), fixture=InstagramFixture)
        self.assertEqual(task.run(), 0)
        self.assertEqual(task.extractor.visited, ['page 0', 'page 1'])
        self.assertEqual(self.archive_count(), 3)

    def test_initial_and_recovery_keep_traversing_known_posts(self):
        known = post(123)
        self.assertEqual(self.task(known, fixture=InstagramFixture).run(), 0)
        for mode in ('initial', 'recovery'):
            self.producer.scan_mode = mode
            task = self.task(known, post(124), post(125), fixture=InstagramFixture)
            self.assertEqual(task.run(), 0)
            self.assertEqual(task.extractor.visited, ['page 0', 'page 1'])

    def test_routine_story_and_highlight_checks_keep_newer_items_after_known_ones(self):
        for highlight in (False, True):
            with self.subTest(highlight=highlight):
                fixture = InstagramHighlightsFixture if highlight else InstagramStoriesFixture
                record = stories(highlight=highlight)
                record['items'] = [media(701)]
                self.assertEqual(self.task(record, fixture=fixture).run(), 0)
                self.producer.scan_mode = 'routine'
                task = self.task(stories(highlight=highlight), fixture=fixture)
                self.assertEqual(task.run(), 0)
                captured = [e['post']['value'] for e in self.events() if e['kind'] == 'source.capture']
                self.assertIn('702', captured)
                self.assertIn('703', captured)

    def test_empty_story_or_highlight_container_does_not_block_later_media(self):
        for highlight in (False, True):
            with self.subTest(highlight=highlight):
                empty = stories(highlight=highlight)
                empty['items'] = []
                task = self.task(empty, stories(highlight=highlight), fixture=InstagramFixture)
                self.assertEqual(task.run(), 0)
                self.assertIsNone(self.producer.failure_code)
        self.assertEqual(self.archive_count(), 3)
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        self.assertEqual({e['post']['value'] for e in captures}, {'701', '702', '703'})

    def test_single_media_with_empty_carousel_field_is_a_complete_single_post(self):
        for empty in (None, []):
            for video in (False, True):
                with self.subTest(carousel=empty, video=video):
                    record = {**post(123), **media(123, video=video), 'carousel_media': empty}
                    self.producer.scan_mode = 'initial'
                    self.assertEqual(self.task(record, fixture=InstagramFixture).run(), 0)
                    self.producer.scan_mode = 'routine'
                    task = self.task(record, post(124), fixture=InstagramFixture)
                    self.assertEqual(task.run(), 0)
                    captures = [e for e in self.events() if e['kind'] == 'source.capture']
                    self.assertEqual({e['post']['value'] for e in captures}, {'123'})
                    self.assertTrue(all(not e['source']['instagram_media']['album'] for e in captures))

    def test_missing_or_empty_actual_carousel_remains_an_error(self):
        for empty in (None, []):
            with self.subTest(carousel=empty):
                record = {**post(123), 'media_type': 8, 'carousel_media': empty}
                task = self.task(record, fixture=InstagramFixture)
                self.assertNotEqual(task.run(), 0)
                self.assertEqual(self.producer.failure_code, 'source_rejected')
        self.assertEqual(self.events(), [])

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
