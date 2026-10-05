"""Process/receipt binding tests; Go's command test exercises the real validator."""

from contextlib import closing, redirect_stderr, redirect_stdout
import hashlib
import io
import json
import fcntl
import os
from pathlib import Path
import sqlite3
import sys
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from stash_archive.bundle import FORMAT, export_archive, import_archive, iter_artifacts
from stash_archive.cli import main
from stash_archive.storage import InvalidArchive, json_bytes
from stash_archive.verification import verify_archive_proofs, validator_output


def contract_validator(root, *, mode="ok", overrides=None):
    """A protocol fixture, not a substitute for the real native schema checks."""
    path = Path(root) / "validator $ with spaces"
    path.write_text(f"#!{sys.executable}\n" + f"mode = {mode!r}\noverrides = {overrides!r}\n" + r'''
import hashlib, json, os, sqlite3, sys, time
from pathlib import Path
assert len(sys.argv) == 3 and sys.argv[1] == '--verify-native-snapshot'
database = Path(sys.argv[2])
assert database.is_absolute() and database.name == 'library.sqlite'
if mode in ('sleep', 'closed-pipes'):
    if mode == 'closed-pipes':
        os.close(1)
        os.close(2)
    time.sleep(60)
if mode in ('large-stdout', 'large-stderr'):
    for _ in range(32):
        os.write(1 if mode == 'large-stdout' else 2, b'x' * 8192)
    sys.exit(0)
with database.open('rb') as source:
    sha = hashlib.file_digest(source, 'sha256').hexdigest()
connection = sqlite3.connect(database.as_uri() + '?mode=ro', uri=True)
schema, = connection.execute('SELECT version FROM schema_migrations').fetchone()
connection.close()
report = dict(format='org.notsafeforgit.stash.native-archive.snapshot-verification',
    version=1, lineage='org.notsafeforgit.stash.native-archive', schema_version=schema,
    sha256=sha, bytes=database.stat().st_size, database_verified=True,
    pending_file_deletions=3, filesystem_recovery_verified=False)
report.update(overrides or {})
body = json.dumps(report)
if mode == 'duplicate-key':
    body = '{"version": 1, ' + body[1:]
if mode == 'log-prefix':
    print('server startup log')
if mode == 'extra-json':
    print(body)
print(body)
if mode == 'failed':
    print('native schema rejected', file=sys.stderr)
    sys.exit(7)
''')
    path.chmod(0o700)
    return path


