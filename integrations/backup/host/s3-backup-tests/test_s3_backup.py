"""Regression tests: every cloud boundary is mocked; never contact AWS/rclone."""
import argparse
import importlib.util
import io
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import unittest
import uuid
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import s3_restore_performer as restore
import s3_test_restore as restore_test
from fake_s3 import FakeS3
from native_backup import NativeBackupSession
from native_store import FORMAT, PREFIX, NativeStore, selection_digest, validate_reference
from stash_archive.storage import json_bytes


class PolicyNativeSession(NativeBackupSession):
    """Media-policy fixture: capture/packing separately tested with real bundles.

    Uses the real immutable/master publication methods and checksum transport.
    """
    def __init__(self, fixture):
        self.fixture = fixture
        self.live_media = self.media_path = fixture.source
        self.root = fixture.root / "native-runs" / fixture.m.RUN_ID
        self.root.mkdir(parents=True)
        self.run_id, self.created_epoch = fixture.m.RUN_ID, fixture.m.current_epoch
        self.view = SimpleNamespace(verify=lambda: None)
        self.store = NativeStore(fixture.native_s3, "metadata")
        self.committed = False
        self.publication = None

    def check_source(self):
        pass

    def publish(self, catalog, ledger_paths):
        archive = str(uuid.uuid4())
        base = PREFIX + "runs/" + archive + "/"
        self.publication = validate_reference({
            "format": FORMAT, "version": 1, "archive_uuid": archive, "checkpoint_uuid": str(uuid.uuid4()),
            "selection_sha256": selection_digest(catalog), "objects_prefix": PREFIX + "objects/",
            **{name: self.store.put_bytes(base + filename, json_bytes({"fixture": name}))
               for name, filename in (("inventory", "artifacts.jsonl"), ("verification", "verification.json"), ("manifest", "manifest.json"))},
        })
        return self.publication

    def finish(self):
        assert self.committed
        self.fixture.native_finishes += 1


