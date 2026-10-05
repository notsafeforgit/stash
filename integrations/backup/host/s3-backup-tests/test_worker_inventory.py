"""Dependency closure, private values and capture/retry integration."""

import base64
from contextlib import closing
import hashlib
import json
from pathlib import Path
import sqlite3
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from fake_s3 import FakeS3
from native_backup import CONFIG_FORMAT, NativeBackupSession
from worker_inventory import FORMAT, collect, components_for_capture, verify_stage
from stash_archive.host_boundary import HostFilesystemCapture
from stash_archive.storage import InvalidArchive, json_bytes
from stash_ingest.outbox import Outbox


class WorkerInventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for name in ('media', 'locks', 'archives', 'cloud', 'originals'):
            (self.root / name).mkdir(mode=0o700)
        self.secret = 'private-website-value-never-in-report'
        self.cookies = self.root / 'cookies.txt'
        self.cookies.write_text(self.secret)
        self.helper = self.root / 'helper.py'
        self.helper.write_text('raise AssertionError("inventory must not execute helpers")\n')
        self.private = self.root / 'private.json'
        self.private.write_bytes(json_bytes({'cookies': str(self.cookies), 'password': self.secret,
                                            'headers': {'archive': self.secret}}))
        self.outbox = self.root / 'queue.sqlite'
        Outbox(self.outbox, 'https://stash.example', str(uuid.uuid4())).close()
        for name in ('reddit.sqlite3', 'twitter.sqlite3', 'instagram.sqlite3.bak'):
            with closing(sqlite3.connect(self.root / 'archives' / name)) as db:
                db.execute('CREATE TABLE archive(entry TEXT PRIMARY KEY)')
                db.execute('INSERT INTO archive VALUES(?)', (name,))
                db.commit()
        self.profile = self.root / 'profile.json'
        binding = lambda name: '${stash:' + name + '}'
        directory = lambda name: {'path': str(self.root / name),
                                  'identity': [(self.root / name).stat().st_dev, (self.root / name).stat().st_ino]}
        self.document = {
            'schema': 'stash-gallery-worker-v1', 'root': dict(directory('media'), uuid=str(uuid.uuid4())),
            'locks': directory('locks'), 'bindings': {
                'downloads': {'kind': 'path', 'path': './archives'},
                'helper': {'kind': 'asset', 'path': './helper.py', 'sha256': hashlib.sha256(self.helper.read_bytes()).hexdigest()},
                **{name: {'kind': 'private', 'file': './private.json', 'pointer': '/' + name}
                   for name in ('cookies', 'password', 'headers')},
            },
            'gallery': {'extractor': {'archive': [binding('downloads'), '{category}.sqlite3'],
                                     'cookies': binding('cookies'), 'password': binding('password'),
                                     'headers': binding('headers')},
                        'postprocessor': {'function': binding('helper') + ':process'}, 'netrc': False},
        }
        self.spec = {'name': 'host', 'profile': str(self.profile), 'home': str(self.root),
                     'working_directory': str(self.root / 'media'), 'outboxes': [str(self.outbox)]}
        self.inventory = self.root / 'workers.json'
        self.save()

    def save(self, workers=None):
        self.profile.write_bytes(json_bytes(self.document))
        self.inventory.write_bytes(json_bytes({'format': FORMAT, 'version': 1, 'workers': workers or [self.spec]}))

    def test_closure_includes_private_path_targets_helpers_and_current_archives(self):
        report = collect(str(self.inventory))
        paths = {c['path']: c['role'] for c in report['components']}
        self.assertEqual(paths[str(self.cookies)], 'config')
        self.assertEqual(paths[str(self.private)], 'config')
        self.assertEqual(paths[str(self.helper)], 'config')
        self.assertEqual(paths[str(self.outbox)], 'producer_outbox')
        self.assertEqual(paths[str(self.profile)], 'worker_profile')
        self.assertEqual(sum(role == 'download_archive' for role in paths.values()), 2)
        self.assertEqual(report['worker_lock_roots'], [str(self.root / 'locks')])
        self.assertNotIn(self.secret, json.dumps(report))
        self.assertNotIn(str(self.root / 'archives/instagram.sqlite3.bak'), paths)

    def test_container_mounts_and_duplicate_profiles_share_one_dependency(self):
        virtual = '/container'
        translated = json.loads(json.dumps(self.document).replace(str(self.root), virtual))
        container_profile = self.root / 'container.json'
        container_profile.write_bytes(json_bytes(translated))
        worker = dict(self.spec, name='container', profile=virtual + '/container.json', home=virtual,
                      working_directory=virtual + '/media', outboxes=[virtual + '/queue.sqlite'],
                      path_mappings=[{'from': virtual, 'to': str(self.root)}])
        self.save([self.spec, worker])
        report = collect(str(self.inventory))
        paths = [c['path'] for c in report['components']]
        self.assertEqual(paths.count(str(self.private)), 1)
        self.assertEqual(paths.count(str(self.cookies)), 1)
        self.assertEqual(paths.count(str(self.outbox)), 1)
        self.assertEqual(report['worker_lock_roots'], [str(self.root / 'locks')])

    def test_private_layers_and_environment_sources_retain_files_without_values(self):
        second = self.root / 'second.json'
        second.write_bytes(json_bytes({'headers': {'additional': self.secret}, 'cookie_path': str(self.cookies)}))
        self.document['bindings']['headers'] = {'kind': 'private', 'sources': [
            {'file': './private.json', 'pointer': '/headers'}, {'file': './second.json', 'pointer': '/headers'}]}
        self.document['bindings']['cookies'] = {'kind': 'private', 'env': 'COOKIE_PATH'}
        self.save()
        with self.assertRaisesRegex(InvalidArchive, 'environment binding'):
            collect(str(self.inventory))
        self.spec['environment'] = {'COOKIE_PATH': {'file': './second.json', 'pointer': '/cookie_path'}}
        self.save()
        report = collect(str(self.inventory))
        self.assertIn(str(second), report['read_checksums'])
        self.assertNotIn(self.secret, json.dumps(report))

    def test_archive_enumeration_includes_new_service_on_next_capture(self):
        before = collect(str(self.inventory))
        with closing(sqlite3.connect(self.root / 'archives/bluesky.sqlite3')) as db:
            db.execute('CREATE TABLE archive(entry TEXT PRIMARY KEY)')
        after = collect(str(self.inventory))
        self.assertEqual(len(after['components']), len(before['components']) + 1)
        self.assertNotEqual(before['archive_sets'], after['archive_sets'])

    def test_metadata_profiles_retain_access_files_without_media_or_archive_claims(self):
        for schema in ('stash-gallery-enrichment-v1', 'stash-gallery-discovery-v1', 'stash-gallery-discovery-detail-v1'):
            with self.subTest(schema=schema):
                self.document = {'schema': schema, 'source_category': 'reddit',
                                 'bindings': {'cookies': {'kind': 'private', 'file': './private.json', 'pointer': '/cookies'}},
                                 'gallery': {'extractor': {'reddit': {'cookies': '${stash:cookies}'}}}}
                self.save()
                report = collect(str(self.inventory))
                self.assertIn(str(self.cookies), {c['path'] for c in report['components']})
                self.assertFalse(report['worker_lock_roots'])
                self.assertFalse(report['archive_sets'])

    def test_unreviewed_asset_is_not_treated_as_a_verified_helper(self):
        self.document['bindings']['helper']['sha256'] = None
        self.save()
        with self.assertRaisesRegex(InvalidArchive, 'reviewed SHA-256'):
            collect(str(self.inventory))

    def test_missing_cookie_changed_helper_and_symlink_archive_fail(self):
        self.cookies.unlink()
        with self.assertRaises(FileNotFoundError):
            collect(str(self.inventory))
        self.cookies.write_text(self.secret)
        original = self.helper.read_bytes()
        self.helper.write_text('changed helper')
        with self.assertRaisesRegex(InvalidArchive, 'reviewed digest'):
            collect(str(self.inventory))
        self.helper.write_bytes(original)
        (self.root / 'archives/unsafe.sqlite3').symlink_to(self.private)
        with self.assertRaises(InvalidArchive):
            collect(str(self.inventory))

    def test_unheld_worker_barrier_and_conflicting_explicit_role_fail(self):
        report = collect(str(self.inventory))
        with self.assertRaisesRegex(InvalidArchive, 'publication barrier'):
            components_for_capture(report, [], [])
        wrong = [{'role': 'config', 'name': 'wrong', 'path': str(self.outbox)}]
        with self.assertRaisesRegex(InvalidArchive, 'conflicts'):
            components_for_capture(report, wrong, [self.root / 'locks'])

    def test_inspected_profiles_must_match_captured_bytes(self):
        report = collect(str(self.inventory))
        entries = [{'source_path': base64.b64encode(c['path'].encode()).decode(),
                    'role': c['role'], 'sha256': hashlib.sha256(Path(c['path']).read_bytes()).hexdigest()}
                   for c in report['components']]
        stage = SimpleNamespace(record={'components': entries})
        verify_stage(report, stage)
        target = next(c for c in entries if c['source_path'] == base64.b64encode(str(self.private).encode()).decode())
        target['sha256'] = '0' * 64
        with self.assertRaisesRegex(InvalidArchive, 'between inspection and capture'):
            verify_stage(report, stage)

    def host_config(self):
        key = self.root / 'api-key'
        key.write_text('test-application-key')
        config = {'format': CONFIG_FORMAT, 'version': 1, 'server': 'https://stash.example',
                  'api_key_file': str(key), 'state_directory': str(self.root / 'state'),
                  'artwork_sources': [str(self.root / 'originals')], 'components': [],
                  'worker_lock_roots': [str(self.root / 'locks')], 'worker_inventory': str(self.inventory),
                  'media': {'dataset': 'pool/library', 'guid': '123', 'mountpoint': str(self.root), 'relative_path': 'media'},
                  'producer_origin': 'https://stash.example', 'native_validator': sys.executable,
                  'recovery_roots': [], 'reserve_bytes': 0}
        path = self.root / 'host.json'
        path.write_bytes(json_bytes(config))
        return path

    def test_host_captures_real_databases_and_retries_original_inventory(self):
        config = self.host_config()
        cloud = FakeS3(self.root / 'cloud')
        with patch('native_backup.ArtworkPins'), patch('native_backup.ZFSMedia'), \
             patch('stash_archive.server_checkpoint.ServerCheckpoint.seal', side_effect=OSError('lost server reply')):
            with self.assertRaisesRegex(OSError, 'lost server reply'):
                NativeBackupSession(config, 'first', self.root / 'media', 'metadata', '', cloud)
        saved = json.loads((self.root / 'state/active.json').read_bytes())
        stage = self.root / 'state/components' / saved['checkpoint_uuid']
        record = json.loads((stage / 'manifest.json').read_bytes())
        databases = [c for c in record['components'] if c['role'] in {'download_archive', 'producer_outbox'}]
        self.assertEqual(len(databases), 3)
        for component in databases:
            with closing(sqlite3.connect(stage / f"component-{component['index']:05d}")) as db:
                self.assertEqual(db.execute('PRAGMA integrity_check').fetchone(), ('ok',))
        self.inventory.unlink()
        self.profile.unlink()
        self.private.unlink()
        self.cookies.unlink()
        with patch('native_backup.ArtworkPins'), patch('native_backup.ZFSMedia'), \
             patch('native_backup.checkpoint_status', return_value={'state': 'sealed'}), \
             patch('worker_inventory.collect', side_effect=AssertionError('must not inspect live dependencies on retry')), \
             patch('stash_archive.server_checkpoint.ServerCheckpoint.seal', side_effect=OSError('retry sealed response')):
            with self.assertRaisesRegex(OSError, 'retry sealed response'):
                NativeBackupSession(config, 'second', self.root / 'media', 'metadata', '', cloud)
        self.assertEqual(json.loads((stage / 'manifest.json').read_bytes()), record)
        self.assertEqual(cloud.operations, [])

    def test_host_detects_config_changed_during_copy_before_server_capture(self):
        config = self.host_config()
        prepare = HostFilesystemCapture.prepare
        def changed(host, *args, **kwargs):
            self.private.write_bytes(json_bytes({'cookies': str(self.cookies), 'password': 'changed', 'headers': {}}))
            return prepare(host, *args, **kwargs)
        with patch('native_backup.ArtworkPins'), patch('native_backup.ZFSMedia'), \
             patch.object(HostFilesystemCapture, 'prepare', changed), \
             patch('stash_archive.server_checkpoint.ServerCheckpoint.seal') as seal:
            with self.assertRaisesRegex(InvalidArchive, 'between inspection and capture'):
                NativeBackupSession(config, 'first', self.root / 'media', 'metadata', '', FakeS3(self.root / 'cloud'))
        seal.assert_not_called()


if __name__ == '__main__':
    unittest.main()
