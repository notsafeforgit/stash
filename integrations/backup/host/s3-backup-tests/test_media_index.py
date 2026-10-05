"""Cold upload receipts, restart behavior and actual request-count bounds."""

from contextlib import closing
from datetime import timedelta
import hashlib
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeColdS3, S3Error
from media_index import MediaIndex
import media_objects as media


class MediaIndexTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cloud = self.root / 'objects'
        self.cloud.mkdir()
        self.client = FakeColdS3(self.cloud)
        self.database = self.root / 'ledger.sqlite3'
        self.store = {'bucket': 'test-archive', 'prefix': 'cold/'}

    def seed(self, key, body):
        path = self.cloud / self.store['prefix'] / key
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        return path

    def open(self):
        return MediaIndex(self.database, self.client, self.store)

    def test_thousand_unchanged_cold_objects_use_two_lists_after_restart(self):
        files = {}
        for number in range(1003):
            body = str(number).encode()
            key = media.object_key(hashlib.sha256(body).hexdigest())
            files[key] = self.seed(key, body)
        with closing(self.open()) as index:
            for key, path in files.items():
                record = index.get(key, local=media.hash_file(path))
                self.assertEqual(record['sha256'], path.name)
        self.assertEqual(sum(op == 'head' for op, _ in self.client.operations), 1003)
        self.client.operations.clear()
        with closing(self.open()) as index:
            for key in files:
                self.assertEqual(index.get(key)['sha256'], key.rsplit('/', 1)[1])
        self.assertEqual(self.client.operations, [('list', 'cold/')] * 2)

    def test_unknown_legacy_object_is_verified_without_payload_read(self):
        path = self.seed('old/name.mp4', b'original data')
        with closing(self.open()) as index:
            record = index.get('old/name.mp4')
            self.assertNotIn('sha256', record)
            self.client.operations.clear()
            record = index.get('old/name.mp4', local=media.hash_file(path))
            self.assertEqual(record['sha256'], hashlib.sha256(b'original data').hexdigest())
            self.assertEqual(index.same_content_keys(record['sha256']), ['old/name.mp4'])
            self.assertEqual(self.client.operations, [])
        with closing(self.open()) as index:
            self.assertEqual(index.get('old/name.mp4'), record)

    def test_changed_identity_rechecks_bytes_and_does_not_adopt_overwrites(self):
        path = self.seed('legacy.mp4', b'original')
        with closing(self.open()) as index:
            index.get('legacy.mp4', local=media.hash_file(path))
        self.client.headers['cold/legacy.mp4']['LastModified'] += timedelta(seconds=1)
        self.client.operations.clear()
        with closing(self.open()) as index:
            index.get('legacy.mp4')
        self.assertEqual(self.client.operations, [('list', 'cold/'), ('head', 'cold/legacy.mp4')])
        path.write_bytes(b'replaced')
        with closing(self.open()) as index:
            with self.assertRaisesRegex(ValueError, 'missing or changed'):
                index.get('legacy.mp4')

    def test_missing_objects_never_fabricate_upload_success(self):
        path = self.seed('old.mp4', b'bytes')
        with closing(self.open()) as index:
            index.get('old.mp4', local=media.hash_file(path))
        path.unlink()
        self.client.operations.clear()
        with closing(self.open()) as index:
            self.assertIsNone(index.get('old.mp4'))
            self.assertIsNone(index.get('unknown.mp4'))
            with self.assertRaisesRegex(ValueError, 'missing'):
                index.objects(['old.mp4'])
        self.assertEqual(self.client.operations, [('list', 'cold/')])

    def test_unsupported_full_checksum_evidence_is_review_not_reupload(self):
        self.seed('legacy.mp4', b'bytes')
        self.client.reconcile('cold/legacy.mp4')
        self.client.headers['cold/legacy.mp4']['ChecksumType'] = 'COMPOSITE'
        with closing(self.open()) as index:
            with self.assertRaisesRegex(ValueError, 'refusing automatic reupload'):
                index.get('legacy.mp4')
            self.assertIsNone(index.previous('legacy.mp4')[0])
        self.assertEqual(self.client.operations, [('list', 'cold/'), ('head', 'cold/legacy.mp4')])

    def test_new_upload_proof_requires_local_bytes_and_deep_archive(self):
        local = self.root / 'upload'
        local.write_bytes(b'new content')
        content = media.hash_file(local)
        key = media.object_key(content.sha256.hexdigest())
        with closing(self.open()) as index:
            self.seed(key, b'wrong bytes')
            with self.assertRaisesRegex(ValueError, 'local content checksums'):
                index.verify_upload(key, content)
            self.assertIsNone(index.previous(key)[0])
            self.seed(key, local.read_bytes())
            self.client.reconcile('cold/' + key)
            self.client.headers['cold/' + key]['StorageClass'] = 'GLACIER'
            with self.assertRaisesRegex(ValueError, 'must use Deep Archive'):
                index.verify_upload(key, content)
            self.client.headers['cold/' + key]['StorageClass'] = 'DEEP_ARCHIVE'
            record = index.verify_upload(key, content)
            self.client.operations.clear()
            self.assertEqual(index.get(key), record)
            self.assertEqual(self.client.operations, [])

    def test_scope_change_and_denied_requests_do_not_reuse_old_receipts(self):
        self.seed('original.mp4', b'content')
        with closing(self.open()) as index:
            index.get('original.mp4')
        self.client.operations.clear()
        for change in ({'bucket': 'different-bucket'}, {'prefix': 'elsewhere/'}):
            with self.assertRaisesRegex(ValueError, 'different bucket or prefix'):
                MediaIndex(self.database, self.client, self.store | change)
        self.assertEqual(self.client.operations, [])
        self.client.list_failure = 'AccessDenied'
        with self.assertRaises(S3Error):
            self.open()


if __name__ == '__main__':
    unittest.main()
