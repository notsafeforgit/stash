"""Cold-media identity and restore contracts; all cloud requests stay local."""

import base64
import copy
from contextlib import closing
from datetime import datetime, timezone
import hashlib
import io
import json
from pathlib import Path
import sqlite3
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeS3, S3Error
import media_objects as media
from native_store import FORMAT, PREFIX, media_selection, selection_digest
import s3_backup_audit as audit
import s3_restore_performer as restore


class ColdS3(FakeS3):
    bucket = 'archive-bucket'
    prefix = 'retained/media/'

    def seed(self, key, body, algorithm='crc64nvme'):
        content = media.Digests()
        content.update(body)
        full = self.prefix + key
        path = self.root / full
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        self.headers[full] = {'ContentLength': len(body), 'ChecksumType': 'FULL_OBJECT',
                              media.ALGORITHMS[algorithm][0]: content.checksum(algorithm),
                              'StorageClass': 'DEEP_ARCHIVE', 'ETag': '"multipart-etag-2"',
                              'LastModified': datetime(2026, 10, 5, tzinfo=timezone.utc)}
        return media.object_from_head(self.headers[full], local=content)

    def list_objects_v2(self, **args):
        assert args['Bucket'] == self.bucket and args['Prefix'] == self.prefix
        return super().list_objects_v2(**args)

    def head_object(self, **args):
        assert args['Bucket'] == self.bucket and args['Key'].startswith(self.prefix)
        return super().head_object(**args)

    def get_object(self, *, Bucket, Key, ChecksumMode):
        assert Bucket == self.bucket and Key.startswith(self.prefix) and ChecksumMode == 'ENABLED'
        self.operations.append(('get', Key))
        if not (self.root / Key).is_file():
            raise S3Error('NoSuchKey')
        body = (self.root / Key).read_bytes()
        if self.corrupt_read:
            body = body[:-1] + bytes([body[-1] ^ 1])
        return dict(self.headers[Key], Body=io.BytesIO(body))

    def restore_object(self, *, Bucket, Key, RestoreRequest):
        assert Bucket == self.bucket and Key.startswith(self.prefix)
        assert RestoreRequest == {'Days': 7, 'GlacierJobParameters': {'Tier': 'Bulk'}}
        self.operations.append(('thaw', Key))
        return {}

    def put_object(self, **args):
        raise AssertionError('Identity verification and restore must never upload')


class MediaTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cloud = self.root / 'cloud'
        self.cloud.mkdir()
        self.client = ColdS3(self.cloud)
        self.store = {'bucket': self.client.bucket, 'prefix': self.client.prefix}
        self.objects = self.cloud / self.client.prefix
        self.body = b'original video bytes'
        self.key = media.object_key(hashlib.sha256(self.body).hexdigest())
        self.video = self.client.seed(self.key, self.body)
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w:') as tar:
            info = tarfile.TarInfo('photo.jpg')
            info.size = len(b'image bytes')
            tar.addfile(info, io.BytesIO(b'image bytes'))
        self.base_key = 'tarballs/creator.base.tar'
        self.base = self.client.seed(self.base_key, stream.getvalue())
        self.catalog = {'format': 's3-log-backup', 'version': 4, 'scope': 'media', 'run_id': 'fixture',
                        'media_store': self.store,
                        'videos': [{'key': self.key, 'path': path, 'size': len(self.body)}
                                   for path in ('creator/first.mp4', 'creator/duplicate.mp4')],
                        'units': [{'unit_id': 'R:creator', 'kind': 'recursive', 'rel_dir': 'creator',
                                   'base_key': self.base_key, 'deltas': []}],
                        'objects': {self.key: self.video, self.base_key: self.base}}
        for target in ('boto3.client', 'subprocess.run', 'subprocess.Popen', 's3_restore_performer.media_client'):
            guard = patch(target, side_effect=AssertionError('Unexpected external request'))
            guard.start()
            self.addCleanup(guard.stop)

    def write_manifest(self, catalog=None):
        path = self.root / 'manifest.json'
        path.write_text(json.dumps(catalog or self.catalog))
        return path

    def test_streaming_checksums_and_legacy_full_checksum_adoption(self):
        expected = {'sha256': hashlib.sha256(b'123456789').digest(),
                    'sha1': hashlib.sha1(b'123456789').digest(),
                    'crc32': bytes.fromhex('cbf43926'), 'crc32c': bytes.fromhex('e3069283'),
                    'crc64nvme': bytes.fromhex('ae8b14860a799888')}
        content = media.Digests()
        for part in (b'123', b'456', b'789'):
            content.update(part)
        path = self.root / 'legacy.mp4'
        path.write_bytes(b'123456789')
        local = media.hash_file(path)
        for algorithm, value in expected.items():
            with self.subTest(algorithm=algorithm):
                encoded = base64.b64encode(value).decode('ascii')
                self.assertEqual(content.checksum(algorithm), encoded)
                self.assertEqual(local.checksum(algorithm), encoded)
                record = self.client.seed('old/filename.mp4', path.read_bytes(), algorithm)
                self.assertTrue(media.matches_local(record, local))
                media.verify_local(record, path)
        self.assertEqual(self.client.operations, [])  # No thaw, copy or upload.

    def test_missing_composite_or_metadata_only_evidence_never_suffices(self):
        header = self.client.headers[self.client.prefix + self.key]
        for change in ({'ChecksumType': 'COMPOSITE'}, {'ChecksumType': None}, {'StorageClass': 'STANDARD'},
                       {'ChecksumCRC64NVME': None}, {'ChecksumCRC64NVME': 'not-base64'},
                       {'ContentLength': True}, {'StorageClass': []}, {'DeleteMarker': True}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                media.object_from_head(header | change | {'Metadata': {'sha256': self.video['sha256']}})
        with self.assertRaises(ValueError):
            media.object_from_head(header, local=media.Digests())
        self.assertEqual(self.client.operations, [])

    def test_malformed_descriptors_are_rejected(self):
        for change in ({'size': True}, {'size': -1}, {'storage_class': []}, {'sha256': 'a' * 63},
                       {'checksum': {'algorithm': [], 'value': ''}},
                       {'checksum': {'algorithm': 'crc64nvme', 'value': 'AAAA'}},
                       {'VersionId': 'unused'}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                media.validate_object(self.video | change)

    def test_shared_object_restores_to_each_path_without_network(self):
        plan = restore.select_plan(self.catalog, 'creator', include_videos=True)
        self.assertEqual(restore.required_keys(plan), [self.base_key, self.key])
        target = self.root / 'restored'
        restore.restore_local(plan, self.objects, target)
        self.assertEqual((target / 'creator/photo.jpg').read_bytes(), b'image bytes')
        for video in plan['videos']:
            self.assertEqual((target / video['path']).read_bytes(), self.body)
        self.assertEqual(self.client.operations, [])

    def test_selection_searches_filenames_and_retains_only_selected_proofs(self):
        plan = restore.select_plan(self.catalog, 'duplicate', include_videos=True)
        self.assertEqual(plan['videos'], [self.catalog['videos'][1]])
        self.assertEqual(plan['objects'], {self.key: self.video})
        self.assertEqual(plan['media_store'], self.store)
        self.assertEqual(plan['scope'], 'media')
        with self.assertRaises(ValueError):
            restore.select_plan(self.catalog, self.video['sha256'], include_videos=True)

    def native_catalog(self):
        catalog = copy.deepcopy(self.catalog)
        catalog['scope'] = 'native'
        uid = '416e9faa-d5d8-4a77-9041-cfe58178467a'
        catalog['native_archive'] = {'format': FORMAT, 'version': 1, 'archive_uuid': uid, 'checkpoint_uuid': uid,
                                     'selection_sha256': selection_digest(catalog), 'objects_prefix': PREFIX + 'objects/'}
        for name, filename in (('manifest', 'manifest.json'), ('inventory', 'artifacts.jsonl'), ('verification', 'verification.json')):
            catalog['native_archive'][name] = {'key': PREFIX + 'runs/' + uid + '/' + filename,
                                                'sha256': '1' * 64, 'bytes': 1}
        return catalog

    def test_native_binding_covers_store_paths_and_full_content_identity(self):
        original = self.native_catalog()
        restore.validate_manifest(original)
        for target, field, value in ((('media_store',), 'bucket', 'different-bucket'),
                                      (('media_store',), 'prefix', 'different/'),
                                      (('videos', 0), 'path', 'creator/renamed.mp4'),
                                      (('objects', self.base_key, 'checksum'), 'value', base64.b64encode(b'changed!').decode())):
            catalog = copy.deepcopy(original)
            record = catalog
            for part in target:
                record = record[part]
            record[field] = value
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, 'selection differs'):
                restore.validate_manifest(catalog)
        selected = restore.select_plan(original, 'first', include_videos=True)
        self.assertNotIn('native_archive', selected)
        self.assertEqual(selected['scope'], 'media')
        self.assertEqual(media_selection(original)['version'], 2)
        self.assertEqual(selection_digest(media_selection(original)), selection_digest(original))

    def test_invalid_store_paths_inventory_and_collisions_fail_before_mutation(self):
        for change in ({'media_store': {'bucket': 'archive-bucket', 'prefix': '../'}},
                       {'objects': {self.key: self.video}}, {'scope': 'everything'},
                       {'videos': [self.catalog['videos'][0]] * 2},
                       {'videos': [self.catalog['videos'][0] | {'path': '../escape.mp4'}]},
                       {'videos': [self.catalog['videos'][0] | {'path': 'creator'}]},
                       {'videos': [self.catalog['videos'][0] | {'path': 'creator'}, self.catalog['videos'][1]]},
                       {'videos': [self.catalog['videos'][0] | {'size': True}]}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                restore.restore_local(self.catalog | change, self.objects, self.root / 'invalid')
            self.assertFalse((self.root / 'invalid').exists())
        altered = copy.deepcopy(self.catalog)
        altered['objects'][self.key]['sha256'] = '1' * 64
        with self.assertRaisesRegex(ValueError, 'key differs'):
            restore.validate_manifest(altered)

    def test_video_cannot_silently_replace_an_archive_member(self):
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w:') as tar:
            info = tarfile.TarInfo('first.mp4')
            info.size = 3
            tar.addfile(info, io.BytesIO(b'old'))
        self.catalog['objects'][self.base_key] = self.client.seed(self.base_key, stream.getvalue())
        with self.assertRaisesRegex(ValueError, 'collides with an archive member'):
            restore.restore_local(self.catalog, self.objects, self.root / 'collision')
        self.assertEqual((self.root / 'collision/creator/first.mp4').read_bytes(), b'old')

    def test_tampered_or_missing_objects_fail_before_creating_output(self):
        for key in (self.key, self.base_key):
            path = self.objects / key
            original = path.read_bytes()
            path.write_bytes(original[:-1] + bytes([original[-1] ^ 1]))
            with self.assertRaisesRegex(ValueError, 'content identity'):
                restore.restore_local(self.catalog, self.objects, self.root / 'tampered')
            self.assertFalse((self.root / 'tampered').exists())
            path.unlink()
            with self.assertRaises(FileNotFoundError):
                restore.restore_local(self.catalog, self.objects, self.root / 'missing')
            self.assertFalse((self.root / 'missing').exists())
            path.write_bytes(original)

    def test_mutation_after_preflight_is_not_reported_as_success(self):
        original = restore.apply_archive
        def during_restore(*args):
            result = original(*args)
            (self.objects / self.key).write_bytes(b'replaced after checking')
            return result
        with patch.object(restore, 'apply_archive', side_effect=during_restore):
            with self.assertRaisesRegex(ValueError, 'changed after restore'):
                restore.restore_local(self.catalog, self.objects, self.root / 'interrupted')
        self.assertFalse((self.root / 'interrupted/creator/first.mp4').exists())

    def test_plan_only_does_not_construct_client_or_read_cold_objects(self):
        path = self.write_manifest()
        saved = self.root / 'plan.json'
        with patch('sys.stdout', new=io.StringIO()) as output:
            restore.main(['creator', '--manifest', str(path), '--include-videos', '--save-plan', str(saved)])
        self.assertIn('2 videos, 2 objects', output.getvalue())
        self.assertEqual(restore.load_manifest(saved), self.catalog)
        self.assertEqual(self.client.operations, [])

    def test_explicit_status_thaw_and_download_use_bound_keys_once(self):
        args = ['creator', '--manifest', str(self.write_manifest()), '--include-videos']
        with patch.object(restore, 'media_client', return_value=self.client), patch('sys.stdout', new=io.StringIO()):
            restore.main(args + ['--check-status'])
            self.assertEqual(self.client.operations, [('head', self.client.prefix + key) for key in (self.base_key, self.key)])
            self.client.operations.clear()
            restore.main(args + ['--request-thaw'])
            self.assertEqual(self.client.operations, [(op, self.client.prefix + key)
                                                       for key in (self.base_key, self.key) for op in ('head', 'thaw')])
            self.client.operations.clear()
            restore.main(args + ['--download', '--destination', str(self.root / 'downloaded')])
            self.assertEqual(self.client.operations, [('get', self.client.prefix + key) for key in (self.base_key, self.key)])
            self.assertEqual((self.root / 'downloaded/creator/first.mp4').read_bytes(), self.body)

    def test_no_fallback_on_missing_denied_changed_or_expired_objects(self):
        for code in ('AccessDenied', 'NoSuchKey', 'SlowDown'):
            self.client.head_failure = code
            with self.assertRaises(S3Error):
                media.request_restore(self.client, self.store, self.key, self.video, 7)
        self.client.head_failure = None
        self.client.headers[self.client.prefix + self.key]['ChecksumCRC64NVME'] = base64.b64encode(b'changed!').decode()
        with self.assertRaisesRegex(ValueError, 'missing or changed'):
            media.request_restore(self.client, self.store, self.key, self.video, 7)
        self.assertTrue(all(op == 'head' for op, _ in self.client.operations))

    def test_already_restoring_is_the_only_ignored_restore_error(self):
        for code in ('RestoreAlreadyInProgress', 'AccessDenied', 'NoSuchKey'):
            with patch.object(self.client, 'restore_object', side_effect=S3Error(code)):
                if code == 'RestoreAlreadyInProgress':
                    media.request_restore(self.client, self.store, self.key, self.video, 7)
                else:
                    with self.assertRaises(S3Error):
                        media.request_restore(self.client, self.store, self.key, self.video, 7)

    def test_download_checks_bytes_and_never_replaces_existing_files(self):
        path = self.root / 'download'
        self.client.corrupt_read = True
        with self.assertRaisesRegex(ValueError, 'content verification'):
            media.download(self.client, self.store, self.key, self.video, path, reserve=0)
        self.assertFalse(path.exists())
        path.write_bytes(b'keep this')
        with self.assertRaises(FileExistsError):
            media.download(self.client, self.store, self.key, self.video, path, reserve=0)
        self.assertEqual(path.read_bytes(), b'keep this')
        self.assertTrue(all(op == 'get' for op, _ in self.client.operations))

    def test_audit_lists_bound_store_without_object_heads(self):
        inventory = audit.MetadataReader(self.client).inventory(self.store)
        self.assertEqual(set(inventory), {self.key, self.base_key})
        self.assertEqual(self.client.operations, [('list', self.client.prefix)])
        self.assertEqual(inventory[self.key]['size'], len(self.body))
        with patch.object(self.client, 'list_objects_v2', return_value={'Contents': []}):
            with self.assertRaises(ValueError):
                audit.MetadataReader(self.client).inventory(self.store)

    def audit_fixture(self):
        snapshot = self.root / 'ledger'
        snapshot.mkdir()
        with closing(sqlite3.connect(snapshot / 'tar_delta_state.sqlite3')) as db:
            db.executescript('CREATE TABLE unit_state(unit_id,base_key,last_delta_key); CREATE TABLE file_index(unit_id,relpath,size,nfohash);')
        (snapshot / 'tarball_footprints.jsonl').write_text('')
        source = self.root / 'source'
        source.mkdir()
        for video in self.catalog['videos']:
            target = source / video['path']
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(self.body)
        catalog = self.catalog | {'units': [], 'objects': {self.key: self.video}}
        backup = SimpleNamespace(BASE_DIR=str(source), VIDEO_EXTENSIONS=('.mp4',), TAR_INCLUDE_EXTENSIONS=('.jpg',),
                                  collect_partitioned_tar_units=lambda path: [])
        return snapshot, source, catalog, backup

    def test_audit_resolves_shared_object_paths_and_distinguishes_pending_deletion(self):
        snapshot, source, catalog, backup = self.audit_fixture()
        inventory = audit.MetadataReader(self.client).inventory(self.store)
        counts = audit.audit_local(backup, snapshot, catalog, inventory, self.root / 'issues.jsonl')
        self.assertEqual(counts['local_videos_matching_remote_size'], 2)
        self.assertEqual(counts['published_object_references'], 1)
        self.assertEqual(counts.get('local_videos_missing_remote', 0), 0)
        (source / 'creator/first.mp4').unlink()
        counts = audit.audit_local(backup, snapshot, catalog, inventory, self.root / 'issues.jsonl')
        self.assertEqual(counts['published_videos_pending_deletion'], 1)
        self.assertEqual(counts.get('remote_videos_without_current_local_path', 0), 0)

    def test_audit_only_heads_cold_objects_when_explicitly_requested(self):
        snapshot, source, catalog, backup = self.audit_fixture()
        args = ['--remote', '--base-dir', str(source), '--ledger-dir', str(snapshot)]
        with (patch('boto3.client', return_value=self.client),
              patch.object(audit, 'snapshot_ledgers', return_value=snapshot),
              patch.object(audit, 'load_module', side_effect=[backup, restore, backup, restore]),
              patch.object(audit.MetadataReader, 'manifest', return_value=(json.dumps(catalog).encode(), None)),
              patch('sys.stdout', new=io.StringIO())):
            audit.main(args + ['--output-dir', str(self.root / 'normal-report')])
            self.assertEqual(self.client.operations, [('list', self.client.prefix)])
            self.client.operations.clear()
            audit.main(args + ['--output-dir', str(self.root / 'checksum-report'), '--media-checksums'])
            self.assertEqual(self.client.operations, [('list', self.client.prefix), ('head', self.client.prefix + self.key)])
            summary = json.loads((self.root / 'checksum-report/summary.json').read_text())
            self.assertEqual(summary['media_archive'], {'coverage': 'full-object-checksums', 'objects_verified': 1})


if __name__ == '__main__':
    unittest.main()