def import_backup():
    spec = importlib.util.spec_from_file_location('backup_under_test', ROOT / 's3_log_backup.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class BackupTests(unittest.TestCase):
    def setUp(self):
        # Catch any accidental escape from the fake transport, including thawing.
        self.no_process = patch('subprocess.run', side_effect=AssertionError('Real subprocess forbidden'))
        self.no_popen = patch('subprocess.Popen', side_effect=AssertionError('Real subprocess forbidden'))
        self.no_process.start()
        self.no_popen.start()
        self.addCleanup(self.no_process.stop)
        self.addCleanup(self.no_popen.stop)
        self.temp = tempfile.TemporaryDirectory(prefix='s3-backup-regression-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.m = import_backup()
        self.no_aws = patch.object(self.m.boto3, 'client', side_effect=AssertionError('AWS client forbidden'))
        self.no_aws.start()
        self.addCleanup(self.no_aws.stop)
        self.source = self.root / 'source'
        self.ledger = self.root / 'ledgers'
        self.archive = self.root / 'objects'
        self.standard = self.root / 'standard'
        for directory in (self.source, self.ledger, self.archive, self.standard):
            directory.mkdir()
        m = self.m
        m.BASE_DIR, m.LEDGER_DIR = str(self.source), str(self.ledger)
        m.TMP_BASE_DIR = str(self.root / 'runs')
        m.SOURCE_MOUNT = None
        m.RUN_LOCK_PATH = str(self.ledger / 'lock')
        for variable, name in {
            'TAR_DELTA_DB': 'state.sqlite3', 'TARBALL_FP_JSONL': 'fp.jsonl',
            'VIDEO_PREV_MANIFEST': 'previous.nul', 'TOMBSTONES_LEDGER': 'tombstones.txt',
            'VIDEO_TOMBSTONE_PROGRESS': 'progress.nul',
        }.items():
            setattr(m, variable, str(self.ledger / name))
        m.log = lambda *a, **kw: None
        m.assert_s3_access_ready = lambda **kw: None
        m.run_cmd = self.run_cmd
        m.ensure_s3_delete_marker = self.delete
        m.S3ObjectTagger = lambda *a, **kw: self
        m.tombstone_remote_accidental_nonvideo_objects = lambda **kwargs: None
        self.native_s3 = FakeS3(self.standard)
        self.native_finishes = 0
        m.open_native_session = lambda args: PolicyNativeSession(self)
        self.remote_metadata = {}
        m.remote_upload_matches = lambda key,size,name,value: (self.archive/key).is_file() and (self.archive/key).stat().st_size == size and self.remote_metadata.get(key,{}).get(name) == value
        m.remote_video_matches = lambda key,local,size,value: m.remote_upload_matches(key,size,"backup-source-signature",value)
        self.operations = []
        self.tagged = set()
        self.failure = None
        self.run_number = 0
        self.write('sample/a.jpg', b'image A')
        self.write('sample/b.jpg', b'image B')
        self.write('sample/details.nfo', b'<title>old</title>')
        self.write('sample/clip.mp4', b'fake video')

    def write(self, relative, content):
        path = self.source / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)

    def remote_path(self, text):
        if text.startswith(self.m.REMOTE_PATH):
            return self.archive / text[len(self.m.REMOTE_PATH):]
        if text.startswith(self.m.STANDARD_REMOTE_PATH):
            return self.standard / text[len(self.m.STANDARD_REMOTE_PATH):]
        return Path(text)

    def run_cmd(self, cmd, **kwargs):
        self.assertEqual(cmd[0], 'rclone')
        self.assertNotIn('backend', cmd, 'Backup must never initiate Glacier restores')
        self.operations.append(tuple(cmd))
        if self.failure and self.failure(cmd):
            raise subprocess.CalledProcessError(1, cmd)
        if cmd[1] == 'lsjson':
            prefix = self.archive / 'tarballs'
            items = [{'Path': str(path.relative_to(prefix)), 'IsDir': False, 'ModTime': '2020-01-01T00:00:00Z'}
                     for path in prefix.rglob('*.tar')]
            return subprocess.CompletedProcess(cmd, 0, json.dumps(items), '')
        if cmd[1] == 'lsf':
            prefix = self.archive / 'tarballs/delta'
            output = ''.join(str(path.relative_to(prefix)) + '\n' for path in prefix.rglob('*.tar'))
            return subprocess.CompletedProcess(cmd, 0, output, '')
        if cmd[1] == 'copyto':
            source, destination = self.remote_path(cmd[2]), self.remote_path(cmd[3])
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
            if "--metadata-set" in cmd:
                name,value=cmd[cmd.index("--metadata-set")+1].split("=",1)
                self.remote_metadata[str(destination.relative_to(self.archive))]={name:value}
        elif cmd[1] == 'copy' and '--files-from-raw' not in cmd:
            source, destination = self.remote_path(cmd[2]), self.remote_path(cmd[3])
            destination.mkdir(parents=True, exist_ok=True)
            for path in source.iterdir():
                shutil.copyfile(path, destination / path.name)
        elif cmd[1] == 'copy':
            names = Path(cmd[cmd.index('--files-from-raw') + 1]).read_text().splitlines()
            for name in names:
                source, destination = self.source / name, self.archive / name
                if source.exists():
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    if not destination.exists() or source.stat().st_size != destination.stat().st_size:
                        shutil.copyfile(source, destination)
        else:
            raise AssertionError(f'Unexpected command: {cmd}')
        return subprocess.CompletedProcess(cmd, 0, '', '')

    def ensure_tag(self, key, tag_key, tag_value, dry_run):
        self.assertTrue((self.standard / 'current_manifest.json').exists())
        self.assertNotIn(key, restore.required_keys(self.catalog()))
        self.tagged.add(key)
        self.operations.append(('tag', key))
        return True

    def clear_obsolete(self, key):
        self.tagged.discard(key)
        self.operations.append(('clear_obsolete', key))
        return True

    def delete(self, key, dry_run):
        self.assertFalse(dry_run)
        self.assertNotIn(key, restore.required_keys(self.catalog()))
        self.operations.append(('delete', key))
        (self.archive / key).unlink(missing_ok=True)
        return 'deleted'

    def run_backup(self, **overrides):
        self.run_number += 1
        self.m.initialize_runtime()
        self.m.RUN_ID += f'_{self.run_number}'
        args = argparse.Namespace(dry_run=False, init_run=False, compact=False,
                                  delta_cutover_now=False, aws_region=None)
        for key, value in overrides.items():
            setattr(args, key, value)
        self.m.run_backup(args)

    def catalog(self):
        return restore.load_manifest(self.standard / 'current_manifest.json')

    def test_next_run_removes_crashed_workspace_but_preserves_pending_archives(self):
        self.m.initialize_runtime()
        abandoned = Path(self.m.TMP_DIR)
        (abandoned / 'catalog-snapshot').mkdir()
        (abandoned / 'catalog-snapshot/registry.sqlite3').write_bytes(b'stale copy')
        durable = self.ledger / 'pending-archives/retry.tar'
        durable.parent.mkdir()
        durable.write_bytes(b'required for retry')
        def check():
            self.assertFalse(abandoned.exists())
            self.assertTrue(Path(self.m.TMP_DIR).is_dir())
            self.assertEqual(durable.read_bytes(), b'required for retry')
        with patch.object(self.m, 'configure_logging'), patch.object(self.m.signal, 'signal'), \
             patch.object(self.m, 'run_backup', side_effect=lambda args: check()):
            self.assertEqual(self.m.main([]), 0)
        self.assertEqual(list(Path(self.m.TMP_BASE_DIR).iterdir()), [])
        self.assertEqual(durable.read_bytes(), b'required for retry')

    def test_busy_backup_does_not_create_or_remove_workspaces(self):
        self.m.initialize_runtime()
        active = Path(self.m.TMP_DIR)
        marker = active / 'active.sqlite3'
        marker.write_bytes(b'active snapshot')
        with self.m.exclusive_run_lock(self.m.RUN_LOCK_PATH), \
             patch.object(self.m, 'configure_logging'), patch.object(self.m, 'run_backup') as run:
            self.assertEqual(self.m.main([]), 2)
        run.assert_not_called()
        self.assertEqual(marker.read_bytes(), b'active snapshot')
        self.assertEqual(list(Path(self.m.TMP_BASE_DIR).iterdir()), [active])

    def test_stale_cleanup_ignores_symlinks_and_unrecognized_entries(self):
        self.m.initialize_runtime()
        base = Path(self.m.TMP_BASE_DIR)
        unrelated = base / 'keep-me'; unrelated.mkdir()
        outside = self.root / 'outside'; outside.mkdir()
        (outside / 'keep').write_bytes(b'untouched')
        link = base / 'run_20260928_030000_123_abcdefgh'
        link.symlink_to(outside, target_is_directory=True)
        regular = base / 'run_20260928_030000_123_ijklmnop'
        regular.write_bytes(b'not a directory')
        with self.m.exclusive_run_lock(self.m.RUN_LOCK_PATH):
            self.m.cleanup_stale_tmp()
        self.assertTrue(Path(self.m.TMP_DIR).is_dir())
        self.assertTrue(unrelated.is_dir())
        self.assertTrue(link.is_symlink())
        self.assertEqual((outside / 'keep').read_bytes(), b'untouched')
        self.assertEqual(regular.read_bytes(), b'not a directory')

    def test_dry_run_does_not_remove_abandoned_workspace(self):
        self.m.initialize_runtime()
        abandoned = Path(self.m.TMP_DIR)
        with patch.object(self.m, 'configure_logging'), patch.object(self.m.signal, 'signal'), \
             patch.object(self.m, 'run_backup'):
            self.assertEqual(self.m.main(['--dry-run']), 0)
        self.assertTrue(abandoned.is_dir())
        self.assertEqual(list(Path(self.m.TMP_BASE_DIR).iterdir()), [abandoned])

    def test_failed_backup_discards_catalog_copies_and_keeps_retry_marker(self):
        workspaces = []
        def fail(args):
            workspace = Path(self.m.TMP_DIR); workspaces.append(workspace)
            (workspace / 'scrape-catalog-backup').mkdir()
            (workspace / 'scrape-catalog-backup/registry.sqlite3').write_bytes(b'copy')
            raise RuntimeError('metadata upload failed')
        with patch.object(self.m, 'configure_logging'), patch.object(self.m.signal, 'signal'), \
             patch.object(self.m, 'run_backup', side_effect=fail):
            with self.assertRaisesRegex(RuntimeError, 'metadata upload failed'):
                self.m.main([])
        self.assertTrue((self.ledger / '.backup-interrupted.json').is_file())
        self.assertEqual(len(workspaces), 1)
        self.assertFalse(workspaces[0].exists())

    def assert_round_trip(self):
        destination = self.root / f'restored-{self.run_number}'
        restore.restore_local(self.catalog(), self.archive, destination)
        self.assertGreater(restore.verify_tree(destination, self.source), 0)
        return destination

    def test_native_snapshot_is_referenced_and_upload_failure_stops_cleanup(self):
        self.run_backup()
        catalog = self.catalog()
        ref = catalog['native_archive']
        self.assertEqual(catalog['version'], 3)
        self.assertTrue((self.standard / ref['manifest']['key']).is_file())
        old_manifest = (self.standard / 'current_manifest.json').read_bytes()
        (self.source / 'sample/clip.mp4').unlink()
        def failure(key):
            if key.startswith(PREFIX):
                raise RuntimeError('native upload failed')
        self.native_s3.before_put = failure
        with self.assertRaisesRegex(RuntimeError, 'native upload failed'):
            self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), old_manifest)
        self.assertNotIn('sample/clip.mp4', self.tagged)

    def test_full_backup_delta_delete_and_readd_round_trip(self):
        self.run_backup()
        self.assert_round_trip()
        (self.source / 'sample/b.jpg').unlink()
        (self.source / 'sample/details.nfo').unlink()
        self.run_backup()
        self.assertEqual(len(self.catalog()['units'][0]['deltas']), 1)
        self.assert_round_trip()
        self.write('sample/b.jpg', b're-added image')
        self.write('sample/details.nfo', b'<title>new</title>')
        self.run_backup()
        self.assertEqual(len(self.catalog()['units'][0]['deltas']), 2)
        self.assert_round_trip()

    def downgrade_file_index_to_legacy_seconds(self):
        with self.m.open_delta_db(self.m.TAR_DELTA_DB) as db:
            db.execute('UPDATE file_index SET ctime=ctime/1000000000')

    def test_nfo_retirement_does_not_reupload_unchanged_legacy_media(self):
        self.run_backup()
        self.downgrade_file_index_to_legacy_seconds()
        (self.source / 'sample/details.nfo').unlink()
        self.run_backup()
        key=self.catalog()['units'][0]['deltas'][-1]
        with tarfile.open(self.archive/key) as archive:
            self.assertEqual(set(archive.getnames()), {'__delta_meta__.json','__tombstones__.txt'})
        self.assert_round_trip()

    def test_noop_legacy_index_upgrade_writes_ns_without_new_archive(self):
        self.run_backup()
        before=restore.required_keys(self.catalog())
        self.downgrade_file_index_to_legacy_seconds()
        self.run_backup()
        self.assertEqual(before,restore.required_keys(self.catalog()))
        with self.m.open_delta_db(self.m.TAR_DELTA_DB) as db:
            stored=db.execute("SELECT ctime FROM file_index WHERE relpath='a.jpg'").fetchone()[0]
        actual=(self.source/'sample/a.jpg').stat().st_ctime_ns
        self.assertEqual(stored,actual)
        self.assertFalse(self.m.tar_ctime_matches(actual+1,stored))

    def test_legacy_timestamp_still_detects_changed_media(self):
        self.assertTrue(self.m.tar_ctime_matches(1790000000123456789,1790000000))
        self.assertFalse(self.m.tar_ctime_matches(1790000001123456789,1790000000))
        self.assertFalse(self.m.tar_ctime_matches(1790000000123456789,0))

    def test_old_video_is_uploaded_and_init_reconciles_all(self):
        # No time filter may hide an untracked path, even after a long outage.
        with patch.object(self.m.os.path, 'getctime', return_value=1):
            self.run_backup()
        self.assertTrue((self.archive / 'sample/clip.mp4').exists())
        copies = sum(op[1:2] == ('copyto',) and op[3].endswith('/sample/clip.mp4') for op in self.operations)
        self.run_backup()
        self.assertEqual(sum(op[1:2] == ('copyto',) and op[3].endswith('/sample/clip.mp4') for op in self.operations), copies)
        self.run_backup(init_run=True)
        self.assertEqual(sum(op[1:2] == ('copyto',) and op[3].endswith('/sample/clip.mp4') for op in self.operations), copies)  # HEAD reconciliation avoids reuploading unchanged bytes

    def test_video_upload_failure_remains_pending(self):
        self.failure = lambda cmd: cmd[1] == 'copyto' and cmd[3].endswith('/sample/clip.mp4')
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_backup()
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        self.assertEqual(conn.execute('SELECT COUNT(*) FROM video_uploads').fetchone()[0], 0)
        conn.close()
        self.assertFalse((self.standard / 'current_manifest.json').exists())
        self.failure = None
        self.run_backup()
        self.assert_round_trip()

    def test_video_created_after_scan_is_not_published_as_uploaded(self):
        original = self.m.upload_new_videos_if_any
        def upload(*args, **kwargs):
            self.write('sample/late.mp4', b'late')
            return original(*args, **kwargs)
        with patch.object(self.m, 'upload_new_videos_if_any', side_effect=upload):
            self.run_backup()
        self.assertNotIn('sample/late.mp4', [v['key'] for v in self.catalog()['videos']])
        self.run_backup()
        self.assert_round_trip()

    def test_changed_video_during_upload_aborts_without_recording_success(self):
        original = self.m.run_cmd
        def mutate(cmd, **kwargs):
            result = original(cmd, **kwargs)
            if cmd[1] == 'copyto' and cmd[3].endswith('/sample/clip.mp4'):
                self.write('sample/clip.mp4', b'changed during upload')
            return result
        with patch.object(self.m, 'run_cmd', side_effect=mutate):
            with self.assertRaisesRegex(RuntimeError, 'Video changed'):
                self.run_backup()
        self.assertFalse((self.standard / 'current_manifest.json').exists())

    def test_empty_source_keeps_last_manifest_and_objects(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        before = len(self.operations)
        shutil.rmtree(self.source / 'sample')
        with self.assertRaisesRegex(RuntimeError, 'empty'):
            self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)
        self.assertEqual(len(self.operations), before)

    def test_unmounted_source_fails_before_remote_work(self):
        self.m.SOURCE_MOUNT = str(self.root)
        with patch.object(self.m.os.path, 'ismount', return_value=False):
            with self.assertRaisesRegex(RuntimeError, 'not mounted'):
                self.run_backup()
        self.assertFalse(self.operations)

    def test_missing_collection_on_available_source_is_automatically_obsolete(self):
        self.write('removed/only.jpg', b'old image')
        self.write('removed/clip.mp4', b'old video')
        self.run_backup()
        removed_archives = {
            key for unit in self.catalog()['units'] if unit['rel_dir'] == 'removed'
            for key in [unit['base_key'], *unit['deltas']]
        }
        self.assertTrue(removed_archives)
        shutil.rmtree(self.source / 'removed')
        self.operations.clear()
        self.run_backup()
        self.assertTrue(removed_archives.issubset(self.tagged))
        self.assertIn('removed/clip.mp4', self.tagged)
        self.assertTrue(set(restore.required_keys(self.catalog())).isdisjoint(self.tagged))
        self.assertFalse([op for op in self.operations if op[0] == 'delete'])
        self.assert_round_trip()

    def test_source_outage_before_manifest_commit_preserves_previous_catalog(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        (self.source / 'sample/clip.mp4').unlink()
        self.operations.clear()
        original = self.m.run_cmd

        def source_outage(cmd, **kwargs):
            result = original(cmd, **kwargs)
            if cmd[:2] == ['rclone', 'copyto'] and cmd[3] == self.m.STANDARD_REMOTE_PATH + 'current_manifest.txt':
                self.source.rename(self.root / 'unavailable-source')
            return result

        with patch.object(self.m, 'run_cmd', side_effect=source_outage):
            with self.assertRaisesRegex(RuntimeError, 'source directory is unavailable'):
                self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)
        self.assertFalse([op for op in self.operations if op[0] in ('tag', 'delete')])

    def test_source_outage_after_publication_stops_cleanup_and_keeps_retry(self):
        self.run_backup()
        (self.source / 'sample/clip.mp4').unlink()
        self.operations.clear()
        original = self.m.build_and_upload_master_manifest

        def source_outage(*args, **kwargs):
            original(*args, **kwargs)
            self.source.rename(self.root / 'unavailable-source')

        with patch.object(self.m, 'build_and_upload_master_manifest', side_effect=source_outage):
            with self.assertRaisesRegex(RuntimeError, 'source directory is unavailable'):
                self.run_backup()
        self.assertFalse([op for op in self.operations if op[0] in ('tag', 'delete')])
        self.assertTrue((self.archive / 'sample/clip.mp4').exists())
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        try:
            self.assertIsNotNone(conn.execute('SELECT 1 FROM pending_gc WHERE key=?', ('sample/clip.mp4',)).fetchone())
        finally:
            conn.close()
        (self.root / 'unavailable-source').rename(self.source)
        self.run_backup()
        self.assertIn('sample/clip.mp4', self.tagged)
        self.assert_round_trip()

    def test_video_delete_breaker_blocks_publication(self):
        self.write('sample/second.mp4', b'second video')
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        self.m.MAX_VIDEO_DELETE_MARKERS_PER_RUN = 1
        for path in (self.source / 'sample').glob('*.mp4'):
            path.unlink()
        self.operations.clear()
        with self.assertRaisesRegex(RuntimeError, 'safety limit'):
            self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)
        self.assertFalse(self.operations)

    def test_database_error_blocks_publication(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        build = self.m.build_restore_catalog
        def broken_catalog(units):
            with patch.object(self.m, 'open_delta_db', side_effect=sqlite3.OperationalError('database unavailable')):
                return build(units)
        with patch.object(self.m, 'build_restore_catalog', side_effect=broken_catalog):
            with self.assertRaises(sqlite3.OperationalError):
                self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)

    def test_failed_remote_listing_blocks_publication(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        self.failure = lambda cmd: cmd[1] == 'lsjson'
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)

    def test_retired_legacy_unit_reappears_with_new_baseline(self):
        self.run_backup()
        original_base = self.catalog()['units'][0]['base_key']
        legacy = 'tarballs/sample.tar'
        shutil.copyfile(self.archive / original_base, self.archive / legacy)
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        with conn:
            conn.execute('UPDATE unit_state SET base_key=? WHERE unit_id=?', (legacy, 'R:sample'))
        conn.close()
        self.write('sample/child/nested.jpg', b'nested')
        self.run_backup()
        self.assertIn(legacy, self.tagged)
        self.assert_round_trip()
        shutil.rmtree(self.source / 'sample/child')
        self.run_backup()
        base = self.catalog()['units'][0]['base_key']
        self.assertNotEqual(base, legacy)
        self.assertNotIn(base, self.tagged)
        self.assert_round_trip()

    def test_missing_baseline_rebuilt_without_any_download(self):
        self.run_backup()
        old_base = self.catalog()['units'][0]['base_key']
        (self.archive / old_base).unlink()
        self.run_backup()
        self.assertNotEqual(self.catalog()['units'][0]['base_key'], old_base)
        self.assert_round_trip()

    def test_publication_failure_never_retires_remote_objects(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        self.write('sample/child/new.jpg', b'new child')
        self.operations.clear()
        def failure(key):
            if key == 'current_manifest.json':
                raise RuntimeError('master upload failed')
        self.native_s3.before_put = failure
        with self.assertRaises(ValueError):
            self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)
        self.assertFalse([op for op in self.operations if op[0] in ('tag', 'delete')])
        self.native_s3.before_put = lambda key: None
        self.run_backup()
        self.assert_round_trip()

    def test_compaction_keeps_restore_valid_and_defers_cleanup(self):
        self.run_backup()
        self.write('sample/details.nfo', b'<title>changed</title>')
        self.run_backup()
        old = restore.required_keys(self.catalog())
        self.m.COMPACT_DELTA_THRESHOLD = 1
        self.run_backup(compact=True)
        self.assertEqual(self.catalog()['units'][0]['deltas'], [])
        self.assertTrue(self.tagged.intersection(old))
        self.assert_round_trip()

    def test_dry_run_does_not_change_persistent_state(self):
        self.run_backup()
        before = {p.name: p.read_bytes() for p in self.ledger.iterdir() if p.is_file()}
        self.write('sample/details.nfo', b'changed metadata')
        self.operations.clear()
        self.run_backup(dry_run=True)
        after = {p.name: p.read_bytes() for p in self.ledger.iterdir() if p.is_file()}
        self.assertEqual(before, after)
        self.assertTrue(all(op[1] == 'lsjson' for op in self.operations))

    def test_deferred_cleanup_publishes_but_does_not_tag_or_delete(self):
        self.run_backup()
        self.write('sample/new.jpg', b'new image')
        self.run_backup()
        old_archives = set(restore.required_keys(self.catalog()))
        (self.source / 'sample/clip.mp4').unlink()
        self.m.COMPACT_DELTA_THRESHOLD = 1
        self.operations.clear()
        with patch.object(self.m, 'tombstone_remote_accidental_nonvideo_objects') as loose_cleanup:
            self.run_backup(compact=True, defer_cleanup=True)
            loose_cleanup.assert_not_called()
        self.assertFalse([op for op in self.operations if op[0] in ('tag', 'delete')])
        self.assertTrue(all((self.archive / key).exists() for key in old_archives))
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        try:
            actions = {row[0] for row in conn.execute('SELECT action FROM pending_gc')}
            self.assertEqual(actions, {'tag'})
        finally:
            conn.close()
        self.assert_round_trip()

    def test_literal_filenames_and_unrepresentable_filename(self):
        self.write('#directory/#video.mp4', b'sharp')
        self.write(' leading.mp4', b'leading')
        self.run_backup()
        self.assert_round_trip()
        self.write('sample/line\nbreak.mp4', b'newline')
        with self.assertRaisesRegex(ValueError, 'cannot be represented'):
            self.run_backup()

    def test_legacy_restore_metadata_adapter(self):
        self.run_backup()
        (self.source / 'sample/b.jpg').unlink()
        self.run_backup()
        converted = restore.load_legacy_manifest(
            self.standard / 'current_manifest.txt', self.m.TAR_DELTA_DB, self.m.TARBALL_FP_JSONL,
        )
        destination = self.root / 'legacy-restore'
        restore.restore_local(converted, self.archive, destination)
        restore.verify_tree(destination, self.source)

    def test_restore_cli_defaults_to_plan_only(self):
        self.run_backup()
        with patch('sys.stdout', new_callable=io.StringIO):
            restore.main(['sample', '--manifest', str(self.standard / 'current_manifest.json')])

    def test_offline_restore_test_cli_checks_every_byte(self):
        self.run_backup()
        destination = self.root / 'cli-restored'
        with patch('sys.stdout', new_callable=io.StringIO) as output:
            restore_test.main([
                'sample', '--manifest', str(self.standard / 'current_manifest.json'),
                '--objects-dir', str(self.archive), '--destination', str(destination),
                '--expected-dir', str(self.source), '--include-videos',
            ])
        self.assertIn('Verified 4 restored files', output.getvalue())

    def test_restore_refuses_missing_archive_and_unsafe_paths(self):
        self.run_backup()
        plan = self.catalog()
        base = self.archive / plan['units'][0]['base_key']
        base.unlink()
        with self.assertRaises(FileNotFoundError):
            restore.restore_local(plan, self.archive, self.root / 'missing')
        self.assertFalse((self.root / 'missing').exists())
        with tarfile.open(base, 'w') as archive:
            entry = tarfile.TarInfo('../escaped.jpg')
            entry.size = 4
            archive.addfile(entry, io.BytesIO(b'evil'))
        with self.assertRaises(ValueError):
            restore.restore_local(plan, self.archive, self.root / 'unsafe')
        self.assertFalse((self.root / 'escaped.jpg').exists())

    def test_retirement_limit_stops_run_before_side_effects(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        for path in (self.source / 'sample').iterdir():
            if path.suffix != '.mp4':
                path.unlink()
        self.m.MAX_DELETED_DIR_TARBALL_TOMBSTONES_PER_RUN = 0
        self.operations.clear()
        with self.assertRaisesRegex(RuntimeError, 'retirement safety limit'):
            self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)
        self.assertFalse(self.operations)

    def test_video_tombstone_retries_after_manifest_baseline_advances(self):
        self.run_backup()
        (self.source / 'sample/clip.mp4').unlink()
        with patch.object(self, 'ensure_tag', return_value=False):
            with self.assertRaisesRegex(RuntimeError, 'queued for retry'):
                self.run_backup()
        self.assertEqual(self.catalog()['videos'], [])
        self.assertTrue((self.archive / 'sample/clip.mp4').exists())
        self.run_backup()
        self.assertTrue((self.archive / 'sample/clip.mp4').exists())
        self.assertIn('sample/clip.mp4', self.tagged)
        self.assertFalse([op for op in self.operations if op[0] == 'delete'])
        self.assert_round_trip()

    def test_reintroduced_video_cancels_pending_deletion(self):
        self.run_backup()
        (self.source / 'sample/clip.mp4').unlink()
        with patch.object(self, 'ensure_tag', return_value=False):
            with self.assertRaises(RuntimeError):
                self.run_backup()
        self.write('sample/clip.mp4', b'newly reintroduced video')
        self.run_backup()
        self.assert_round_trip()
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        self.assertEqual(conn.execute('SELECT COUNT(*) FROM pending_gc').fetchone()[0], 0)
        conn.close()

    def test_returning_video_clears_obsolete_before_reupload(self):
        self.run_backup()
        original = (self.source / 'sample/clip.mp4').read_bytes()
        (self.source / 'sample/clip.mp4').unlink()
        self.run_backup()
        self.assertIn('sample/clip.mp4', self.tagged)
        self.write('sample/clip.mp4', original)
        self.operations.clear()
        self.run_backup()
        self.assertNotIn('sample/clip.mp4', self.tagged)
        untag = self.operations.index(('clear_obsolete', 'sample/clip.mp4'))
        copy = next(i for i, op in enumerate(self.operations)
                    if op[:2] == ('rclone', 'copyto') and op[3] == self.m.REMOTE_PATH + 'sample/clip.mp4')
        self.assertLess(untag, copy)
        self.assert_round_trip()

    def test_tag_removal_failure_blocks_upload_and_publication(self):
        self.run_backup()
        previous = (self.standard / 'current_manifest.json').read_bytes()
        self.write('sample/new.mp4', b'new video')
        self.operations.clear()
        with patch.object(self, 'clear_obsolete', return_value=False):
            with self.assertRaisesRegex(RuntimeError, 'obsolete tag failed'):
                self.run_backup()
        self.assertEqual((self.standard / 'current_manifest.json').read_bytes(), previous)
        self.assertFalse([op for op in self.operations if op[:2] == ('rclone', 'copyto')
                          and op[3] == self.m.REMOTE_PATH + 'sample/new.mp4'])

    def test_dry_run_never_creates_native_or_filesystem_checkpoint(self):
        with patch.object(self.m, 'open_native_session', side_effect=AssertionError('must not capture')):
            self.run_backup(dry_run=True)
        self.assertEqual(self.native_s3.operations, [])

    def test_video_returned_after_snapshot_is_protected_by_live_path(self):
        self.run_backup()
        (self.source / 'sample/clip.mp4').unlink()
        snapshot = self.root / 'retained-media'
        shutil.copytree(self.source, snapshot)
        def capture(args):
            session = PolicyNativeSession(self)
            session.media_path = snapshot
            return session
        self.m.open_native_session = capture
        original = self.m.build_and_upload_master_manifest
        def publish(*args, **kwargs):
            original(*args, **kwargs)
            self.write('sample/clip.mp4', b'returned after snapshot')
        with patch.object(self.m, 'build_and_upload_master_manifest', side_effect=publish):
            self.run_backup()
        self.assertNotIn('sample/clip.mp4', self.tagged)
        self.assertNotIn('sample/clip.mp4', restore.required_keys(self.catalog()))
        self.assertEqual(self.m.BASE_DIR, str(self.source))

    def test_media_subset_cannot_claim_full_native_binding(self):
        self.run_backup()
        catalog = self.catalog()
        plan = restore.select_plan(catalog, 'sample')
        self.assertEqual(plan['version'], 2)
        self.assertNotIn('native_archive', plan)
        catalog['videos'][0]['size'] += 1
        with self.assertRaisesRegex(ValueError, 'Media selection differs'):
            restore.select_plan(catalog, 'sample')

    def test_returning_video_disappearing_after_failed_copy_is_retombstoned(self):
        self.run_backup()
        video = self.source / 'sample/clip.mp4'
        original = video.read_bytes()
        video.unlink()
        self.run_backup()
        self.write('sample/clip.mp4', original)
        self.failure = lambda cmd: cmd[1] == 'copyto' and cmd[3].endswith('/sample/clip.mp4')
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_backup()
        self.assertNotIn('sample/clip.mp4', self.tagged)
        video.unlink()
        self.failure = None
        self.run_backup()
        self.assertIn('sample/clip.mp4', self.tagged)
        self.assertTrue((self.archive / 'sample/clip.mp4').exists())
        self.assert_round_trip()

    def test_cleanup_intent_forces_reconciliation_of_unchanged_video(self):
        self.run_backup()
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        with conn:
            self.m.queue_gc(conn, ['sample/clip.mp4'], 'delete')
        conn.close()
        # Simulate a tag write followed by a crash before recording its result.
        self.tagged.add('sample/clip.mp4')
        self.run_backup()
        self.assertNotIn('sample/clip.mp4', self.tagged)
        self.assert_round_trip()

    def test_old_delete_queue_is_migrated_to_tagging(self):
        self.run_backup()
        (self.source / 'sample/clip.mp4').unlink()
        conn = self.m.open_delta_db(self.m.TAR_DELTA_DB)
        with conn:
            self.m.queue_gc(conn, ['sample/clip.mp4'], 'delete')
        conn.close()
        self.run_backup()
        self.assertIn('sample/clip.mp4', self.tagged)
        self.assertTrue((self.archive / 'sample/clip.mp4').exists())
        self.assertFalse([op for op in self.operations if op[0] == 'delete'])

    def test_missing_source_file_is_not_treated_as_successful_rclone_copy(self):
        original = self.m.run_cmd
        def disappear(cmd, **kwargs):
            if cmd[1] == 'copyto' and cmd[3].endswith('/sample/clip.mp4'):
                (self.source / 'sample/clip.mp4').unlink()
            return original(cmd, **kwargs)
        with patch.object(self.m, 'run_cmd', side_effect=disappear):
            with self.assertRaises(FileNotFoundError):
                self.run_backup()
        self.assertFalse((self.standard / 'current_manifest.json').exists())

    def test_hardlinked_images_round_trip_as_regular_files(self):
        import os
        os.link(self.source / 'sample/a.jpg', self.source / 'sample/linked.jpg')
        self.run_backup()
        result = self.assert_round_trip()
        self.assertFalse((result / 'sample/linked.jpg').is_symlink())

    def test_delta_with_wrong_baseline_is_rejected(self):
        self.run_backup()
        self.write('sample/details.nfo', b'<title>changed</title>')
        self.run_backup()
        plan = self.catalog()
        key = plan['units'][0]['deltas'][0]
        path = self.archive / key
        with tarfile.open(path) as archive:
            meta = json.load(archive.extractfile('__delta_meta__.json'))
        meta['base_key'] = 'tarballs/wrong.tar'
        payload = json.dumps(meta).encode()
        with tarfile.open(path, 'w') as archive:
            member = tarfile.TarInfo('__delta_meta__.json')
            member.size = len(payload)
            archive.addfile(member, io.BytesIO(payload))
        with self.assertRaisesRegex(ValueError, 'mismatched base_key'):
            restore.restore_local(plan, self.archive, self.root / 'wrong-chain')

    def test_matching_legacy_fingerprint_seeds_index_without_reupload(self):
        self.m.initialize_runtime()
        unit = self.m.collect_partitioned_tar_units(self.m.BASE_DIR)[0]
        meta = self.m.collect_current_tar_meta_for_unit(unit)
        legacy = self.archive / 'tarballs/sample.tar'
        legacy.parent.mkdir()
        self.m.create_full_tarball(str(legacy), unit, meta)
        fp = self.m.compute_v2_footprint([v[3] for v in meta.values()], unit['abs_dir'])
        self.m.save_tarball_fp_store({unit['unit_id']: fp})
        self.run_backup()
        self.assertEqual(self.catalog()['units'][0]['base_key'], 'tarballs/sample.tar')
        archive_uploads = [op for op in self.operations if op[1:2] == ('copyto',) and op[3].startswith(self.m.REMOTE_PATH + 'tarballs/')]
        self.assertFalse(archive_uploads)
        self.assert_round_trip()

    def test_legacy_fingerprint_with_deleted_file_requires_fresh_snapshot(self):
        self.m.initialize_runtime()
        unit = self.m.collect_partitioned_tar_units(self.m.BASE_DIR)[0]
        meta = self.m.collect_current_tar_meta_for_unit(unit)
        legacy = self.archive / 'tarballs/sample.tar'
        legacy.parent.mkdir()
        self.m.create_full_tarball(str(legacy), unit, meta)
        self.m.save_tarball_fp_store({unit['unit_id']: self.m.compute_v2_footprint([v[3] for v in meta.values()], unit['abs_dir'])})
        (self.source / 'sample/b.jpg').unlink()
        self.run_backup()
        self.assertNotEqual(self.catalog()['units'][0]['base_key'], 'tarballs/sample.tar')
        self.assert_round_trip()


