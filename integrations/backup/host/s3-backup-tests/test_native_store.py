"""Native publication/restore contracts against real bundles and a fake S3."""

from contextlib import closing
import copy
import hashlib
import io
from pathlib import Path
import sqlite3
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from fake_s3 import FakeS3, S3Error
from native_backup import CONFIG_FORMAT, NativeBackupSession, copy_ledger
from native_store import MEDIA_FORMAT, NativeStore, PREFIX, selection_digest, media_selection
from stash_archive.bundle import FORMAT, export_archive, import_archive, iter_artifacts
from stash_archive.filesystem_boundary import FORMAT as BOUNDARY_FORMAT
from stash_archive.server_checkpoint import FORMAT as CHECKPOINT_FORMAT, COVERAGE
from stash_archive.storage import InvalidArchive, json_bytes, store_file
from stash_archive.verification import verify_archive_proofs


class NativeStoreTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cloud = self.root / 'cloud'
        self.cloud.mkdir()
        self.s3 = FakeS3(self.cloud)
        self.store = NativeStore(self.s3, 'metadata', 'prefix')
        self.catalog = {'format': 's3-log-backup', 'version': 3, 'run_id': 'test', 'created_epoch': 1,
                        'videos': [{'key': 'creator/video.mp4', 'size': 3}], 'units': []}
        self.checkpoint = str(uuid.uuid4())
        self.database = self.root / 'native.sqlite'
        with closing(sqlite3.connect(self.database)) as db:
            db.executescript(f"""
                CREATE TABLE native_schema(singleton INTEGER PRIMARY KEY,lineage TEXT);
                INSERT INTO native_schema VALUES(1,'{FORMAT}');
                CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,dirty INTEGER);
                INSERT INTO schema_migrations VALUES(1000077,0);
                CREATE TABLE blobs(checksum TEXT PRIMARY KEY,blob BLOB);
                CREATE TABLE ingest_producers(uuid TEXT PRIMARY KEY);
            """)
        self.boundary = {'format': BOUNDARY_FORMAT, 'version': 1, 'uuid': self.checkpoint,
                         'request_sha256': '1' * 64, 'token': str(uuid.uuid4()),
                         'confirmed_at': '2026-10-05T00:00:00Z', 'details': {'fixture': 'retained-view'}}
        self.media = {'format': MEDIA_FORMAT, 'version': 1, 'checkpoint_uuid': self.checkpoint,
                      'filesystem_boundary_sha256': hashlib.sha256(json_bytes(self.boundary)).hexdigest(),
                      'selection_sha256': selection_digest(self.catalog), 'selection': media_selection(self.catalog)}
        components = []
        for role, name, body in [
            ('operating_state', 'filesystem-boundary.json', json_bytes(self.boundary)),
            ('media_manifest', 's3-media.json', json_bytes(self.media)),
            ('config', 'config.yml', b'database: library.sqlite\n'),
            ('config', 'runtime-overrides.yml', b'{}\n'),
            ('file_journal', 'deletions.zip', b'fixture recovery bytes'),
        ]:
            path = self.root / name
            path.write_bytes(body)
            components.append({'role': role, 'name': name, 'path': path})
        self.archive = self.root / 'archive'
        self.manifest = export_archive(self.database, self.archive, reserve=0, components=components)
        self.entries = list(iter_artifacts(self.archive, self.manifest))
        checkpoint = {'format': CHECKPOINT_FORMAT, 'version': 1, 'uuid': self.checkpoint, 'coverage': COVERAGE,
                      'created_at': '2026-10-05T00:00:00Z', 'request_sha256': self.boundary['request_sha256'],
                      'source_database_path': None, 'source_config_path': None, 'source_working_directory': None,
                      'committed_deletion_ids': [], 'components': [
                          {'role': e['role'], 'name': 'library.sqlite' if e['role'] == 'library' else e['name'],
                           'sha256': e['sha256'], 'bytes': e['size']} for e in self.entries if e['role'] != 'media_manifest']}
        path = self.root / 'server-checkpoint.json'
        path.write_bytes(json_bytes(checkpoint))
        entry = store_file(self.archive, path, reserve=0)
        entry.update(role='operating_state', name=path.name)
        self.entries.append(entry)
        body = b''.join(json_bytes(e) for e in self.entries)
        self.manifest['inventory'] = {'sha256': hashlib.sha256(body).hexdigest(), 'size': len(body),
                                      'count': len(self.entries), 'total_bytes': sum(e['size'] for e in self.entries)}
        (self.archive / 'artifacts.jsonl').write_bytes(body)
        (self.archive / 'manifest.json').write_bytes(json_bytes(self.manifest))
        library = self.entries[0]
        # The real Go schema verifier has its own interoperability gate. This
        # fixture exercises its protocol alongside real content/receipt restore.
        report = {'format': FORMAT + '.snapshot-verification', 'version': 1, 'lineage': FORMAT,
                  'schema_version': 1000077, 'sha256': library['sha256'], 'bytes': library['size'],
                  'database_verified': True, 'pending_file_deletions': 0, 'filesystem_recovery_verified': False}
        with patch('stash_archive.verification.validator_output', return_value=json_bytes(report)):
            self.proof = verify_archive_proofs(self.archive, native_validator=sys.executable,
                                                producer_origin='https://stash.example', reserve=0, temp_parent=self.root)

    def publish(self):
        return self.store.publish_archive(self.archive, self.proof, self.checkpoint, selection_digest(self.catalog))

    def test_publish_audit_download_restore_real_bundle(self):
        reference = self.publish()
        puts = [op for op in self.s3.operations if op[0] == 'put']
        self.assertEqual(puts[-1][1], 'prefix/' + reference['manifest']['key'])
        self.assertEqual(self.publish(), reference)
        self.assertEqual([op for op in self.s3.operations if op[0] == 'put'], puts)
        result = self.store.audit(reference, reserve=0)
        self.assertGreater(result['objects_verified'], 0)
        downloaded = self.root / 'downloaded'
        self.store.download(reference, downloaded, reserve=0)
        restored = self.root / 'restored'
        import_archive(downloaded, restored, reserve=0)
        self.assertEqual((restored / 'components/config/config.yml').read_bytes(), b'database: library.sqlite\n')
        self.assertEqual((restored / 'components/media_manifest/s3-media.json').read_bytes(), json_bytes(self.media))

    def test_missing_checksums_wrong_class_and_composite_rejected(self):
        value = self.store.put_bytes('object', b'some bytes')
        original = dict(self.s3.headers['prefix/object'])
        for change in ({'ChecksumSHA256': None}, {'ChecksumSHA256': 'wrong'}, {'ChecksumType': 'COMPOSITE'},
                       {'ContentLength': 0}, {'StorageClass': 'DEEP_ARCHIVE'}):
            with self.subTest(change=change):
                self.s3.headers['prefix/object'] = original | change
                with self.assertRaises(InvalidArchive):
                    self.store.put_bytes('object', b'some bytes')
        self.assertEqual(sum(op[0] == 'put' for op in self.s3.operations), 1)
        self.s3.headers['prefix/object'] = original
        self.assertEqual(self.store.read(value), b'some bytes')

    def test_lost_put_reply_verified_without_overwrite(self):
        self.s3.lost_reply.add('prefix/object')
        self.store.put_bytes('object', b'complete bytes')
        self.store.put_bytes('object', b'complete bytes')
        self.assertEqual(sum(op[0] == 'put' for op in self.s3.operations), 1)

    def test_denied_head_is_never_treated_as_absent(self):
        for code in ('403', 'AccessDenied', 'SlowDown'):
            self.s3.head_failure = code
            with self.assertRaises(S3Error):
                self.store.put_bytes('object', b'bytes')
        self.assertFalse(any(op[0] == 'put' for op in self.s3.operations))

    def test_mismatched_proof_or_selection_prevents_all_uploads(self):
        for field in ('native_snapshot', 'ingestion_receipts'):
            proof = copy.deepcopy(self.proof)
            proof[field]['archive_uuid'] = str(uuid.uuid4())
            with self.assertRaises(InvalidArchive):
                self.store.publish_archive(self.archive, proof, self.checkpoint, selection_digest(self.catalog))
        with self.assertRaises(InvalidArchive):
            self.store.publish_archive(self.archive, self.proof, self.checkpoint, '0' * 64)
        with self.assertRaises(InvalidArchive):
            self.store.publish_archive(self.archive, self.proof, str(uuid.uuid4()), selection_digest(self.catalog))
        proof = copy.deepcopy(self.proof)
        proof['native_snapshot']['sha256'] = '0' * 64
        with self.assertRaises(InvalidArchive):
            self.store.publish_archive(self.archive, proof, self.checkpoint, selection_digest(self.catalog))
        proof = copy.deepcopy(self.proof)
        proof['ingestion_receipts']['components'] = []
        with self.assertRaises(InvalidArchive):
            self.store.publish_archive(self.archive, proof, self.checkpoint, selection_digest(self.catalog))
        self.assertEqual(self.s3.operations, [])

    def test_audit_missing_object_and_corrupt_download_fail(self):
        reference = self.publish()
        self.s3.corrupt_read = True
        with self.assertRaises(InvalidArchive):
            self.store.download(reference, self.root / 'bad-download', reserve=0)
        self.s3.corrupt_read = False
        next((self.cloud / 'prefix' / PREFIX / 'objects').iterdir()).unlink()
        with self.assertRaises(InvalidArchive):
            self.store.audit(reference, reserve=0)

    def test_wal_ledger_snapshot_preserves_original_and_is_atomic(self):
        source, target = self.root / 'ledger.sqlite3', self.root / 'copy.sqlite3'
        with closing(sqlite3.connect(source)) as db:
            db.executescript('PRAGMA journal_mode=WAL; CREATE TABLE pending(value); INSERT INTO pending VALUES(1);')
            copy_ledger(source, target, 0)
            db.execute('INSERT INTO pending VALUES(2)')
            db.commit()
            copy_ledger(source, target, 0)
        with closing(sqlite3.connect(target)) as db:
            self.assertEqual(db.execute('SELECT value FROM pending').fetchall(), [(1,)])
        target.unlink()
        with patch('native_backup.require_space', side_effect=InvalidArchive('reserve')):
            with self.assertRaises(InvalidArchive):
                copy_ledger(source, target, 0)
        self.assertFalse(target.exists())

    def test_host_session_seals_before_exposing_retained_media(self):
        live = self.root / 'live'
        live.mkdir()
        key = self.root / 'key'
        key.write_text('fixture-key\n')
        config = {'format': CONFIG_FORMAT, 'version': 1, 'server': 'https://stash.example',
                  'api_key_file': str(key), 'state_directory': str(self.root / 'state'),
                  'artwork_sources': [str(self.root / 'originals')], 'components': [], 'worker_lock_roots': [],
                  'media': {'dataset': 'pool/library', 'guid': '123', 'mountpoint': str(self.root), 'relative_path': 'live'},
                  'producer_origin': 'https://stash.example', 'native_validator': sys.executable,
                  'recovery_roots': [], 'reserve_bytes': 0}
        path = self.root / 'host.json'
        path.write_bytes(json_bytes(config))
        view = Mock()
        view.root = live
        view.resolve.return_value = live
        view.verify.return_value = view
        order = []
        host = Mock()
        host.__enter__ = Mock(return_value=host)
        host.__exit__ = Mock(return_value=False)
        host.client.return_value.boundary_receipt = self.boundary
        host.prepare.return_value.seal.side_effect = lambda: order.append('sealed')
        with patch('native_backup.ArtworkPins') as pins, patch('native_backup.ZFSMedia') as media, \
             patch('native_backup.HostFilesystemCapture', return_value=host):
            def opened(boundary):
                self.assertEqual(order, ['sealed'])
                return view
            media.return_value.open_bound.side_effect = opened
            session = NativeBackupSession(path, 'fixture', live, 'metadata', '', self.s3)
        self.assertEqual(session.media_path, live)
        pins.assert_called_once()
        parts = host.prepare.call_args.args[2]
        self.assertEqual({part['name'] for part in parts}, {'host-backup.json', 'host-backup-api-key'})
        self.assertEqual(self.s3.operations, [])

    def test_host_publish_uses_bound_archive_and_failure_leaves_no_reference(self):
        session = NativeBackupSession.__new__(NativeBackupSession)
        session.root, session.store = self.root, self.store
        session.view = Mock()
        session.check_source = Mock()
        session.client = SimpleNamespace(request_id=self.checkpoint, boundary_bytes=json_bytes(self.boundary))
        session.reserve, session.validator_timeout = 0, 10
        session.validator, session.producer_origin = sys.executable, 'https://stash.example'
        with patch('native_backup.verify_archive_proofs', side_effect=InvalidArchive('native schema refused')):
            with self.assertRaisesRegex(InvalidArchive, 'native schema refused'):
                session.publish(self.catalog, [])
        self.assertEqual(self.s3.operations, [])
        self.assertFalse((self.root / 'publication.json').exists())
        with patch('native_backup.verify_archive_proofs', return_value=self.proof):
            reference = session.publish(self.catalog, [])
        self.assertEqual(reference['selection_sha256'], selection_digest(self.catalog))
        changed = copy.deepcopy(self.catalog)
        changed['videos'][0]['size'] += 1
        count = len(self.s3.operations)
        with self.assertRaises(InvalidArchive):
            session.publish(changed, [])
        self.assertEqual(len(self.s3.operations), count)

    def test_release_reclaims_only_run_objects_after_verified_master(self):
        reference = self.publish()
        session = NativeBackupSession.__new__(NativeBackupSession)
        session.root, session.archive, session.store = self.root, self.archive, self.store
        session.run_id, session.publication = 'test', reference
        session.view = SimpleNamespace(verify=lambda: None)
        session.client = session.pins = session.media = session.component_cache = object()
        ledger = self.root / 'ledger-copy'
        ledger.write_bytes(b'ledger')
        session.generated = [{'name': ledger.name, 'sha256': hashlib.sha256(b'ledger').hexdigest(), 'bytes': 6}]
        untouched = self.archive / 'objects' / 'unknown'
        untouched.write_bytes(b'not part of archive')
        body = json_bytes(dict(self.catalog, native_archive=reference))
        session.prepare_master(body)
        self.s3.lost_reply.add('prefix/current_manifest.json')
        session.commit_master(body)
        self.assertTrue(session.committed)
        with patch('native_backup.release_published_artwork') as artwork, patch('native_backup.release_published_media') as media, \
             patch('native_backup.release_published_components') as components:
            session.finish()
            session.finish()
            for call in (artwork, media, components):
                call.assert_called_once()
        self.assertEqual(list((self.archive / 'objects').iterdir()), [untouched])
        self.assertFalse(ledger.exists())
        self.assertTrue(self.database.exists())
        self.assertTrue((self.root / 'master.json').exists())


if __name__ == '__main__':
    unittest.main()
