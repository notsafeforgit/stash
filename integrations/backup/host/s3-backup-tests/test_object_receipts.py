"""Bounded request counts and failure semantics for the verified upload index."""

from contextlib import closing
from datetime import timedelta
import hashlib
import io
from pathlib import Path
import sqlite3
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeS3, S3Error
from native_store import NativeStore, PREFIX
from object_receipts import ObjectReceipts, inventory
from stash_archive.storage import InvalidArchive


class ObjectReceiptTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cloud = self.root / 'cloud'
        self.cloud.mkdir()
        self.s3 = FakeS3(self.cloud)
        self.store = NativeStore(self.s3, 'bucket')
        self.path = self.root / 'receipts.sqlite3'

    def test_thousand_unchanged_objects_need_two_list_requests_after_restart(self):
        objects = []
        receipts = ObjectReceipts(self.path, 'bucket')
        try:
            listed = inventory(self.s3, 'bucket', PREFIX + 'objects/')
            for index in range(1003):
                body = str(index).encode()
                sha = hashlib.sha256(body).hexdigest()
                key = PREFIX + 'objects/' + sha + '.gz'
                self.store.put(key, io.BytesIO(body), sha, len(body), receipts=receipts, listed=listed)
                objects.append((key, body, sha))
        finally:
            receipts.close()
        self.s3.operations.clear()
        receipts = ObjectReceipts(self.path, 'bucket')
        try:
            listed = inventory(self.s3, 'bucket', PREFIX + 'objects/')
            for key, body, sha in objects:
                self.store.put(key, io.BytesIO(body), sha, len(body), receipts=receipts, listed=listed)
        finally:
            receipts.close()
        self.assertEqual(self.s3.operations, [('list', PREFIX + 'objects/')] * 2)

    def test_size_etag_date_class_and_bucket_changes_invalidate_receipts(self):
        value = self.store.put_bytes('object', b'bytes')
        receipts = ObjectReceipts(self.path, 'bucket')
        try:
            head = self.s3.headers['object']
            receipts.remember('object', value['sha256'], head)
            listed = inventory(self.s3, 'bucket', '')['object']
            self.assertTrue(receipts.matches('object', value['sha256'], 5, listed))
            for change in ({'ETag': 'changed'}, {'Size': 6}, {'StorageClass': 'DEEP_ARCHIVE'},
                           {'LastModified': head['LastModified'] + timedelta(seconds=1)}, {'ETag': None}):
                with self.subTest(change=change):
                    self.assertFalse(receipts.matches('object', value['sha256'], 5, listed | change))
            self.assertFalse(receipts.matches('object', '0' * 64, 5, listed))
            self.assertFalse(receipts.matches('object', value['sha256'], 5, None))
            for change in ({'ChecksumSHA256': None}, {'ChecksumType': 'COMPOSITE'}, {'ChecksumType': None}):
                with self.assertRaises(InvalidArchive):
                    receipts.remember('object', value['sha256'], head | change)
        finally:
            receipts.close()
        receipts = ObjectReceipts(self.path, 'different-bucket')
        try:
            self.assertFalse(receipts.matches('object', value['sha256'], 5, listed))
        finally:
            receipts.close()

    def test_partial_duplicate_and_failed_inventory_never_returns_a_result(self):
        first = {'IsTruncated': True, 'NextContinuationToken': 'next', 'Contents': [{'Key': 'objects/a'}]}
        duplicate = {'IsTruncated': False, 'Contents': [{'Key': 'objects/a'}]}
        for second in (duplicate, {'IsTruncated': True, 'NextContinuationToken': 'next',
                                  'Contents': [{'Key': 'objects/b'}]}, S3Error('AccessDenied')):
            with patch.object(self.s3, 'list_objects_v2', side_effect=[first, second]):
                with self.assertRaises((InvalidArchive, S3Error)):
                    inventory(self.s3, 'bucket', 'objects/')

    def test_foreign_database_and_symlink_are_not_used_as_upload_evidence(self):
        self.path.symlink_to(self.root / 'missing')
        with self.assertRaises(OSError):
            ObjectReceipts(self.path, 'bucket')
        self.path.unlink()
        with closing(sqlite3.connect(self.path)) as db, db:
            db.execute('PRAGMA application_id=123')
            db.execute('CREATE TABLE foreign_data(value)')
        self.path.chmod(0o600)
        with self.assertRaises(InvalidArchive):
            ObjectReceipts(self.path, 'bucket')
        with closing(sqlite3.connect(self.path)) as db:
            self.assertEqual(db.execute('PRAGMA application_id').fetchone()[0], 123)