class NativeVerificationTests(unittest.TestCase):
    def test_validator_keeps_host_lock_after_parent_descriptor_closes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            lock = root / 'host.lock'
            released = root / 'finish'
            executable = root / 'validator'
            executable.write_text(f'#!{sys.executable}\n' +
                                  'from pathlib import Path\nimport time\n' +
                                  f'release = Path({str(released)!r})\n' +
                                  'deadline = time.monotonic() + 5\n' +
                                  'while not release.exists() and time.monotonic() < deadline:\n    time.sleep(0.01)\n' +
                                  'assert release.exists()\nprint("{}")\n')
            executable.chmod(0o700)
            fd = os.open(lock, os.O_CREAT | os.O_RDWR, 0o600)
            held = [fd]
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            real_popen = subprocess.Popen
            def child_owns_lock(*args, **kwargs):
                child = real_popen(*args, **kwargs)
                os.close(fd)
                held.clear()
                try:
                    with lock.open('rb') as competing:
                        with self.assertRaises(BlockingIOError):
                            fcntl.flock(competing, fcntl.LOCK_EX | fcntl.LOCK_NB)
                finally:
                    released.touch()
                return child
            try:
                with patch('stash_archive.verification.subprocess.Popen', side_effect=child_owns_lock):
                    self.assertEqual(validator_output(executable, root / 'library.sqlite', 10, lock_fd=fd), b'{}\n')
                with lock.open('rb') as competing:
                    fcntl.flock(competing, fcntl.LOCK_EX | fcntl.LOCK_NB)
            finally:
                for remaining in held:
                    os.close(remaining)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.database = self.root / "native.sqlite"
        with closing(sqlite3.connect(self.database)) as db:
            db.executescript(f"""
                CREATE TABLE native_schema(singleton INTEGER PRIMARY KEY,lineage TEXT);
                INSERT INTO native_schema VALUES(1,'{FORMAT}');
                CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,dirty INTEGER);
                INSERT INTO schema_migrations VALUES(1000077,0);
                CREATE TABLE blobs(checksum TEXT PRIMARY KEY,blob BLOB);
                CREATE TABLE ingest_producers(uuid TEXT PRIMARY KEY);
                CREATE TABLE file_deletions(id TEXT PRIMARY KEY);
                INSERT INTO file_deletions VALUES('one'),('two'),('three');
            """)
        self.archive = self.root / "archive"
        self.manifest = export_archive(self.database, self.archive, reserve=0)
        self.library, = iter_artifacts(self.archive, self.manifest)

    def verify(self, executable, **kwargs):
        return verify_archive_proofs(self.archive, native_validator=executable,
                                     temp_parent=self.root, reserve=0, **kwargs)

    def test_combined_proof_uses_one_restore_and_binds_exact_library(self):
        executable = contract_validator(self.root)
        before = hashlib.sha256(self.database.read_bytes()).hexdigest()
        with patch('stash_archive.verification.import_archive', wraps=import_archive) as restore:
            result = self.verify(executable, producer_origin="https://stash.example")
        self.assertEqual(restore.call_count, 1)
        proof = result['native_snapshot']
        self.assertEqual(proof['archive_uuid'], self.manifest['uuid'])
        self.assertEqual(proof['manifest_sha256'], hashlib.sha256(json_bytes(self.manifest)).hexdigest())
        self.assertEqual(proof['sha256'], self.library['sha256'])
        self.assertEqual(proof['bytes'], self.library['size'])
        self.assertEqual(proof['schema_version'], self.library['sqlite']['schema'])
        self.assertEqual(proof['component'], {'role': 'library', 'name': 'library'})
        self.assertEqual(proof['pending_file_deletions'], 3)
        self.assertFalse(proof['filesystem_recovery_verified'])
        self.assertTrue(result['contents_verified'])
        self.assertEqual(result['coverage'], 'declared-components')
        receipts = result['ingestion_receipts']
        self.assertEqual(receipts['archive_uuid'], proof['archive_uuid'])
        self.assertEqual(receipts['manifest_sha256'], proof['manifest_sha256'])
        self.assertEqual(receipts['components'][0]['sha256'], proof['sha256'])
        self.assertEqual(before, hashlib.sha256(self.database.read_bytes()).hexdigest())
        self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])

    def test_mismatched_partial_or_overclaiming_reports_are_rejected(self):
        for fields in ({'sha256': '0' * 64}, {'bytes': self.library['size'] + 1},
                       {'schema_version': 1000076}, {'lineage': 'foreign'},
                       {'format': FORMAT}, {'version': 2}, {'version': True},
                       {'database_verified': False}, {'database_verified': 1},
                       {'filesystem_recovery_verified': True},
                       {'filesystem_recovery_verified': 0},
                       {'pending_file_deletions': -1}, {'pending_file_deletions': False},
                       {'extra': 'unsupported'}, {'bytes': float(self.library['size'])}):
            with self.subTest(fields=fields):
                with self.assertRaises(InvalidArchive):
                    self.verify(contract_validator(self.root, overrides=fields))
                self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])

    def test_invalid_or_failed_program_output_cannot_claim_success(self):
        for mode in ('duplicate-key', 'log-prefix', 'extra-json', 'failed', 'large-stdout', 'large-stderr'):
            with self.subTest(mode=mode):
                with self.assertRaises(InvalidArchive):
                    self.verify(contract_validator(self.root, mode=mode))
                self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])

    def test_timeout_kills_stalled_validation_even_with_closed_pipes(self):
        for mode in ('sleep', 'closed-pipes'):
            with self.subTest(mode=mode):
                with self.assertRaisesRegex(InvalidArchive, 'timed out'):
                    self.verify(contract_validator(self.root, mode=mode), timeout=0.1)
                self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])

    def test_invalid_configuration_and_missing_executable_fail_closed(self):
        with patch('stash_archive.verification.import_archive', side_effect=AssertionError('unexpected restore')):
            for timeout in (0, -1, float('inf'), float('nan'), True):
                with self.subTest(timeout=timeout):
                    with self.assertRaises(InvalidArchive):
                        self.verify(contract_validator(self.root), timeout=timeout)
            with self.assertRaises(InvalidArchive):
                self.verify('')
            with self.assertRaises(FileNotFoundError):
                self.verify(self.root / 'missing')
            with self.assertRaises(InvalidArchive):
                self.verify(contract_validator(self.root), producer_origin='')

    def test_cli_only_emits_success_when_every_requested_check_passes(self):
        arguments = ['verify', str(self.archive), '--native-validator', str(contract_validator(self.root)),
                     '--temp-parent', str(self.root), '--reserve-bytes', '0']
        output = io.StringIO()
        with redirect_stdout(output):
            main(arguments)
        self.assertTrue(json.loads(output.getvalue())['native_snapshot']['database_verified'])
        contract_validator(self.root, mode='failed')
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors), self.assertRaises(SystemExit) as exited:
            main(arguments)
        self.assertEqual(exited.exception.code, 1)
        self.assertEqual(output.getvalue(), '')
        self.assertIn('exit 7', errors.getvalue())
        self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])

        # Transport and native reports can pass while the producer boundary
        # fails. The successful partial results must not escape on stdout.
        contract_validator(self.root)
        with closing(sqlite3.connect(self.database)) as db:
            db.execute("INSERT INTO ingest_producers VALUES('11111111-1111-4111-8111-111111111111')")
            db.commit()
        other = self.root / 'missing-producer'
        export_archive(self.database, other, reserve=0)
        arguments[1] = str(other)
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors), self.assertRaises(SystemExit) as exited:
            main(arguments + ['--producer-origin', 'https://stash.example'])
        self.assertEqual(exited.exception.code, 1)
        self.assertEqual(output.getvalue(), '')
        self.assertIn('no matching outbox', errors.getvalue())
        self.assertEqual(list(self.root.glob('stash-archive-verify-*')), [])


if __name__ == '__main__':
    unittest.main()
