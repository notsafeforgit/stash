"""Reachability, expiration/reuse, interrupted cleanup and request costs."""

from contextlib import closing
import base64
import hashlib
import io
import json
from pathlib import Path
import shutil
import sqlite3
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import test_native_store as fixtures
from fake_s3 import FakeS3, S3Error
from native_cleanup import COMPLETED, NativeCleanup, lifecycle_rules
from native_history import FORMAT, HISTORY, SnapshotHistory, publication, publication_key, record_publication
from native_store import NativeStore, PREFIX, archive_objects, descriptor
from native_tags import RETIRED_TAG, reconcile
from object_receipts import APPLICATION_ID, ObjectReceipts, identity, inventory
from stash_archive.storage import InvalidArchive, json_bytes


class CleanupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        cloud = self.root / 'cloud'
        cloud.mkdir()
        self.s3 = FakeS3(cloud)
        self.store = NativeStore(self.s3, 'metadata', 'nested/', receipts_path=self.root / 'receipts.sqlite3')
        self.s3.lifecycle = {'Rules': lifecycle_rules(self.store.prefix)}
        self.history = SnapshotHistory(self.store, self.root / 'history')
        self.cleanup = NativeCleanup(self.history, reserve=0)

    def publish(self, number):
        case = fixtures.NativeStoreTests('test_publish_audit_download_restore_real_bundle')
        case.setUp()
        self.addCleanup(case.doCleanups)
        case.store = self.store
        reference = case.publish()
        body = json_bytes(case.catalog | {'run_id': 'run-' + str(number), 'created_epoch': number,
                                         'native_archive': reference})
        record = publication(body)
        self.store.put_bytes(record['master']['key'], body)
        value = descriptor('current_manifest.json', body)
        self.s3.put_object(Bucket=self.store.bucket, Key=self.store.prefix + value['key'], Body=body,
                          ContentLength=len(body), StorageClass='STANDARD',
                          ChecksumSHA256=base64.b64encode(bytes.fromhex(value['sha256'])).decode())
        record_publication(self.store, body)
        return case, body

    def retire(self, old, current):
        record, latest = publication(old), publication(current)
        self.store.put_bytes(HISTORY + 'retirements/' + record['archive_uuid'] + '.json', json_bytes({
            'format': FORMAT + '.retirement', 'version': 1, 'archive_uuid': record['archive_uuid'],
            'publication': descriptor(publication_key(record['archive_uuid']), json_bytes(record)),
            'retired_by': latest['archive_uuid']}))

    def chunks(self, case):
        return {PREFIX + 'objects/' + sha + '.gz': size for sha, size in archive_objects(case.archive, case.manifest).items()}

    def retired_keys(self):
        return {key for key, tags in self.s3.tags.items() if tags.get(RETIRED_TAG) == 'true'}

    def rebuild_caches(self):
        shutil.rmtree(self.root / 'history')
        self.store.receipts_path.unlink(missing_ok=True)
        self.history = SnapshotHistory(self.store, self.root / 'history')
        self.cleanup = NativeCleanup(self.history, reserve=0)

    def test_all_retained_snapshots_protect_shared_objects_unknown_uploads_are_untouched(self):
        first, old = self.publish(1)
        second, _ = self.publish(2)
        third, current = self.publish(3)
        self.retire(old, current)
        orphan = PREFIX + 'objects/' + hashlib.sha256(b'orphan').hexdigest() + '.gz'
        self.store.put_bytes(orphan, b'orphan')
        result = self.cleanup.run(current)
        protected = self.chunks(second).keys() | self.chunks(third).keys()
        unique = self.chunks(first).keys() - protected
        self.assertTrue(unique)
        self.assertTrue(self.chunks(first).keys() & protected)
        self.assertTrue({self.store.prefix + key for key in unique} <= self.retired_keys())
        self.assertFalse({self.store.prefix + key for key in protected | {orphan}} & self.retired_keys())
        self.assertEqual(result['retired_snapshots'], 1)
        self.assertFalse(any(op[0] in ('delete', 'copy', 'restore') for op in self.s3.operations))

    def test_expired_metadata_and_cache_loss_do_not_lose_cleanup_completion(self):
        _, old = self.publish(1)
        _, current = self.publish(2)
        self.retire(old, current)
        self.cleanup.run(current)
        for key in self.retired_keys():
            (self.s3.root / key).unlink()
        self.rebuild_caches()
        self.s3.operations.clear()
        self.cleanup.run(current)
        self.assertFalse(any(op[0] == 'put-tags' for op in self.s3.operations))
        self.assertEqual(len(list(self.history.cache.glob('native-*.json'))), 1)

    def test_reused_chunk_cancels_retirement_preserves_other_tags_and_verifies_presence(self):
        first, old = self.publish(1)
        second, current = self.publish(2)
        self.retire(old, current)
        self.cleanup.run(current)
        key = next(iter(self.chunks(first).keys() - self.chunks(second).keys()))
        full_key = self.store.prefix + key
        self.s3.tags[full_key]['owner'] = 'archive'
        sha = Path(key).stem
        path = first.archive / 'objects' / (sha + '.gz')
        with closing(ObjectReceipts(self.store.receipts_path, self.store.bucket)) as receipts:
            listed = inventory(self.s3, self.store.bucket, self.store.prefix + PREFIX + 'objects/')
            self.s3.lost_tag_reply.add(full_key)
            self.store.put_file(key, path, sha, path.stat().st_size, receipts=receipts, listed=listed)
        self.assertEqual(self.s3.tags[full_key], {'owner': 'archive'})
        self.s3.lost_tag_reply.clear()
        self.cleanup.run(current)
        self.assertNotIn(full_key, self.retired_keys())

    def test_expiration_winning_tag_removal_race_reuploads_original_bytes(self):
        body = b'encoded chunk'
        sha = hashlib.sha256(body).hexdigest()
        key = PREFIX + 'objects/' + sha + '.gz'
        full_key = self.store.prefix + key
        self.store.put_bytes(key, body)
        self.s3.tags[full_key] = {RETIRED_TAG: 'true'}
        self.s3.before_tags = lambda candidate: (self.s3.root / candidate).unlink()
        self.store.put_bytes(key, body)
        self.assertEqual((self.s3.root / full_key).read_bytes(), body)
        self.assertNotIn(full_key, self.retired_keys())

    def test_tag_error_invalidates_cached_live_state_before_request_and_blocks_reuse(self):
        body = b'encoded chunk'
        value = descriptor(PREFIX + 'objects/' + hashlib.sha256(body).hexdigest() + '.gz', body)
        full_key = self.store.prefix + value['key']
        with closing(ObjectReceipts(self.store.receipts_path, self.store.bucket)) as receipts:
            self.store.put(value['key'], io.BytesIO(body), value['sha256'], len(body), receipts=receipts)
            listed = inventory(self.s3, self.store.bucket, self.store.prefix + PREFIX + 'objects/')
            def lose_verification(key):
                with closing(sqlite3.connect(self.store.receipts_path)) as independent:
                    self.assertEqual(independent.execute('SELECT tag_state FROM receipts WHERE key=?', (key,)).fetchone(), ('unknown',))
                self.s3.tags[key] = {RETIRED_TAG: 'true'}
                raise S3Error('AccessDenied')
            self.s3.before_tags = lose_verification
            with patch.object(self.s3, 'get_object_tagging', side_effect=[{'TagSet': []}, S3Error('AccessDenied')]):
                with self.assertRaises(S3Error):
                    reconcile(self.store, value['key'], value['sha256'], len(body), 'retired', receipts=receipts, listed=listed)
        with closing(ObjectReceipts(self.store.receipts_path, self.store.bucket)) as receipts:
            with self.assertRaises(S3Error):
                self.store.put(value['key'], io.BytesIO(body), value['sha256'], len(body), receipts=receipts, listed=listed)
        self.assertEqual(self.s3.tags[full_key], {RETIRED_TAG: 'true'})

    def test_failed_chunk_tagging_keeps_inventory_and_resumes_original_retirement(self):
        _, old = self.publish(1)
        _, current = self.publish(2)
        self.retire(old, current)
        def denied(key):
            if '/objects/' in key:
                raise S3Error('AccessDenied')
        self.s3.before_tags = denied
        with self.assertRaises(S3Error):
            self.cleanup.run(current)
        self.assertFalse(self.retired_keys())
        self.assertFalse(any(key.startswith(self.store.prefix + COMPLETED) for key in self.s3.headers))
        self.s3.before_tags = lambda key: None
        self.cleanup.run(current)
        self.assertTrue(self.retired_keys())

    def test_completed_chunks_survive_interruption_and_lost_metadata_tag_reply(self):
        _, old = self.publish(1)
        _, current = self.publish(2)
        self.retire(old, current)
        record = publication(old)
        key = self.store.prefix + record['master']['key']
        def stop_metadata(candidate):
            if candidate == key:
                raise S3Error('AccessDenied')
        self.s3.before_tags = stop_metadata
        with self.assertRaises(S3Error):
            self.cleanup.run(current)
        self.assertTrue(any(k.startswith(self.store.prefix + COMPLETED) for k in self.s3.headers))
        self.s3.before_tags = lambda key: None
        self.s3.lost_tag_reply.add(key)
        self.rebuild_caches()
        self.s3.operations.clear()
        self.cleanup.run(current)
        self.assertFalse(any(op[0] == 'put-tags' and '/objects/' in op[1] for op in self.s3.operations))

    def test_policy_or_versioning_cannot_be_assumed_and_never_changes_cloud_policy(self):
        _, current = self.publish(1)
        self.s3.operations.clear()
        self.s3.lifecycle = {'Rules': []}
        with self.assertRaisesRegex(InvalidArchive, 'lifecycle differs'):
            self.cleanup.run(current)
        self.s3.lifecycle = {'Rules': lifecycle_rules(self.store.prefix)}
        with patch.object(self.s3, 'get_bucket_versioning', side_effect=S3Error('AccessDenied')):
            with self.assertRaisesRegex(InvalidArchive, 'readable, reviewed'):
                self.cleanup.run(current)
        self.assertFalse(any(op[0] in ('put', 'put-tags') for op in self.s3.operations))

    def test_missing_retained_object_prevents_any_retirement(self):
        _, old = self.publish(1)
        latest, current = self.publish(2)
        self.retire(old, current)
        key = next(iter(self.chunks(latest)))
        (self.s3.root / (self.store.prefix + key)).unlink()
        with self.assertRaisesRegex(InvalidArchive, 'missing from the complete inventory'):
            self.cleanup.run(current)
        self.assertFalse(self.retired_keys())

    def test_unchanged_cleanup_uses_listings_and_one_current_head_without_object_sweeps(self):
        _, old = self.publish(1)
        _, current = self.publish(2)
        self.retire(old, current)
        self.cleanup.run(current)
        self.cleanup.run(current)  # Cache the new small completion receipt.
        self.s3.operations.clear()
        self.cleanup.run(current)
        self.assertEqual([op for op in self.s3.operations if op[0] == 'head'],
                         [('head', self.store.prefix + 'current_manifest.json')])
        self.assertFalse(any(op[0] in ('get', 'get-tags', 'put-tags', 'put') for op in self.s3.operations))

    def test_schema_one_upload_evidence_migrates_with_unknown_tag_state(self):
        body = b'old verified chunk'
        sha = hashlib.sha256(body).hexdigest()
        key = PREFIX + 'objects/' + sha + '.gz'
        self.store.put_bytes(key, body)
        full_key = self.store.prefix + key
        with closing(sqlite3.connect(self.store.receipts_path)) as db:
            db.executescript(f'''PRAGMA application_id={APPLICATION_ID}; PRAGMA user_version=1;
                CREATE TABLE receipts(bucket TEXT, key TEXT, sha256 TEXT, size INTEGER, etag TEXT,
                    modified TEXT, storage TEXT, PRIMARY KEY(bucket,key)) WITHOUT ROWID;''')
            db.execute('INSERT INTO receipts VALUES (?,?,?,?,?,?,?)',
                       (self.store.bucket, full_key, sha, *identity(self.s3.headers[full_key])))
            db.commit()
        self.store.receipts_path.chmod(0o600)
        self.s3.tags[full_key] = {RETIRED_TAG: 'true'}
        self.s3.operations.clear()
        with closing(ObjectReceipts(self.store.receipts_path, self.store.bucket)) as receipts:
            listed = inventory(self.s3, self.store.bucket, self.store.prefix + PREFIX + 'objects/')
            self.store.put(key, io.BytesIO(body), sha, len(body), receipts=receipts, listed=listed)
        self.assertNotIn(full_key, self.retired_keys())
        self.assertIn(('get-tags', full_key), self.s3.operations)
        self.assertFalse(any(op[0] == 'put' for op in self.s3.operations))

    def local_run(self, case, body):
        record = publication(body)
        runs = self.root / 'state' / 'runs'
        runs.mkdir(mode=0o700, parents=True, exist_ok=True)
        root = runs / record['run_id']
        root.mkdir(mode=0o700)
        (root / 'archive').mkdir(mode=0o700)
        for name in ('master.json', 'prepared-master.json'):
            (root / name).write_bytes(body)
        catalog = json.loads(body)
        del catalog['native_archive']
        (root / 'catalog.json').write_bytes(json_bytes(catalog))
        shutil.copyfile(case.archive / 'artifacts.jsonl', root / 'archive' / 'artifacts.jsonl')
        identity = {'run_id': record['run_id'], 'checkpoint_uuid': record['native_archive']['checkpoint_uuid'],
                    'bucket': self.store.bucket, 'prefix': self.store.prefix}
        finished = {'master_sha256': record['master']['sha256'], 'publication': record['native_archive']}
        for name, value in [('identity.json', identity), ('finished.json', finished), ('released.json', finished)]:
            (root / name).write_bytes(json_bytes(value))
        return runs, root, record

    def test_local_retirement_keeps_identity_unknown_files_and_resumes_after_unlink(self):
        case, body = self.publish(1)
        runs, root, record = self.local_run(case, body)
        (root / 'unknown.bin').write_bytes(b'preserve')
        with patch('native_cleanup.sync_directory', side_effect=OSError('crash after unlink')):
            with self.assertRaises(OSError):
                self.cleanup.prune_local(runs, record)
        self.assertEqual(self.cleanup.prune_local(runs, record), 0)
        self.assertFalse((root / 'master.json').exists())
        self.assertFalse((root / 'archive' / 'artifacts.jsonl').exists())
        self.assertEqual((root / 'unknown.bin').read_bytes(), b'preserve')
        self.assertEqual({path.name for path in root.glob('*.json')},
                         {'identity.json', 'finished.json', 'released.json', 'local-retirement.json'})

    def test_local_retirement_preserves_active_or_unfinished_attempts_and_changed_files(self):
        case, body = self.publish(1)
        runs, root, record = self.local_run(case, body)
        active = runs.parent / 'active.json'
        active.write_bytes(json_bytes({'run_id': record['run_id']}))
        self.assertEqual(self.cleanup.prune_local(runs, record), 0)
        active.unlink()
        finished = (root / 'finished.json').read_bytes()
        (root / 'finished.json').unlink()
        self.assertEqual(self.cleanup.prune_local(runs, record), 0)
        (root / 'finished.json').write_bytes(finished)
        (root / 'master.json').write_bytes(b'changed')
        with self.assertRaises(InvalidArchive):
            self.cleanup.prune_local(runs, record)
        self.assertEqual((root / 'master.json').read_bytes(), b'changed')

    def test_local_cleanup_only_runs_for_successfully_retired_snapshots(self):
        first, old = self.publish(1)
        latest, current = self.publish(2)
        runs, old_root, _ = self.local_run(first, old)
        _, current_root, _ = self.local_run(latest, current)
        self.retire(old, current)
        result = self.cleanup.run(current, runs=runs)
        self.assertGreater(result['local_bytes_reclaimed'], 0)
        self.assertFalse((old_root / 'master.json').exists())
        self.assertEqual((current_root / 'master.json').read_bytes(), current)

    def test_versioned_tag_updates_target_the_checked_version(self):
        body = b'chunk'
        key = PREFIX + 'objects/' + hashlib.sha256(body).hexdigest() + '.gz'
        value = self.store.put_bytes(key, body)
        self.s3.headers[self.store.prefix + key]['VersionId'] = 'version-123'
        self.s3.versioning = {'Status': 'Enabled'}
        with patch.object(self.s3, 'put_object_tagging', wraps=self.s3.put_object_tagging) as tagging:
            reconcile(self.store, key, value['sha256'], len(body), 'retired')
            self.assertEqual(tagging.call_args.kwargs['VersionId'], 'version-123')
