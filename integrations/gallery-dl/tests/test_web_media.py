"""Source membership plus actual pinned Tumblr/Chevereto/LeakGallery jobs."""

import copy
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from gallery_dl import config
from gallery_dl.extractor.tumblr import TumblrUserExtractor
from gallery_dl.extractor.chevereto import CheveretoFileExtractor
from gallery_dl.extractor.leakgallery import LeakgalleryPostExtractor, LeakgalleryUserExtractor

from stash_ingest import source, web_media
from stash_ingest.encoding import InvalidData
from stash_ingest.retention import retain
from stash_ingest.runs import SourceFailure
from stash_ingest.source_window import SourceWindow
import test_gallery


class TumblrFixture(TumblrUserExtractor):
    def posts(self):
        return iter(copy.deepcopy(self.records))


class WebMediaContractTests(unittest.TestCase):
    def test_shared_go_contract_before_and_after_retention(self):
        path = Path(__file__).resolve().parents[3] / 'pkg/archive/testdata/captured-web-media-v1.json'
        for case in json.loads(path.read_text())['cases']:
            for data in (case['source'], retain(case['source'])):
                with self.subTest(name=case['name']):
                    if case['error']:
                        with self.assertRaises(InvalidData):
                            web_media.manifest(data, data['category'])
                        continue
                    marker = web_media.manifest(data, data['category'])
                    self.assertEqual(source.post(data), case['post'])
                    self.assertEqual([{'namespace': case['post']['namespace'], 'value': web_media.reference(i['url'])}
                                      for i in marker['items']], case['items'])
                    self.assertEqual(source.attachment(data), case['items'][-1])

    def test_missing_or_foreign_selected_file_and_zero_date(self):
        data = {'category': 'jpgfish', 'id': 'AbC', 'date': '0001-01-01T00:00:00Z'}
        self.assertNotIn('published_at', source.metadata(data))
        with self.assertRaises(InvalidData):
            source.attachment(data)
        data['web_media'] = {'version': 1, 'post_id': 'AbC', 'complete': True, 'album': False,
                             'items': [{'url': 'https://cdn.example/a.jpg', 'kind': 'image'}]}
        data['web_media_url'] = 'https://cdn.example/b.jpg'
        with self.assertRaises(InvalidData):
            source.attachment(data)


class WebMediaGalleryTests(unittest.TestCase):
    def setUp(self):
        test_gallery.GalleryTests.setUp(self)
        self.producer.window = SourceWindow({'since': None, 'until': '2026-10-08T00:00:00Z', 'basis': 'traversal'})
        config.set(('extractor',), 'filename', '{filename}.{extension}')

    task = test_gallery.GalleryTests.task
    events = test_gallery.GalleryTests.events

    def test_tumblr_two_photos_share_source_membership_and_skip_audio(self):
        self.lease.run['target_url'] = 'https://example.tumblr.com/'
        photos = [{'original_size': {'url': f'https://64.media.tumblr.com/{name}.jpg', 'width': 1200, 'height': 1200},
                   'alt_sizes': [{'url': f'https://64.media.tumblr.com/{name}_small.jpg', 'width': 100, 'height': 100}]}
                  for name in ('first', 'second')]
        data = {'id': 123, 'id_string': '123', 'type': 'photo', 'timestamp': 1790856000,
                'blog_name': 'example', 'blog': {'uuid': 'blog-one', 'name': 'example'},
                'photos': photos, 'caption': 'Album caption'}
        task = self.task(data, fixture=TumblrFixture)
        self.assertEqual(task.run(), 0)
        captures = self.events(kinds=('source.capture',))
        files = self.events(kinds=('file.completed',))
        self.assertEqual(len(captures), 2)
        self.assertEqual(len(files), 2)
        one, two = [event['source'] for event in captures]
        self.assertEqual(one['web_media'], two['web_media'])
        self.assertTrue(one['web_media']['complete'])
        self.assertTrue(one['web_media']['album'])
        self.assertEqual([item['url'] for item in one['web_media']['items']],
                         [photo['original_size']['url'] for photo in photos])
        self.assertNotEqual(source.attachment(one), source.attachment(two))
        self.assertEqual(captures[0]['metadata']['original_text'], 'Album caption')
        audio = {**data, 'id': 124, 'id_string': '124', 'type': 'audio', 'audio_url': 'https://a.tumblr.com/audio.mp3'}
        audio.pop('photos')
        task = self.task(audio, fixture=TumblrFixture)
        self.assertEqual(task.run(), 0)
        self.assertEqual(len(self.events(kinds=('file.completed',))), 2)
        excluded = [event for event in self.events(kinds=('attachment.download',)) if event['state'] == 'excluded']
        self.assertEqual(len(excluded), 1)

    def test_chevereto_file_does_not_invent_album_or_publisher_from_folder(self):
        self.lease.run['target_url'] = 'https://jpg7.cr/img/Photo.AbCd'
        page = '''<meta property="og:type" content="image"><meta property="og:title" content="Photo">
        <meta property="og:image" content="https://cdn.example/photo.jpg">
        Added to <a href="/album/Group.XYZ">Group</a><span title="2026-10-01T12:00:00+00:00">
        username: "actual-publisher"'''
        with patch.object(CheveretoFileExtractor, 'request', return_value=SimpleNamespace(text=page)):
            task = self.task(fixture=CheveretoFileExtractor)
            self.assertEqual(task.run(), 0)
        captures = self.events(kinds=('source.capture',))
        self.assertEqual(len(captures), 1)
        data = captures[0]['source']
        self.assertEqual(source.post(data), {'namespace': 'native:jpgfish', 'value': 'AbCd'})
        self.assertFalse(data['web_media']['album'])
        self.assertTrue(data['web_media']['complete'])
        self.assertEqual(data['album'], 'Group')

    def test_leakgallery_html_files_share_a_partial_manifest(self):
        self.lease.run['target_url'] = 'https://leakgallery.com/creator/123'
        urls = ['https://cdn.leakgallery.com/content/creator/watermark_one.jpg',
                'https://cdn.leakgallery.com/content/creator/watermark_two.mp4']
        page = ''.join(f'<a href="{url}">' for url in urls)
        with patch.object(LeakgalleryPostExtractor, 'request', return_value=SimpleNamespace(text=page)):
            task = self.task(fixture=LeakgalleryPostExtractor)
            self.assertEqual(task.run(), 0)
        captures = self.events(kinds=('source.capture',))
        self.assertEqual(len(captures), 2)
        first, second = [event['source']['web_media'] for event in captures]
        self.assertEqual(first, second)
        self.assertFalse(first['complete'])
        self.assertFalse(first['album'])
        self.assertEqual({item['url'] for item in first['items']}, set(urls))

    def test_leakgallery_failed_next_page_cannot_complete_the_scan(self):
        self.lease.run['target_url'] = 'https://leakgallery.com/creator'
        records = [{'id': 123, 'file_path': 'content/creator/watermark_one.jpg'}]
        with patch.object(LeakgalleryUserExtractor, 'request_json', side_effect=[records, RuntimeError('fixture page failure')]):
            task = self.task(fixture=LeakgalleryUserExtractor)
            with self.assertRaises(SourceFailure):
                task.run()
        self.assertEqual(self.producer.source_failure.code, 'extraction_failed')
        self.assertEqual(len(self.events(kinds=('file.completed',))), 1)
