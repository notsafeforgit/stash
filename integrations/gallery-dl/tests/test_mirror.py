"""Original mirror membership and pinned download lifecycle, without network."""

import copy
from contextlib import closing
import json
from pathlib import Path
import sqlite3
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from gallery_dl import config
from gallery_dl.extractor.kemono import KemonoPostExtractor
from stash_ingest import mirror, source
from stash_ingest.encoding import InvalidData
from stash_ingest.producer import Producer
from stash_ingest.retention import retain
from stash_ingest.scan_resume import legacy_cursor
from stash_ingest.source_window import SourceWindow
import test_gallery

IMAGE = '/aa/aa/' + 'a' * 64 + '.jpg'
VIDEO = '/bb/bb/' + 'b' * 64 + '.mp4'
INLINE = '/cc/cc/' + 'c' * 64 + '.png'


def post(**changes):
    return {'id': '123', 'service': 'onlyfans', 'user': '456', 'title': 'Original title',
            'content': 'Original caption', 'published': '2026-10-01T12:00:00.123456',
            'added': '2026-10-04T12:00:00', 'file': {'path': IMAGE, 'name': 'image.jpg', 'id': 'primary'},
            'attachments': [], **changes}


class MirrorFixture(KemonoPostExtractor):
    def posts(self):
        return iter(copy.deepcopy(self.records))


class MirrorContractTests(unittest.TestCase):
    def test_shared_go_membership_contract_and_retention(self):
        path = Path(__file__).resolve().parents[3] / 'pkg/archive/testdata/captured-mirror-media-v1.json'
        for case in json.loads(path.read_text())['cases']:
            for value in (case['source'], retain(case['source'])):
                with self.subTest(name=case['name']):
                    if case['error']:
                        with self.assertRaises(InvalidData):
                            mirror.manifest(value)
                        continue
                    evidence = mirror.manifest(value)
                    if case['items'] is None:
                        self.assertIsNone(evidence)
                    else:
                        self.assertEqual(evidence['items'], case['items'])
                        self.assertEqual(source.attachment(value), case['attachment'])


