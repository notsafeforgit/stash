import json
import unittest
from contextlib import closing
from unittest.mock import patch

from gallery_dl import config, exception
from gallery_dl.extractor.redgifs import RedgifsAPI
from requests import Response

from stash_ingest import deleted_media
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
import test_gallery
from test_gallery import LinkedRedgifsFixture
from test_producer import reddit_data


def reply(status=404, message=deleted_media.DELETED_MESSAGE):
    response = Response()
    response.status_code = status
    response.url = 'https://api.redgifs.com/v2/gifs/deletedclip'
    response._content = json.dumps({'error': {'description': message}}).encode()
    return response


class DeletedMediaTests(unittest.TestCase):
    setUp = test_gallery.GalleryTests.setUp
    task = test_gallery.GalleryTests.task
    events = test_gallery.GalleryTests.events
    archive_count = test_gallery.GalleryTests.archive_count

    def test_only_explicit_item_deletion_is_cached_not_access_or_server_failures(self):
        for status, message, expected in [(404, deleted_media.DELETED_MESSAGE, True),
                                          (410, deleted_media.DELETED_MESSAGE, True),
                                          (404, 'Not found', False),
                                          (403, deleted_media.DELETED_MESSAGE, False),
                                          (429, deleted_media.DELETED_MESSAGE, False),
                                          (500, deleted_media.DELETED_MESSAGE, False)]:
            with self.subTest(status=status, message=message):
                response = reply(status, message)
                error = exception.HttpError(response=response)
                self.assertEqual(deleted_media.response(error, response.url, 'deletedclip') is not None, expected)
                self.assertIsNone(deleted_media.response(error, response.url, 'otherclip'))
                self.assertIsNone(deleted_media.response(error, 'https://api.redgifs.com/v2/auth/temporary', 'deletedclip'))

    def test_explicit_deletion_message_locations_exclude_unrelated_payload_fields(self):
        for data, expected in [({'message': deleted_media.DELETED_MESSAGE}, True),
                               ({'description': deleted_media.DELETED_MESSAGE}, True),
                               ({'error': {'description': deleted_media.DELETED_MESSAGE}}, True),
                               ({'error': {'message': deleted_media.DELETED_MESSAGE}}, True),
                               ({'gif': {'description': deleted_media.DELETED_MESSAGE}}, False),
                               ({'error': deleted_media.DELETED_MESSAGE}, False),
                               ({'error': {'description': 'Item unavailable'}}, False),
                               ([], False)]:
            with self.subTest(data=data):
                response = reply(410)
                response._content = json.dumps(data).encode()
                error = exception.HttpError(response=response)
                self.assertEqual(deleted_media.response(error, response.url, 'deletedclip') is not None, expected)

    def test_deleted_child_records_failed_source_transfer_and_continues_with_next_post(self):
        dead = reddit_data('dead', url='https://www.redgifs.com/watch/deletedclip')
        next_post = reddit_data('next', url='https://www.redgifs.com/watch/availableclip')
        good = {'id': 'availableclip', 'createDate': 1790812800, 'gallery': None,
                'urls': {'hd': 'https://media.redgifs.com/AvailableClip.mp4'}}
        calls = []

        def request(session, method, url, **kwargs):
            calls.append(url)
            if url.endswith('/deletedclip'):
                return reply()
            response = reply(200)
            response._content = json.dumps({'gif': good}).encode()
            return response

        def download(child, url):
            child.pathfmt.part_enable()
            with child.pathfmt.open('wb') as output:
                output.write(b'complete video')
            return True

        config.set(('extractor', 'reddit'), 'parent-metadata', '_reddit')
        config.set(('extractor',), 'retries', 0)
        with patch.object(RedgifsAPI, '_auth', lambda _: 'fixture'), \
                patch('requests.sessions.Session.request', request), \
                patch.object(NativeDownloadJob, 'download', download):
            self.assertEqual(self.task(dead, next_post, fixture=LinkedRedgifsFixture).run(), 0)
            self.assertEqual(self.task(dead, next_post, fixture=LinkedRedgifsFixture).run(), 0)
        self.assertEqual(calls.count('https://api.redgifs.com/v2/gifs/deletedclip'), 1)
        self.assertEqual(self.archive_count(), 1, 'a deletion never enters the successful download archive')
        events = self.events(kinds=None)
        failures = [e for e in events if e['kind'] == 'attachment.download' and e['state'] == 'failed']
        self.assertEqual(len(failures), 2)
        self.assertTrue(all(e['attachment'] == {'namespace': 'native:redgifs', 'value': 'deletedclip'} for e in failures))
        captures = [e for e in events if e['kind'] == 'source.capture' and 'native_media_status' in e['source']]
        self.assertEqual([e['post']['value'] for e in captures], ['postdead', 'postdead'])
        self.assertEqual(captures[0]['source']['native_media_status'], captures[1]['source']['native_media_status'])
        self.assertEqual(len([e for e in events if e['kind'] == 'file.completed']), 2)

    def test_deleted_cache_survives_restart_and_expires_without_marking_downloaded(self):
        status = deleted_media.response(exception.HttpError(response=reply()), reply().url, 'deletedclip')
        self.box.clock = lambda: 1000
        deleted_media.remember(self.box, status)
        with closing(Outbox(self.box.path, self.box.endpoint, self.box.producer, clock=lambda: 1001)) as reopened:
            self.assertEqual(deleted_media.cached(reopened, 'deletedclip'), status)
            self.assertIsNone(deleted_media.cached(reopened, 'otherclip'))
            reopened.clock = lambda: 1000 + deleted_media.RECHECK_SECONDS
            self.assertIsNone(deleted_media.cached(reopened, 'deletedclip'))

    def test_deleted_without_media_does_not_count_as_an_archived_boundary(self):
        data = reddit_data(url='https://www.redgifs.com/watch/deletedclip')
        status = deleted_media.response(exception.HttpError(response=reply()), reply().url, 'deletedclip')
        deleted_media.remember(self.box, status)
        self.assertEqual(self.task(data, fixture=LinkedRedgifsFixture).run(), 0)
        self.assertTrue(self.producer.routine_post_new)
        self.assertEqual(self.producer.routine_post_items, 0)
        self.assertEqual(self.events(kinds=('file.completed',)), [])

    def test_cached_deletion_still_downloads_reddit_fallback(self):
        fallback = 'https://preview.redd.it/fallback.jpg?width=900'
        data = reddit_data(url='https://www.redgifs.com/watch/deletedclip',
                           preview={'images': [{'source': {'url': fallback}}]}, _fallback=[fallback])
        status = deleted_media.response(exception.HttpError(response=reply()), reply().url, 'deletedclip')
        deleted_media.remember(self.box, status)
        self.assertEqual(self.task(data, fixture=LinkedRedgifsFixture).run(), 0)
        self.assertEqual(len(self.events(kinds=('file.completed',))), 1)
        self.assertTrue(self.producer.routine_post_new, 'a preview does not prove the deleted original was downloaded')
