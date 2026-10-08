import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import unittest

from stash_ingest import source, ytdl_media
from stash_ingest.encoding import InvalidData
from stash_ingest.retention import retain
from stash_ingest.source_window import SourceWindow, published, validate_keywords


class YTDLMediaTests(unittest.TestCase):
    def test_shared_post_attachment_and_metadata_contract(self):
        path = Path(__file__).resolve().parents[3] / 'pkg/archive/testdata/captured-ytdl-v1.json'
        for case in json.loads(path.read_text())['cases']:
            with self.subTest(case=case['name']):
                original = copy.deepcopy(case['source'])
                for data in (original, retain(original)):
                    if case['post'] is None:
                        with self.assertRaises(InvalidData):
                            source.post(data)
                        continue
                    self.assertEqual(case['post'], source.post(data))
                    if case.get('metadata_error'):
                        with self.assertRaises(InvalidData):
                            source.metadata(data)
                    else:
                        self.assertEqual(case['metadata'], source.metadata(data))
                    if case['attachment'] is None:
                        with self.assertRaises(InvalidData):
                            source.attachment(data)
                    else:
                        self.assertEqual(case['attachment'], source.attachment(data))
                self.assertEqual(case['source'], original)

    def test_timestamp_window_ignores_collection_and_file_dates(self):
        data = {'category': 'ytdl', 'timestamp': 1790856000.123, 'upload_date': '20261001',
                'date': '2020-01-01T00:00:00Z', 'epoch': 2000000000}
        observed = published(data, 'ytdl')
        self.assertEqual(observed, datetime(2026, 10, 1, 12, 0, 0, 123000, timezone.utc))
        self.assertTrue(SourceWindow({'since': '2026-10-01T12:00:00.123Z',
                                      'until': '2026-10-01T12:00:00.124Z'}).contains(observed))
        self.assertFalse(SourceWindow({'since': None, 'until': '2026-10-01T12:00:00.123Z'}).contains(observed))
        for data in ({}, {'upload_date': '20261001'}, {'epoch': 1790856000}, {'date': '2026-10-01T12:00:00Z'}):
            with self.subTest(data=data), self.assertRaises(InvalidData):
                published(data, 'ytdl')

    def test_only_resolved_leaf_can_supply_membership(self):
        data = {'extractor_key': 'Youtube', 'id': 'video'}
        for extra in ({'_type': 'url'}, {'_type': 'url_transparent'}, {'entries': []}, {'_type': 'playlist'}):
            with self.subTest(extra=extra), self.assertRaises(InvalidData):
                ytdl_media.mark_leaf({**data, **extra})
        ytdl_media.mark_leaf(data)
        self.assertEqual(data['ytdl_media'], {'version': 1, 'type': 'video'})

    def test_keywords_cannot_relabel_source_identity_or_dates(self):
        class Extractor:
            category = 'ytdl'

            def config(self, key):
                return {field: 'override'} if key == 'keywords' else None

        for field in ('ytdl_media', 'extractor_key', 'webpage_url', 'timestamp', 'upload_date', 'uploader_id'):
            with self.subTest(field=field), self.assertRaises(InvalidData):
                validate_keywords(Extractor())


if __name__ == '__main__':
    unittest.main()