class MirrorGalleryTests(unittest.TestCase):
    def setUp(self):
        test_gallery.GalleryTests.setUp(self)
        self.lease.run['target_url'] = 'https://coomer.st/onlyfans/user/456/post/123'
        config.set(('extractor',), 'filename', '{id}_{num}.{extension}')
        config.set(('extractor',), 'archive-format', '{category}_{service}_{user}_{id}_{num}')
        config.set(('extractor',), 'original', True)
        profile = patch('gallery_dl.extractor.kemono.KemonoAPI.creator_profile',
                        return_value={'id': '456', 'name': 'Example'})
        profile.start()
        self.addCleanup(profile.stop)

    task = test_gallery.GalleryTests.task
    events = test_gallery.GalleryTests.events

    def archive_count(self):
        with closing(sqlite3.connect(self.directory / 'downloads.sqlite')) as db:
            return db.execute('SELECT count(*) FROM archive').fetchone()[0]

    def test_original_membership_survives_download_order_filtering_and_duplicate_primary(self):
        for category, service in (('coomer', 'onlyfans'), ('kemono', 'patreon')):
            with self.subTest(category=category):
                self.lease.run['target_url'] = f'https://{category}.st/{service}/user/456/post/123'
                record = post(service=service, attachments=[{'path': VIDEO, 'name': 'video.mp4'}, None,
                                                           {'path': IMAGE, 'name': 'image.jpg'}],
                              content=f'<img src="{INLINE}"><img src="{IMAGE}">')
                start = len(self.events())
                task = self.task(record, fixture=MirrorFixture)
                self.assertEqual(task.run(), 0)
                events = self.events()[start:]
                captures = [e for e in events if e['kind'] == 'source.capture']
                files = [e for e in events if e['kind'] == 'file.completed']
                expected = [IMAGE, VIDEO, INLINE] if category == 'coomer' else [VIDEO, IMAGE, INLINE]
                self.assertEqual([e['source']['attachment']['value'] for e in files], expected)
                for capture in captures:
                    self.assertEqual(capture['source']['mirror_media']['items'], [
                        {'id': VIDEO, 'kind': 'video'}, None, {'id': IMAGE, 'kind': 'image'}, {'id': INLINE, 'kind': 'image'}])
                    self.assertEqual(capture['source']['attachments'], record['attachments'])
                    self.assertEqual(capture['source']['file'], record['file'])
                    self.assertEqual(capture['post'], {'namespace': f'mirror:{category}:{service}', 'value': '456/123'})
        self.assertEqual(self.archive_count(), 6)

    def test_configured_file_selection_does_not_shrink_source_membership(self):
        config.set(('extractor',), 'files', ['attachments'])
        record = post(attachments=[{'path': VIDEO, 'name': 'video.mp4'}])
        self.assertEqual(self.task(record, fixture=MirrorFixture).run(), 0)
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        self.assertEqual(captures[0]['source']['mirror_media']['items'], [
            {'id': IMAGE, 'kind': 'image'}, {'id': VIDEO, 'kind': 'video'}])
        self.assertEqual([e['source']['attachment']['value'] for e in self.events() if e['kind'] == 'file.completed'], [VIDEO])

    def test_source_window_uses_fractional_publication_not_mirror_import_time(self):
        self.producer.window = SourceWindow({'since': '2026-10-01T12:00:00.123Z', 'until': '2026-10-01T12:00:00.124Z'})
        records = [post(id='old', published='2026-09-01T00:00:00'), post(),
                   post(id='until', published='2026-10-01T12:00:00.124')]
        self.assertEqual(self.task(*records, fixture=MirrorFixture).run(), 0)
        self.assertEqual([e['post']['value'] for e in self.events() if e['kind'] == 'source.capture'], ['456/123'])
        self.assertEqual(self.archive_count(), 1)

    def test_missing_publication_does_not_fall_back_to_import_time(self):
        task = self.task(post(published=None), fixture=MirrorFixture)
        task.download = lambda _: self.fail('Post without a publication time was downloaded')
        self.assertNotEqual(task.run(), 0)
        self.assertEqual(self.events(), [])

    def test_unsupported_files_keep_evidence_without_download_or_archive_acknowledgement(self):
        record = post(file={'path': '/dd/dd/' + 'd' * 64 + '.mp3', 'name': 'audio.mp3'},
                      attachments=[{'path': IMAGE, 'name': 'image.jpg'},
                                   {'path': '/ee/ee/' + 'e' * 64 + '.zip', 'name': 'archive.zip'}])
        task = self.task(record, fixture=MirrorFixture)
        original = task.download
        def download(url):
            self.assertTrue(url.endswith('.jpg'))
            return original(url)
        task.download = download
        self.assertEqual(task.run(), 0)
        captures = [e for e in self.events() if e['kind'] == 'source.capture']
        self.assertEqual(len(captures), 3)
        self.assertEqual(sum(bool(e['source'].get('native_file_exclusion')) for e in captures), 2)
        reports = self.events(kinds=('attachment.download',))
        excluded = [e for e in reports if e['state'] == 'excluded']
        self.assertEqual(len(excluded), 2)
        self.assertTrue(all(e['reason_code'] == 'unsupported_media' and 'file_event_uuid' not in e for e in excluded))
        self.assertEqual(sum(e['state'] == 'downloaded' for e in reports), 1)
        self.assertEqual(self.archive_count(), 1)
        self.assertEqual((self.producer.items_seen, self.producer.files_completed), (3, 1))

    def test_preview_configuration_is_rejected_before_source_access(self):
        config.set(('extractor',), 'original', False)
        task = self.task(post(), fixture=MirrorFixture)
        with self.assertRaisesRegex(InvalidData, 'original=true'):
            task.run()
        self.assertEqual(self.events(), [])

    def test_native_and_legacy_checkpoints_can_resume_past_an_excluded_file(self):
        config.set(('extractor',), 'skip', 'abort:4')
        record = post(file={'path': '/audio.mp3', 'name': 'audio.mp3'},
                      attachments=[{'path': IMAGE, 'name': 'image.jpg'}])
        task = self.task(record, fixture=MirrorFixture)
        self.assertEqual(task.run(), 0)
        cursor = self.lease.checkpoints[0][2]
        old_cursor = legacy_cursor(task, SimpleNamespace(kwdict={
            'category': 'coomer', 'service': 'onlyfans', 'user': '456', 'id': '123', 'num': 1}, filename='audio.mp3'))
        for saved in (cursor, old_cursor):
            self.lease.run['progress'] = {'items_seen': 1, 'files_completed': 0, 'cursor': saved}
            self.producer = Producer(self.box, self.lease, self.root, extractor_version='1.32.15-dev')
            replay = self.task(record, fixture=MirrorFixture)
            self.assertEqual(replay.run(), 0)
            self.assertEqual(self.producer.resume_cursor, '')
            self.assertEqual((self.producer.items_seen, self.producer.files_completed), (2, 1))
        self.assertEqual(self.archive_count(), 1)


if __name__ == '__main__':
    unittest.main()