class LifecycleTagTests(unittest.TestCase):
    def setUp(self):
        self.m = import_backup()
        self.temp = tempfile.TemporaryDirectory(prefix='s3-tag-source-test-')
        self.addCleanup(self.temp.cleanup)
        self.m.BASE_DIR = self.temp.name
        self.m.SOURCE_MOUNT = None
        (Path(self.temp.name) / 'available.jpg').write_bytes(b'local image')
        self.tagger = self.m.S3ObjectTagger.__new__(self.m.S3ObjectTagger)
        self.tagger.bucket, self.tagger.prefix = 'test-bucket', ''
        from unittest.mock import Mock
        self.tagger.s3 = Mock()

    def test_reactivation_removes_only_obsolete_tag(self):
        self.tagger.s3.get_object_tagging.return_value = {'TagSet': [
            {'Key': 'obsolete', 'Value': 'true'}, {'Key': 'owner', 'Value': 'keep'}]}
        self.assertTrue(self.tagger.clear_obsolete('clip.mp4'))
        self.tagger.s3.put_object_tagging.assert_called_once_with(
            Bucket='test-bucket', Key='clip.mp4', Tagging={'TagSet': [{'Key': 'owner', 'Value': 'keep'}]})
        self.tagger.s3.delete_object.assert_not_called()

    def test_reactivation_can_clear_the_last_tag(self):
        self.tagger.s3.get_object_tagging.return_value = {'TagSet': [{'Key': 'obsolete', 'Value': 'true'}]}
        self.assertTrue(self.tagger.clear_obsolete('clip.mp4'))
        self.tagger.s3.put_object_tagging.assert_called_once_with(
            Bucket='test-bucket', Key='clip.mp4', Tagging={'TagSet': []})

    def test_missing_object_is_success_but_permission_error_is_not(self):
        for code, expected in [('NoSuchKey', True), ('AccessDenied', False)]:
            with self.subTest(code=code):
                self.tagger.s3.get_object_tagging.side_effect = self.m.ClientError(
                    {'Error': {'Code': code}}, 'GetObjectTagging')
                self.assertEqual(self.tagger.clear_obsolete('clip.mp4'), expected)
                self.assertEqual(self.tagger.ensure_tag('clip.mp4', 'obsolete', 'true', False), expected)
        self.tagger.s3.put_object_tagging.assert_not_called()

    def test_unmounted_source_blocks_video_and_archive_obsolete_tags(self):
        self.m.SOURCE_MOUNT = self.temp.name
        with patch.object(self.m.os.path, 'ismount', return_value=False):
            for key in ['missing.mp4', 'tarballs/old.tar']:
                with self.subTest(key=key):
                    with self.assertRaisesRegex(RuntimeError, 'not mounted'):
                        self.tagger.ensure_tag(key, 'obsolete', 'true', False)
        self.tagger.s3.get_object_tagging.assert_not_called()
        self.tagger.s3.put_object_tagging.assert_not_called()

    def test_source_outage_during_tag_read_blocks_obsolete_write(self):
        def source_outage(**kwargs):
            shutil.rmtree(self.temp.name)
            return {'TagSet': [{'Key': 'owner', 'Value': 'keep'}]}

        self.tagger.s3.get_object_tagging.side_effect = source_outage
        with self.assertRaisesRegex(RuntimeError, 'source directory is unavailable'):
            self.tagger.ensure_tag('missing.mp4', 'obsolete', 'true', False)
        self.tagger.s3.put_object_tagging.assert_not_called()

    def test_direct_s3_deletion_is_disabled(self):
        with self.assertRaisesRegex(RuntimeError, 'Direct S3 deletion is disabled'):
            self.m.ensure_s3_delete_marker('clip.mp4', False)


