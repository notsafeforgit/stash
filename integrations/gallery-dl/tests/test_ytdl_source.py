"""Exercise the pinned gallery-dl/yt-dlp playlist traversal without live sites."""

from io import BytesIO
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from gallery_dl import config, ytdl
from gallery_dl.extractor.common import Message
from gallery_dl.extractor.ytdl import YoutubeDLExtractor
from yt_dlp import YoutubeDL
from yt_dlp.extractor.common import InfoExtractor
from yt_dlp.networking import Response
from yt_dlp.networking.exceptions import HTTPError, TransportError
from yt_dlp.utils import ExtractorError

from stash_ingest.filesystem import Root
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.producer import Producer
from stash_ingest.runs import SourceFailure, SourcePaused, SourceTurnComplete
from stash_ingest.ytdl_source import install
from stash_ingest import source
from stash_ingest.retention import retain
from helpers import PRODUCER
from test_producer import LeaseFixture


class NativeRootFixtureIE(InfoExtractor):
    _VALID_URL = r'https://fixture\.invalid/(?P<id>account)'

    def _real_extract(self, url):
        self._download_json(url, 'account')
        return self.playlist_result([
            self.url_result('https://fixture.invalid/video/one', ie=NativeVideoFixtureIE),
            self.url_result('https://fixture.invalid/video/two', ie=NativeVideoFixtureIE),
        ], playlist_id='account')


class NativeVideoFixtureIE(InfoExtractor):
    _VALID_URL = r'https://fixture\.invalid/video/(?P<id>one|two)'

    def _real_extract(self, url):
        data = self._download_json(url, self._match_id(url))
        if data.get('fail'):
            raise ExtractorError('Fixture unavailable', expected=True)
        if data.get('optional_probe'):
            self._download_json('https://fixture.invalid/optional', self._match_id(url), fatal=False)
        return {'id': self._match_id(url), 'title': 'Fixture', 'ext': 'mp4',
                'url': 'https://media.invalid/' + self._match_id(url) + '.mp4'}


class GenericIE(InfoExtractor):
    # Resolve the KVS leaf without a live CDN; the outer ThisVid extractor and
    # yt-dlp's transparent delegation/metadata merging still run unmodified.
    _VALID_URL = r'https://thisvid\.com/videos/(?P<id>fixture)/'

    def _real_extract(self, url):
        return {'id': '3533241', 'title': 'Inner title', 'ext': 'mp4',
                'url': 'https://media.invalid/3533241.mp4'}


class YTDLSourceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        directory = Path(temporary.name)
        media = directory / 'media'
        media.mkdir()
        self.locks = directory / 'locks'
        self.locks.mkdir()
        self.lease = LeaseFixture()
        self.box = Outbox(directory / 'outbox.sqlite', self.lease.client.endpoint, PRODUCER)
        self.addCleanup(self.box.close)
        self.producer = Producer(self.box, self.lease, Root(media, Root.probe(media)), extractor_version='fixture')
        self.requests = []
        self.responses = {}
        self.reservations = []

        def reserve(url):
            self.lease.check()
            self.assertTrue(url.startswith('https://'), 'source reservations use real HTTP URLs, not the ytdl: selector')
            self.reservations.append(url)
            return 'host:fixture.invalid'

        self.lease.reserve_source = reserve
        config.clear()
        self.addCleanup(config.clear)
        config.set(('extractor', 'ytdl'), 'module', 'yt_dlp')
        config.set(('extractor', 'ytdl'), 'logging', False)
        config.set(('extractor', 'ytdl'), 'raw-options', {'quiet': True, 'no_warnings': True, 'ignoreerrors': True})
        original = ytdl.construct_YoutubeDL

        def factory(*args, **kwargs):
            client = original(*args, **kwargs)
            client.add_info_extractor(NativeRootFixtureIE())
            client.add_info_extractor(NativeVideoFixtureIE())
            client.add_info_extractor(GenericIE())
            return client

        def request(client, req):
            url = req if isinstance(req, str) else req.url
            self.requests.append(url)
            result = self.responses.get(url, {})
            if isinstance(result, Exception):
                raise result
            body = result if isinstance(result, str) else json.dumps(result)
            return Response(BytesIO(body.encode()), url, {'Content-Type': 'text/html' if isinstance(result, str) else 'application/json'})

        self.construct = factory
        factory_patch = patch('gallery_dl.ytdl.construct_YoutubeDL', factory)
        request_patch = patch.object(YoutubeDL, 'urlopen', request)
        factory_patch.start()
        request_patch.start()
        self.addCleanup(factory_patch.stop)
        self.addCleanup(request_patch.stop)
        self.extractor = YoutubeDLExtractor.from_url('ytdl:https://fixture.invalid/account')
        self.extractor.ytdl_ie_key = 'NativeRootFixture'
        self.extractor.initialize()
        install(self.extractor, self.producer)

    def test_real_playlist_checks_each_page_and_keeps_leaf_identity(self):
        records = list(self.extractor.items())
        self.assertEqual([item[0] for item in records], [Message.Directory, Message.Url] * 2)
        self.assertEqual([item[2]['id'] for item in records], ['one', 'one', 'two', 'two'])
        self.assertEqual(self.requests, ['https://fixture.invalid/account',
                                        'https://fixture.invalid/video/one', 'https://fixture.invalid/video/two'])
        self.assertEqual(self.reservations, self.requests)
        posts = [source.post(retain(dict(item[2], category='ytdl'))) for item in records[::2]]
        self.assertEqual(posts, [{'namespace': 'ytdl:nativevideofixture', 'value': 'one'},
                                 {'namespace': 'ytdl:nativevideofixture', 'value': 'two'}])
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)
        self.assertIsNone(self.producer.source_failure)

    def test_current_file_keeps_working_after_lease_loss_but_next_source_stops(self):
        records = self.extractor.items()
        self.addCleanup(records.close)
        next(records)
        _, _, data = next(records)
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)
        self.lease.active = False
        with data['_ytdl_instance'].urlopen('https://media.invalid/one.mp4') as response:
            self.assertEqual(response.status, 200)
        with self.assertRaises(SourcePaused):
            next(records)
        self.assertNotIn('https://fixture.invalid/video/two', self.requests)
        self.assertNotIn('https://media.invalid/one.mp4', self.reservations)
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)

    def test_direct_thisvid_delegation_is_resolved_before_the_first_file_message(self):
        target = 'https://thisvid.com/videos/fixture/'
        self.responses[target] = '''<html><title>Video: Outer title - ThisVid.com</title>
        <span>Added by: </span><a class="author" href="https://thisvid.com/members/150629/">Publisher</a></html>'''
        extractor = YoutubeDLExtractor.from_url('ytdl:' + target)
        extractor.initialize()
        install(extractor, self.producer)
        records = list(extractor.items())
        self.assertEqual([r[0] for r in records], [Message.Directory, Message.Url])
        data = records[0][2]
        self.assertEqual(data['id'], '3533241')
        self.assertEqual(data['extractor_key'], 'Generic')
        self.assertEqual(data['uploader_id'], '150629')
        self.assertEqual(data['title'], 'Outer title')
        self.assertEqual(data['webpage_url'], target)
        self.assertNotIn('timestamp', data, 'undated metadata must stay undated')
        kept = retain(dict(data, category='ytdl'))
        self.assertEqual(source.post(kept), source.attachment(kept))
        self.assertEqual(source.post(kept)['namespace'], 'ytdl:thisvid.com')
        self.assertNotIn('published_at', source.metadata(kept))
        self.assertEqual(self.requests, [target], 'source resolution never downloads media')
        self.assertEqual(self.reservations, [target])
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)

    def test_rate_limit_cannot_be_swallowed_by_ignoreerrors_or_the_bridge(self):
        self.responses['https://fixture.invalid/video/one'] = HTTPError(
            Response(BytesIO(b''), 'https://fixture.invalid/video/one', {}, status=429))
        with self.assertRaises(SourceFailure) as caught:
            list(self.extractor.items())
        self.assertEqual(caught.exception.code, 'rate_limited')
        self.assertIs(caught.exception, self.producer.source_failure)
        self.assertNotIn('https://fixture.invalid/video/two', self.requests)
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)

    def test_native_job_installs_the_guard_before_starting_the_bridge(self):
        self.responses['https://fixture.invalid/account'] = HTTPError(
            Response(BytesIO(b''), 'https://fixture.invalid/account', {}, status=429))
        extractor = YoutubeDLExtractor.from_url('ytdl:https://fixture.invalid/account')
        extractor.url = self.lease.run['target_url']
        extractor.ytdl_ie_key = 'NativeRootFixture'
        job = NativeDownloadJob(extractor, producer=self.producer, lock_directory=self.locks)
        with self.assertRaises(SourceFailure) as caught:
            job.run()
        self.assertEqual(caught.exception.code, 'rate_limited')
        self.assertEqual(self.requests, ['https://fixture.invalid/account'])
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)

    def test_optional_http_probe_can_fall_back_without_failing_the_playlist(self):
        self.responses['https://fixture.invalid/video/one'] = {'optional_probe': True}
        self.responses['https://fixture.invalid/optional'] = HTTPError(
            Response(BytesIO(b''), 'https://fixture.invalid/optional', {}, status=404))
        self.assertEqual(len(list(self.extractor.items())), 4)
        self.assertIsNone(self.producer.source_failure)
        self.assertEqual(self.reservations, self.requests)

    def test_cancelling_between_messages_restores_factory_and_skips_later_source(self):
        records = self.extractor.items()
        next(records)
        records.close()
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)
        self.assertNotIn('https://fixture.invalid/video/two', self.requests)

    def test_fatal_extractor_error_cannot_make_a_partial_playlist_succeed(self):
        self.responses['https://fixture.invalid/video/two'] = {'fail': True}
        records = self.extractor.items()
        next(records)
        next(records)
        with self.assertRaises(SourceFailure) as caught:
            next(records)
        self.assertEqual(caught.exception.code, 'extraction_failed')
        self.assertIs(ytdl.construct_YoutubeDL, self.construct)

    def test_timeout_retains_native_failure_and_stops_before_more_requests(self):
        self.responses['https://fixture.invalid/account'] = TransportError(cause=TimeoutError())
        with self.assertRaises(SourceFailure) as caught:
            list(self.extractor.items())
        self.assertEqual(caught.exception.code, 'timeout')
        self.assertEqual(self.requests, ['https://fixture.invalid/account'])

    def test_turn_yield_and_lease_loss_are_restored_after_bridge_error_handling(self):
        for failure in (SourceTurnComplete('turn complete'), SourcePaused('lease lost')):
            with self.subTest(failure=type(failure).__name__):
                self.requests.clear()
                self.reservations.clear()
                self.extractor = YoutubeDLExtractor.from_url('ytdl:https://fixture.invalid/account')
                self.extractor.ytdl_ie_key = 'NativeRootFixture'
                self.extractor.initialize()
                install(self.extractor, self.producer)

                def check():
                    if self.requests:
                        raise failure

                with patch.object(self.lease, 'check_turn', check), self.assertRaises(type(failure)) as caught:
                    list(self.extractor.items())
                self.assertIs(caught.exception, failure)
                self.assertEqual(self.requests, ['https://fixture.invalid/account'])
                self.assertIs(ytdl.construct_YoutubeDL, self.construct)


if __name__ == '__main__':
    unittest.main()
