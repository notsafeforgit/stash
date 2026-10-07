"""Adopt historical S3 bytes without an upload, thaw or trusted user metadata."""

from contextlib import closing
from datetime import datetime, timedelta, timezone
import hashlib
import io
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeS3
from media_index import MediaIndex
import media_objects as media
import s3_restore_performer as restore


def etag(body, part_size=None):
    if part_size is None:
        return '"' + hashlib.md5(body, usedforsecurity=False).hexdigest() + '"'
    parts = [body[offset:offset + part_size] for offset in range(0, len(body), part_size)] or [b'']
    checksums = b''.join(hashlib.md5(part, usedforsecurity=False).digest() for part in parts)
    return '"' + hashlib.md5(checksums, usedforsecurity=False).hexdigest() + '-' + str(len(parts)) + '"'


class HistoricalS3(FakeS3):
    def put_object(self, **kwargs):
        raise AssertionError('Existing media must not be uploaded')

    def restore_object(self, **kwargs):
        raise AssertionError('Adoption must not thaw media')

    def get_object(self, *, Bucket, Key, ChecksumMode):
        assert ChecksumMode == 'ENABLED'
        self.operations.append(('get', Key))
        body = (self.root / Key).read_bytes()
        if self.corrupt_read:
            body = body[:-1] + bytes([body[-1] ^ 1])
        return dict(self.headers[Key], Body=io.BytesIO(body))


class HistoricalMediaTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cloud = self.root / 'cloud'
        self.cloud.mkdir()
        self.client = HistoricalS3(self.cloud)
        self.store = {'bucket': 'historical-archive', 'prefix': 'cold/'}
        self.ledger = self.root / 'ledger.sqlite3'

    def seed(self, key, body, part_size=None):
        full = self.store['prefix'] + key
        path = self.cloud / full
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        self.client.headers[full] = {'ContentLength': len(body), 'ETag': etag(body, part_size),
                                     'StorageClass': 'DEEP_ARCHIVE', 'ServerSideEncryption': 'AES256',
                                     'LastModified': datetime(2026, 1, 1, tzinfo=timezone.utc)}
        return path, self.client.headers[full]

    def test_multipart_stream_boundaries_and_repeatable_finalization(self):
        for size in (1, 5, 16):
            for length in (0, size - 1, size, size + 1, 2 * size, 2 * size + 1):
                body = bytes(range(length))
                for step in (1, 3, 11, 50):
                    with self.subTest(size=size, length=length, step=step):
                        result = media.MultipartETag(size)
                        for offset in range(0, length, step):
                            result.value()  # Observing a digest must not finish it.
                            result.update(body[offset:offset + step])
                        self.assertEqual(result.value(), etag(body, size)[1:-1])
                        self.assertEqual(result.value(), result.value())
        for size in (0, -1, True, '16', (5 << 30) + 1):
            with self.subTest(size=size), self.assertRaises(ValueError):
                media.MultipartETag(size)

    def test_single_part_adoption_requires_local_bytes_and_retains_sha256(self):
        path, head = self.seed('older.mp4', b'older video bytes')
        local = media.hash_file(path)
        with self.assertRaisesRegex(ValueError, 'full-object S3 checksum'):
            media.existing_object_from_head(head)
        record = media.existing_object_from_head(head, local=local)
        self.assertEqual(record['checksum'], {'algorithm': media.ETAG_MD5, 'value': etag(path.read_bytes())[1:-1]})
        self.assertEqual(record['sha256'], hashlib.sha256(path.read_bytes()).hexdigest())
        media.verify_head(record, head)
        media.verify_local(record, path)
        self.assertEqual(self.client.operations, [])

    def test_both_historical_multipart_sizes_match_complete_local_bytes(self):
        for size in media.LEGACY_PART_SIZES:
            for extra in (0, 19):
                body = b'x' * size + b'y' * extra
                path, head = self.seed('multipart.mp4', body, size)
                local = media.Digests()
                for offset in range(0, len(body), 1_048_581):
                    local.update(body[offset:offset + 1_048_581])
                record = media.existing_object_from_head(head, local=local)
                self.assertEqual(record['checksum']['algorithm'], media.ETAG_MULTIPART)
                self.assertEqual(record['checksum']['value'], etag(body, size)[1:-1])
                if extra:
                    self.assertEqual(record['checksum']['part_size'], size)
                media.verify_head(record, head)
                media.verify_local(record, path)
                self.assertTrue(media.matches_local(record, local))

    def test_same_size_and_part_count_cannot_adopt_changed_bytes(self):
        size = media.LEGACY_PART_SIZES[0]
        original = b'a' * size + b'old'
        _, head = self.seed('multipart.mp4', original, size)
        local = media.Digests()
        local.update(b'a' * size + b'new')
        with self.assertRaisesRegex(ValueError, 'does not match verified local bytes'):
            media.existing_object_from_head(head, local=local)
        self.assertEqual(self.client.operations, [])

    def test_metadata_encryption_or_unusable_headers_cannot_establish_proof(self):
        path, head = self.seed('old.mp4', b'local bytes')
        local = media.hash_file(path)
        changes = [
            {'ServerSideEncryption': 'aws:kms'}, {'ServerSideEncryption': 'aws:kms:dsse'},
            {'SSECustomerAlgorithm': 'AES256'}, {'SSECustomerKeyMD5': 'customer'}, {'SSEKMSKeyId': 'key'},
            {'ETag': '"' + '0' * 32 + '"', 'Metadata': {'md5chksum': local.md5.hexdigest(), 'sha256': local.sha256.hexdigest()}},
            {'ETag': local.md5.hexdigest()}, {'ETag': '"' + local.md5.hexdigest().upper() + '"'},
            {'ETag': '"' + local.md5.hexdigest() + '-0"'}, {'ETag': '"' + local.md5.hexdigest() + '-10001"'},
            {'ContentLength': True}, {'ContentLength': len(b'local bytes') + 1}, {'DeleteMarker': True},
            {'StorageClass': 'STANDARD'}, {'PartsCount': 1},
            {'ChecksumType': 'COMPOSITE'}, {'ChecksumType': None}, {'ChecksumSHA256': 'bad'},
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                media.existing_object_from_head(head | change, local=local)

    def test_plaintext_and_single_part_multipart_uploads_have_distinct_proofs(self):
        path, head = self.seed('old.mp4', b'one part', media.LEGACY_PART_SIZES[0])
        head.pop('ServerSideEncryption')
        head['PartsCount'] = 1
        record = media.existing_object_from_head(head, local=media.hash_file(path))
        self.assertEqual(record['checksum']['algorithm'], media.ETAG_MULTIPART)
        self.assertTrue(record['checksum']['value'].endswith('-1'))
        media.verify_head(record, head)
        for count in (True, 0, 2):
            with self.subTest(count=count), self.assertRaisesRegex(ValueError, 'multipart count contradicts'):
                media.existing_object_from_head(head | {'PartsCount': count}, local=media.hash_file(path))

    def test_historical_descriptors_require_exact_layout_and_independent_sha256(self):
        body = b'a' * media.LEGACY_PART_SIZES[0] + b'b'
        path, head = self.seed('parts.mp4', body, media.LEGACY_PART_SIZES[0])
        record = media.existing_object_from_head(head, local=media.hash_file(path))
        for changed in (
            {k: v for k, v in record.items() if k != 'sha256'},
            record | {'sha256': 'bad'}, record | {'size': 1},
            record | {'checksum': record['checksum'] | {'part_size': True}},
            record | {'checksum': record['checksum'] | {'part_size': 6 << 20}},
            record | {'checksum': record['checksum'] | {'part_size': 16 << 20}},
            record | {'checksum': record['checksum'] | {'value': '0' * 32 + '-02'}},
            record | {'checksum': record['checksum'] | {'untrusted': 'extra'}},
        ):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                media.validate_object(changed)
        self.assertFalse(media.matches_local(record | {'sha256': '0' * 64}, media.hash_file(path)))

    def test_index_restart_reuses_adoption_without_per_object_requests(self):
        path, head = self.seed('old.mp4', b'old bytes')
        with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
            record = index.get('old.mp4', local=media.hash_file(path), allow_legacy_etag=True)
            self.assertEqual(index.same_content_keys(record['sha256']), ['old.mp4'])
        self.assertEqual(self.client.operations, [('list', 'cold/'), ('head', 'cold/old.mp4')])
        self.client.operations.clear()
        with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
            self.assertEqual(index.get('old.mp4'), record)
        self.assertEqual(self.client.operations, [('list', 'cold/')])
        head['LastModified'] += timedelta(seconds=1)
        self.client.operations.clear()
        with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
            self.assertEqual(index.get('old.mp4'), record)
        self.assertEqual(self.client.operations, [('list', 'cold/'), ('head', 'cold/old.mp4')])

    def test_known_legacy_bytes_and_encryption_cannot_be_replaced(self):
        path, head = self.seed('old.mp4', b'old bytes')
        with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
            record = index.get('old.mp4', local=media.hash_file(path), allow_legacy_etag=True)
        for change in ({'ETag': '"' + '0' * 32 + '"'}, {'ServerSideEncryption': 'aws:kms'}, {'PartsCount': 1}):
            original = dict(head)
            head.update(change, LastModified=head['LastModified'] + timedelta(seconds=1))
            try:
                with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
                    with self.assertRaisesRegex(ValueError, 'missing or changed'):
                        index.get('old.mp4')
                    self.assertEqual(index.previous('old.mp4')[0], record)
            finally:
                head.clear()
                head.update(original)

    def test_new_upload_verification_never_falls_back_to_legacy_etags(self):
        path, head = self.seed('new.mp4', b'new bytes')
        with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
            with self.assertRaisesRegex(ValueError, 'full-object S3 checksum'):
                index.verify_upload('new.mp4', media.hash_file(path))
            self.assertIsNone(index.previous('new.mp4')[0])
        with self.assertRaisesRegex(ValueError, 'full-object S3 checksum'):
            media.object_from_head(head, local=media.hash_file(path))

    def test_unknown_content_keys_and_archives_require_full_object_evidence(self):
        body = b'no additional checksum headers'
        key = media.object_key(hashlib.sha256(body).hexdigest())
        for key, allow in ((key, True), ('tarballs/new.tar', False), ('old.mp4', False)):
            with self.subTest(key=key):
                path, _ = self.seed(key, body)
                with closing(MediaIndex(self.ledger, self.client, self.store)) as index:
                    with self.assertRaisesRegex(ValueError, 'full-object S3 checksum'):
                        index.get(key, local=media.hash_file(path), allow_legacy_etag=allow)
                    self.assertIsNone(index.previous(key)[0])

    def test_historical_manifest_restores_offline_and_remote_stream_checks_sha256(self):
        body = b'z' * media.LEGACY_PART_SIZES[0] + b'tail'
        path, head = self.seed('old/location.mp4', body, media.LEGACY_PART_SIZES[0])
        record = media.existing_object_from_head(head, local=media.hash_file(path))
        catalog = {'format': 's3-log-backup', 'version': 4, 'scope': 'media', 'run_id': 'fixture',
                   'media_store': self.store, 'videos': [{'key': 'old/location.mp4', 'path': 'renamed/video.mp4', 'size': len(body)}],
                   'units': [], 'objects': {'old/location.mp4': record}}
        restored = self.root / 'offline'
        restore.restore_local(catalog, self.cloud / 'cold', restored)
        self.assertEqual((restored / 'renamed/video.mp4').read_bytes(), body)
        self.assertEqual(self.client.operations, [])
        downloaded = self.root / 'downloaded.mp4'
        media.download(self.client, self.store, 'old/location.mp4', record, downloaded, reserve=0)
        self.assertEqual(downloaded.read_bytes(), body)
        self.client.corrupt_read = True
        rejected = self.root / 'rejected.mp4'
        with self.assertRaisesRegex(ValueError, 'content verification'):
            media.download(self.client, self.store, 'old/location.mp4', record, rejected, reserve=0)
        self.assertFalse(rejected.exists())
        self.client.corrupt_read = False
        with self.assertRaisesRegex(ValueError, 'content verification'):
            media.download(self.client, self.store, 'old/location.mp4', record | {'sha256': '0' * 64}, rejected, reserve=0)
        self.assertFalse(rejected.exists())


if __name__ == '__main__':
    unittest.main()