if __name__ == '__main__':
    unittest.main()

class PowerRecoveryTests(BackupTests):
    # Inherit the same isolated fake S3 transport and explicit cloud denial.
    def test_uploaded_archive_survives_lost_commit_without_duplicate_upload(self):
        original=self.m.run_cmd
        def crash(cmd,**kwargs):
            result=original(cmd,**kwargs)
            if cmd[1]=='copyto' and cmd[3].startswith(self.m.REMOTE_PATH+'tarballs/'):
                raise SystemExit('power lost after S3 accepted object')
            return result
        with patch.object(self.m,'run_cmd',side_effect=crash):
            with self.assertRaises(SystemExit):self.run_backup()
        before=[op for op in self.operations if op[1:2]==('copyto',) and op[3].startswith(self.m.REMOTE_PATH+'tarballs/')]
        self.assertEqual(len(before),1)
        self.run_backup();self.assert_round_trip()
        after=[op for op in self.operations if op[1:2]==('copyto',) and op[3].startswith(self.m.REMOTE_PATH+'tarballs/')]
        self.assertEqual(after,before)
    def test_uploaded_video_survives_lost_commit_without_duplicate_upload(self):
        original=self.m.run_cmd
        def crash(cmd,**kwargs):
            result=original(cmd,**kwargs)
            if cmd[1]=='copyto' and cmd[3].endswith('/sample/clip.mp4'):raise SystemExit('power boundary')
            return result
        with patch.object(self.m,'run_cmd',side_effect=crash):
            with self.assertRaises(SystemExit):self.run_backup()
        self.run_backup();self.assert_round_trip()
        self.assertEqual(sum(op[1:2]==('copyto',) and op[3].endswith('/sample/clip.mp4') for op in self.operations),1)
    def test_same_size_changed_video_is_uploaded(self):
        self.run_backup()
        before=(self.archive/'sample/clip.mp4').read_bytes()
        self.write('sample/clip.mp4',b'x'*len(before))
        self.run_backup()
        self.assertEqual((self.archive/'sample/clip.mp4').read_bytes(),b'x'*len(before))
        self.assert_round_trip()

    def test_legacy_multipart_video_can_be_verified_without_glacier_download(self):
        import hashlib,base64
        module=import_backup()
        local=self.source/'sample/clip.mp4'
        digest=base64.b64encode(hashlib.md5(local.read_bytes()).digest()).decode()
        head={'ContentLength':local.stat().st_size,'ETag':'"multipart-3"','Metadata':{'md5chksum':digest}}
        with patch.object(module,'remote_upload_head',return_value=head):
            self.assertTrue(module.remote_video_matches('fixture',str(local),local.stat().st_size,'new-signature'))
            local.write_bytes(b'x'*local.stat().st_size)
            self.assertFalse(module.remote_video_matches('fixture',str(local),local.stat().st_size,'new-signature'))

    def test_interrupted_marker_survives_failure_and_clears_after_success(self):
        marker=self.ledger/'.backup-interrupted.json'
        with patch.object(self.m,'configure_logging'),patch.object(self.m.signal,'signal'),patch.object(self.m,'run_backup',side_effect=RuntimeError('power boundary')):
            with self.assertRaises(RuntimeError):self.m.main([])
        self.assertTrue(marker.is_file())
        with patch.object(self.m,'configure_logging'),patch.object(self.m.signal,'signal'),patch.object(self.m,'run_backup'):
            self.assertEqual(self.m.main([]),0)
        self.assertFalse(marker.exists())
