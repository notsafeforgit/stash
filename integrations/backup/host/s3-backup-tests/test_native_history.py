"""Backup history, retained-media reachability and bounded request contracts."""

import base64
import hashlib
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeS3, S3Error
from native_history import HISTORY, SnapshotHistory, policy, publication, publication_key, record_publication
from native_store import FORMAT, PREFIX, NativeStore, descriptor, selection_digest
from stash_archive.storage import InvalidArchive, json_bytes


class HistoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        cloud = self.root / 'cloud'
        cloud.mkdir()
        self.s3 = FakeS3(cloud)
        self.store = NativeStore(self.s3, 'metadata', 'nested/')
        self.cache = self.root / 'cache'
        self.history = SnapshotHistory(self.store, self.cache)
        self.cold = {'bucket': 'media', 'prefix': 'cold/'}

    def master(self, number, *, videos=None, archives=None, store=None):
        archive = str(uuid.UUID(int=number + 1))
        base = PREFIX + 'runs/' + archive + '/'
        videos = [f'creator/{number}.mp4'] if videos is None else videos
        archives = [] if archives is None else archives
        objects = {}
        for key in [*videos, *archives]:
            sha = hashlib.sha256(key.encode()).digest()
            objects[key] = {'size': 1, 'storage_class': 'DEEP_ARCHIVE', 'sha256': sha.hex(),
                            'checksum': {'algorithm': 'sha256', 'value': base64.b64encode(sha).decode()}}
        catalog = {'format': 's3-log-backup', 'version': 4, 'run_id': f'run-{number}', 'created_epoch': number + 1,
                   'media_store': store or self.cold, 'objects': objects,
                   'videos': [{'key': key, 'path': key, 'size': 1} for key in videos],
                   'units': [] if not archives else [{'unit_id': 'fixture', 'base_key': archives[0], 'deltas': archives[1:]}]}
        catalog['native_archive'] = {'format': FORMAT, 'version': 1, 'archive_uuid': archive,
            'checkpoint_uuid': str(uuid.UUID(int=number + 10000)), 'objects_prefix': PREFIX + 'objects/',
            'selection_sha256': selection_digest(catalog),
            **{kind: descriptor(base + name, b'fixture') for kind, name in
               [('manifest', 'manifest.json'), ('inventory', 'artifacts.jsonl'), ('verification', 'verification.json')]}}
        return json_bytes(catalog)

    def publish(self, number, **kwargs):
        body = self.master(number, **kwargs)
        record = publication(body)
        self.store.put_bytes(record['master']['key'], body)
        self.s3.put_object(Bucket=self.store.bucket, Key=self.store.prefix + 'current_manifest.json', Body=body,
                           ContentLength=len(body), StorageClass='STANDARD',
                           ChecksumSHA256=base64.b64encode(hashlib.sha256(body).digest()).decode())
        record_publication(self.store, body)
        return body

    def test_default_keeps_seven_successful_snapshots_and_all_pinned_references(self):
        bodies = [self.publish(n, archives=[f'tarballs/base/{n}.tar', 'tarballs/delta/shared.tar']) for n in range(10)]
        first = publication(bodies[0])['archive_uuid']
        result = self.history.retain(bodies[-1], self.cold, {'keep_last': 7, 'pins': [first]})
        self.assertEqual(len(result['retained']), 8)
        self.assertEqual(len(result['retired']), 2)
        for n in [0, *range(3, 10)]:
            self.assertIn(f'creator/{n}.mp4', result['protected_media'])
            self.assertIn(f'tarballs/base/{n}.tar', result['protected_media'])
        self.assertIn('tarballs/delta/shared.tar', result['protected_media'])
        self.assertNotIn('creator/1.mp4', result['protected_media'])
        self.assertEqual(policy()['keep_last'], 7)

    def test_unknown_or_retired_pin_never_changes_remote_retention(self):
        first, latest = self.publish(1), self.publish(2)
        old = publication(first)['archive_uuid']
        self.history.retain(latest, self.cold, {'keep_last': 1, 'pins': []})
        self.s3.operations.clear()
        for pin in (old, str(uuid.uuid4())):
            with self.assertRaisesRegex(InvalidArchive, 'Pinned backup'):
                self.history.retain(latest, self.cold, {'keep_last': 7, 'pins': [pin]})
        self.assertFalse(any(op[0] == 'put' for op in self.s3.operations))

    def test_cache_loss_reconstructs_exact_protection_from_remote_receipts(self):
        self.publish(1, videos=['shared.mp4', 'older.mp4'])
        latest = self.publish(2, videos=['shared.mp4', 'newer.mp4'])
        expected = self.history.retain(latest, self.cold)
        shutil.rmtree(self.cache)
        fresh = SnapshotHistory(self.store, self.cache)
        self.assertEqual(fresh.retain(latest, self.cold), expected)
        self.assertEqual(expected['protected_media'], {'shared.mp4', 'older.mp4', 'newer.mp4'})

    def test_full_pagination_and_retired_graph_pruning_preserve_small_receipts(self):
        self.s3.page_size = 2
        for n in range(5):
            body = self.publish(n)
            self.history.retain(body, self.cold, {'keep_last': 2, 'pins': []})
        records, retired = self.history.inspect()
        self.assertEqual(len(records), 5)
        self.assertEqual(len(retired), 3)
        self.assertEqual(len(list(self.cache.rglob('media-*.json'))), 2)
        self.assertEqual(len(list(self.cache.rglob('publications-*.json'))), 5)

    def test_same_receipt_bytes_with_new_remote_identity_refresh_the_cache(self):
        current = self.publish(1)
        expected = self.history.retain(current, self.cold)
        record = publication(current)
        body = json_bytes(record)
        self.s3.put_object(Bucket=self.store.bucket, Key=self.store.prefix + publication_key(record['archive_uuid']),
            Body=body, ContentLength=len(body), StorageClass='STANDARD',
            ChecksumSHA256=base64.b64encode(hashlib.sha256(body).digest()).decode())
        self.assertEqual(self.history.retain(current, self.cold), expected)
        self.s3.operations.clear()
        self.assertEqual(self.history.retain(current, self.cold), expected)
        self.assertFalse(any(op[0] in ('get', 'put') for op in self.s3.operations))

    def test_unchanged_history_uses_one_list_and_current_head_without_media_requests(self):
        for n in range(7):
            body = self.publish(n, videos=[f'creator/{k}.mp4' for k in range(1003)])
        self.history.retain(body, self.cold)
        self.s3.operations.clear()
        result = SnapshotHistory(self.store, self.cache).retain(body, self.cold)
        self.assertEqual(len(result['protected_media']), 1003)
        self.assertEqual(self.s3.operations, [('list', self.store.prefix + HISTORY), ('head', self.store.prefix + 'current_manifest.json')])

    def test_prepared_master_without_commit_receipt_is_not_a_successful_snapshot(self):
        unpublished = self.master(1)
        self.store.put_bytes(publication(unpublished)['master']['key'], unpublished)
        current = self.publish(2)
        result = self.history.retain(current, self.cold)
        self.assertEqual(result['protected_media'], {'creator/2.mp4'})
        self.assertEqual(len(result['retained']), 1)

    def test_lost_receipt_or_retirement_reply_reuses_immutable_records(self):
        old = self.publish(1)
        body = self.master(2)
        receipt_key = self.store.prefix + publication_key(publication(body)['archive_uuid'])
        self.s3.lost_reply.add(receipt_key)
        current = self.publish(2)
        expired_key = self.store.prefix + HISTORY + 'retirements/' + publication(old)['archive_uuid'] + '.json'
        self.s3.lost_reply.add(expired_key)
        expected = self.history.retain(current, self.cold, {'keep_last': 1, 'pins': []})
        self.s3.operations.clear()
        self.assertEqual(self.history.retain(current, self.cold, {'keep_last': 1, 'pins': []}), expected)
        self.assertFalse(any(op[0] == 'put' for op in self.s3.operations))

    def test_retirement_failure_returns_no_cleanup_authorization_and_retries(self):
        self.publish(1)
        latest = self.publish(2)
        def fail(key):
            if '/retirements/' in key:
                raise S3Error('AccessDenied')
        self.s3.before_put = fail
        with self.assertRaisesRegex(S3Error, 'AccessDenied'):
            self.history.retain(latest, self.cold, {'keep_last': 1, 'pins': []})
        self.assertFalse(self.history.inspect()[1])
        self.s3.before_put = lambda key: None
        self.assertEqual(len(self.history.retain(latest, self.cold, {'keep_last': 1, 'pins': []})['retired']), 1)

    def test_missing_master_or_different_cold_store_prevents_retirement(self):
        old = self.publish(1, store={'bucket': 'other', 'prefix': ''})
        latest = self.publish(2)
        self.s3.operations.clear()
        with self.assertRaisesRegex(InvalidArchive, 'different cold store'):
            self.history.retain(latest, self.cold)
        self.assertFalse(any(op[0] == 'put' for op in self.s3.operations))
        shutil.rmtree(self.cache)
        self.history = SnapshotHistory(self.store, self.cache)
        (self.s3.root / (self.store.prefix + publication(old)['master']['key'])).unlink()
        with self.assertRaisesRegex(InvalidArchive, 'missing'):
            self.history.retain(latest, self.cold)
        self.assertFalse(self.history.inspect()[1])

    def test_changed_current_pointer_refuses_retirement(self):
        self.publish(1)
        stale = self.publish(2)
        self.publish(3)
        with self.assertRaisesRegex(InvalidArchive, 'exact SHA-256'):
            self.history.retain(stale, self.cold, {'keep_last': 1, 'pins': []})
        self.assertFalse(self.history.inspect()[1])

    def test_missing_retained_media_in_complete_inventory_blocks_retirement(self):
        self.publish(1)
        self.publish(2)
        current = self.publish(3)
        with self.assertRaisesRegex(InvalidArchive, 'Retained backup media is missing'):
            self.history.retain(current, self.cold, {'keep_last': 2, 'pins': []},
                                available_media={'creator/3.mp4': {}})
        self.assertFalse(self.history.inspect()[1])

    def test_malformed_or_incomplete_history_listing_is_not_empty_history(self):
        current = self.publish(1)
        self.s3.list_failure = 'AccessDenied'
        with self.assertRaises(S3Error):
            self.history.retain(current, self.cold)
        self.s3.list_failure = None
        self.store.put_bytes(HISTORY + 'unexpected.json', b'{}\n')
        with self.assertRaisesRegex(InvalidArchive, 'Unknown backup history'):
            self.history.retain(current, self.cold)

    def test_changed_immutable_receipt_is_rejected_after_cache_reopen(self):
        current = self.publish(1)
        self.history.retain(current, self.cold)
        value = publication(current)
        value['created_epoch'] += 1
        body = json_bytes(value)
        self.s3.put_object(Bucket=self.store.bucket, Key=self.store.prefix + publication_key(value['archive_uuid']),
            Body=body, ContentLength=len(body), StorageClass='STANDARD',
            ChecksumSHA256=base64.b64encode(hashlib.sha256(body).digest()).decode())
        with self.assertRaisesRegex(InvalidArchive, 'Immutable backup history'):
            SnapshotHistory(self.store, self.cache).retain(current, self.cold)

    def test_corrupt_cached_media_graph_cannot_drop_protected_keys(self):
        current = self.publish(1)
        self.history.retain(current, self.cold)
        path, = list(self.cache.rglob('media-*.json'))
        path.write_bytes(path.read_bytes().replace(b'creator/1.mp4', b'creator/2.mp4'))
        with self.assertRaisesRegex(InvalidArchive, 'cache is corrupt'):
            self.history.retain(current, self.cold)

    def test_current_is_counted_when_its_original_capture_predates_another_snapshot(self):
        self.publish(2)
        latest = self.publish(1)
        result = self.history.retain(latest, self.cold, {'keep_last': 1, 'pins': []})
        self.assertEqual(result['retained'], [publication(latest)['archive_uuid']])

    def test_invalid_retention_policy_never_defaults_to_expiring_everything(self):
        for value in ({}, {'keep_last': 0, 'pins': []}, {'keep_last': True, 'pins': []},
                      {'keep_last': 7, 'pins': ['unknown']}, {'keep_last': 366, 'pins': []}):
            with self.subTest(value=value), self.assertRaises(InvalidArchive):
                policy(value)


if __name__ == '__main__':
    unittest.main()
