"""Shared large-manifest bounds and verified streaming metadata transfers."""

import copy
import base64
import hashlib
import io
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeS3
from manifest_limits import MASTER_BYTES, INVENTORY_BYTES, TRANSFER_BYTES
import native_backup
import native_history
import native_store
import s3_backup_audit
import s3_restore_performer
from stash_archive.storage import InvalidArchive, json_bytes


class LimitedBody(io.BytesIO):
    def __init__(self, body):
        super().__init__(body)
        self.requests = []

    def read(self, amount=-1):
        if not 0 < amount <= TRANSFER_BYTES:
            raise AssertionError('Metadata transfer must use bounded blocks')
        self.requests.append(amount)
        return super().read(amount)


class ManifestLimitTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        cloud = self.root / 'cloud'
        cloud.mkdir()
        self.s3 = FakeS3(cloud)
        self.store = native_store.NativeStore(self.s3, 'metadata')
        archive, checkpoint = str(uuid.uuid4()), str(uuid.uuid4())
        base = native_store.PREFIX + 'runs/' + archive + '/'
        self.catalog = {'format': 's3-log-backup', 'version': 4, 'scope': 'native', 'run_id': 'limits',
                        'created_epoch': 1, 'videos': [], 'units': [], 'objects': {},
                        'media_store': {'bucket': 'media', 'prefix': ''}}
        self.catalog['native_archive'] = {
            'format': native_store.FORMAT, 'version': 1, 'archive_uuid': archive, 'checkpoint_uuid': checkpoint,
            'objects_prefix': native_store.PREFIX + 'objects/', 'selection_sha256': native_store.selection_digest(self.catalog),
            **{kind: native_store.descriptor(base + name, b'fixture') for kind, name in
               [('manifest', 'manifest.json'), ('inventory', 'artifacts.jsonl'), ('verification', 'verification.json')]}}
        self.body = json_bytes(self.catalog)

    def test_writers_history_and_readers_accept_exact_shared_limit_and_refuse_one_extra_byte(self):
        session = native_backup.NativeBackupSession.__new__(native_backup.NativeBackupSession)
        session.root, session.store = self.root, self.store
        session.view = SimpleNamespace(verify=lambda: None)
        session.run_id, session.publication, session.committed = 'limits', self.catalog['native_archive'], False
        path = self.root / 'restore.json'
        path.write_bytes(self.body)
        reader = s3_backup_audit.MetadataReader(Mock())
        with patch.object(native_backup, 'MASTER_BYTES', len(self.body)), \
             patch.object(native_history, 'MASTER_BYTES', len(self.body)), \
             patch.object(native_store, 'MASTER_BYTES', len(self.body)), \
             patch.object(s3_backup_audit, 'MASTER_BYTES', len(self.body)), \
             patch.object(s3_restore_performer, 'MASTER_BYTES', len(self.body)):
            session.prepare_master(self.body)
            session.commit_master(self.body)
            self.assertTrue(session.committed)
            native_history.validate_publication(native_history.publication(self.body), session.publication['archive_uuid'])
            self.assertEqual(s3_restore_performer.load_manifest(path), self.catalog)
            self.assertEqual(native_backup.read_record(path, limit=len(self.body)), self.catalog)
            reader.client.get_object.return_value = {'Body': io.BytesIO(self.body), 'ContentLength': len(self.body)}
            self.assertEqual(reader.manifest('current_manifest.json')[0], self.body)
            oversized = self.body + b'\n'
            path.write_bytes(oversized)
            for operation in (lambda: session.prepare_master(oversized), lambda: native_history.publication(oversized),
                              lambda: s3_restore_performer.load_manifest(path),
                              lambda: native_backup.read_record(path, limit=len(self.body))):
                with self.subTest(operation=operation), self.assertRaises(ValueError):
                    operation()
            for key in ('current_manifest.json', 'current_manifest.txt'):
                reader.client.get_object.return_value = {'Body': io.BytesIO(oversized), 'ContentLength': len(oversized)}
                with self.assertRaisesRegex(ValueError, 'exceeds'):
                    reader.manifest(key)
            with self.assertRaisesRegex(InvalidArchive, 'oversized'):
                self.store.read(native_store.descriptor('object', oversized))

    def test_inventory_has_independent_streamed_limit_and_manifest_fields_stay_bounded(self):
        reference = copy.deepcopy(self.catalog['native_archive'])
        reference['inventory']['bytes'] = INVENTORY_BYTES
        native_store.validate_reference(reference)
        reference['inventory']['bytes'] += 1
        with self.assertRaises(InvalidArchive):
            native_store.validate_reference(reference)
        for field in ('manifest', 'verification'):
            reference = copy.deepcopy(self.catalog['native_archive'])
            reference[field]['bytes'] = (128 << 20) + 1
            with self.subTest(field=field), self.assertRaises(InvalidArchive):
                native_store.validate_reference(reference)
        record = native_history.publication(self.body)
        record['master']['bytes'] = MASTER_BYTES
        native_history.validate_publication(record, record['archive_uuid'])
        record['master']['bytes'] += 1
        with self.assertRaises(InvalidArchive):
            native_history.validate_publication(record, record['archive_uuid'])

    def test_sparse_oversized_local_manifest_is_refused_before_parsing(self):
        path = self.root / 'oversized.json'
        with path.open('wb') as out:
            out.truncate(MASTER_BYTES + 1)
        with patch.object(s3_restore_performer, 'decode_json', side_effect=AssertionError('must not decode')):
            with self.assertRaisesRegex(ValueError, 'exceeds'):
                s3_restore_performer.load_manifest(path)

    def test_audit_rejects_truncation_and_oversized_undeclared_body(self):
        reader = s3_backup_audit.MetadataReader(Mock())
        reader.client.get_object.return_value = {'Body': io.BytesIO(b'{}'), 'ContentLength': 3}
        with self.assertRaisesRegex(ValueError, 'declared length'):
            reader.manifest('current_manifest.json')
        with patch.object(s3_backup_audit, 'MASTER_BYTES', 4):
            stream = io.BytesIO(b'abcdef')
            reader.client.get_object.return_value = {'Body': stream}
            with self.assertRaisesRegex(ValueError, 'size limit'):
                reader.manifest('current_manifest.txt')
            self.assertTrue(stream.closed)

    def test_streamed_download_bounds_reads_and_publishes_only_verified_bytes(self):
        body = b'data' * (TRANSFER_BYTES // 4 + 7)
        value = self.store.put_bytes('large/inventory', body)
        stream = LimitedBody(body)
        path = self.root / 'download.jsonl'
        with patch.object(self.s3, 'get_object', return_value={'Body': stream}):
            self.store.download_file(value, path, reserve=0)
        self.assertEqual(path.read_bytes(), body)
        self.assertEqual(stream.requests, [TRANSFER_BYTES, 28, 1])
        self.assertTrue(stream.closed)
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(list(self.root.glob('.native-download-*')), [])

    def test_inventory_over_old_128mib_limit_streams_without_a_whole_body_buffer(self):
        size = (128 << 20) + 17
        digest = hashlib.sha256()
        for _ in range(128):
            digest.update(b'x' * TRANSFER_BYTES)
        digest.update(b'x' * 17)
        value = {'key': 'large/inventory', 'bytes': size, 'sha256': digest.hexdigest()}
        head = {'ContentLength': size, 'StorageClass': 'STANDARD', 'ChecksumType': 'FULL_OBJECT',
                'ChecksumSHA256': base64.b64encode(digest.digest()).decode()}
        class GeneratedBody(LimitedBody):
            def __init__(self):
                super().__init__(b'')
                self.remaining = size
            def read(self, amount=-1):
                if not 0 < amount <= TRANSFER_BYTES:
                    raise AssertionError('Whole-object read is forbidden')
                self.requests.append(amount)
                length = min(amount, self.remaining)
                self.remaining -= length
                return b'x' * length
        stream = GeneratedBody()
        path = self.root / 'large.jsonl'
        with patch.object(self.store, 'head', return_value=head), \
             patch.object(self.s3, 'get_object', return_value={'Body': stream}):
            self.store.download_file(value, path, reserve=0)
        self.assertEqual(path.stat().st_size, size)
        with path.open('rb') as incoming:
            self.assertEqual(hashlib.file_digest(incoming, 'sha256').hexdigest(), value['sha256'])
        self.assertEqual(len(stream.requests), 130)
        self.assertTrue(stream.closed)

    def test_bad_stream_never_publishes_or_leaves_temporary_files(self):
        body = b'content' * 1000
        value = self.store.put_bytes('inventory', body)
        for changed in (body[:-1], body + b'extra', b'X' + body[1:]):
            path = self.root / 'failed.jsonl'
            stream = LimitedBody(changed)
            with self.subTest(size=len(changed)), patch.object(self.s3, 'get_object', return_value={'Body': stream}):
                with self.assertRaisesRegex(InvalidArchive, 'length|digest'):
                    self.store.download_file(value, path, reserve=0)
            self.assertTrue(stream.closed)
            self.assertFalse(path.exists())
            self.assertEqual(list(self.root.glob('.native-download-*')), [])

    def test_network_failure_closes_descriptor_and_removes_temporary_download(self):
        value = self.store.put_bytes('inventory', b'data')
        with patch.object(self.s3, 'get_object', side_effect=OSError('network interruption')):
            with self.assertRaisesRegex(OSError, 'network interruption'):
                self.store.download_file(value, self.root / 'failed', reserve=0)
        self.assertFalse((self.root / 'failed').exists())
        self.assertEqual(list(self.root.glob('.native-download-*')), [])

    def test_streaming_space_reserve_failure_cleans_partial_file(self):
        body = b'x' * (TRANSFER_BYTES * 2)
        value = self.store.put_bytes('inventory', body)
        with patch.object(native_store, 'require_space', side_effect=[None, None, InvalidArchive('reserved headroom')]):
            with self.assertRaisesRegex(InvalidArchive, 'reserved headroom'):
                self.store.download_file(value, self.root / 'failed', reserve=50 << 30)
        self.assertFalse((self.root / 'failed').exists())
        self.assertEqual(list(self.root.glob('.native-download-*')), [])

    def test_download_never_replaces_existing_destination_or_unknown_files(self):
        value = self.store.put_bytes('inventory', b'new')
        path = self.root / 'keep'
        path.write_bytes(b'existing')
        self.s3.operations.clear()
        with self.assertRaisesRegex(InvalidArchive, 'already exists'):
            self.store.download_file(value, path, reserve=0)
        self.assertEqual(path.read_bytes(), b'existing')
        self.assertEqual(self.s3.operations, [])

    def test_oversized_or_unsafe_descriptor_never_starts_cloud_reads(self):
        for value in ({'key': 'inventory', 'sha256': '1' * 64, 'bytes': INVENTORY_BYTES + 1},
                      {'key': '../inventory', 'sha256': '1' * 64, 'bytes': 3},
                      {'key': 'inventory', 'sha256': '1' * 64, 'bytes': True}):
            with self.subTest(value=value), self.assertRaisesRegex(InvalidArchive, 'descriptor'):
                self.store.download_file(value, self.root / 'never', reserve=0)
        self.assertEqual(self.s3.operations, [])


if __name__ == '__main__':
    unittest.main()
