"""Exercise acknowledged download/outbox/library ordering with live WAL writers."""

from contextlib import closing, redirect_stderr, redirect_stdout
import io
import json
import sqlite3
from unittest.mock import patch

from stash_archive.bundle import export_archive, import_archive, snapshot_database
from stash_archive.cli import main
from stash_archive.receipts import verify_restored_receipts
from stash_archive.storage import InvalidArchive, store_file
from test_receipts import ORIGIN, ReceiptFixture


class CheckpointTests(ReceiptFixture):
    def downloads(self, name):
        path = self.root / name
        with closing(sqlite3.connect(path)) as db:
            db.execute('CREATE TABLE archive(entry TEXT PRIMARY KEY)')
        return path

    def component(self, role, path):
        return {'role': role, 'name': path.name, 'path': path}

    def test_all_downloads_then_outbox_then_library_survive_interleaved_acknowledgements(self):
        downloads = [self.downloads('one.sqlite'), self.downloads('two.sqlite')]
        events = []

        def completed():
            event = self.event()
            self.accepted(event)  # Native commit, then durable queue acknowledgement.
            events.append(event['event_uuid'])
            for path in downloads:
                with closing(sqlite3.connect(path)) as db:
                    db.execute('INSERT INTO archive VALUES(?)', (event['event_uuid'],))
                    db.commit()  # Follows durable file completion, like the adapter.

        completed()
        phases = []

        def snapshot(source, target, role, reserve):
            meta = snapshot_database(source, target, role, reserve)
            phases.append(role)
            if len(phases) == 2 or role == 'producer_outbox':
                completed()
            return meta

        packed = []

        def pack(root, source, **kwargs):
            if not packed:
                # Later file deliveries may continue during long artwork
                # compression; none can alter these already captured inputs.
                completed()
            packed.append(source)
            return store_file(root, source, **kwargs)

        archive = self.root / 'bundle'
        # Deliberately list the outbox before its archives: input enumeration
        # order must not override the required causal snapshot order.
        components = [self.component('producer_outbox', self.outbox)] + [
            self.component('download_archive', path) for path in downloads]
        with patch('stash_archive.bundle.snapshot_database', side_effect=snapshot), \
                patch('stash_archive.bundle.store_file', side_effect=pack):
            manifest = export_archive(self.library, archive, components=components,
                                      producer_origin=ORIGIN, reserve=0)
        self.assertEqual(phases, ['download_archive', 'download_archive', 'producer_outbox', 'library'])
        self.assertEqual(len(events), 4)
        restored = self.root / 'restored'
        import_archive(archive, restored, reserve=0)
        for path in downloads:
            with closing(sqlite3.connect(restored / 'components/download_archive' / path.name)) as db:
                self.assertEqual(db.execute('SELECT entry FROM archive').fetchall(), [(events[0],)])
        with closing(sqlite3.connect(restored / 'components/producer_outbox' / self.outbox.name)) as db:
            self.assertEqual(db.execute('SELECT event_uuid FROM events ORDER BY seq').fetchall(), [(e,) for e in events[:2]])
        with closing(sqlite3.connect(restored / 'library.sqlite')) as db:
            self.assertEqual({r[0] for r in db.execute('SELECT event_uuid FROM ingest_receipts')}, set(events[:3]))
        proof = verify_restored_receipts(archive, restored, manifest, ORIGIN)
        self.assertEqual(proof['producers'][0]['counts']['acknowledged'], 2)
        self.assertEqual(self.box.db.execute('SELECT count(*) FROM events').fetchone()[0], 4)

    def test_inconsistent_receipt_boundary_fails_before_packing_or_sealing(self):
        self.accepted(self.event())
        self.db.execute('DELETE FROM ingest_receipts')
        self.db.commit()
        before = self.box.db.execute('SELECT * FROM events').fetchall()
        output = self.root / 'rejected'
        with patch('stash_archive.bundle.store_file', side_effect=AssertionError('packed an invalid boundary')):
            with self.assertRaisesRegex(InvalidArchive, 'missing from the native snapshot'):
                export_archive(self.library, output, components=[self.component('producer_outbox', self.outbox)],
                               producer_origin=ORIGIN, reserve=0)
        self.assertFalse(output.exists())
        self.assertEqual(before, self.box.db.execute('SELECT * FROM events').fetchall())

    def test_missing_registered_outbox_rejects_export(self):
        output = self.root / 'rejected'
        with self.assertRaisesRegex(InvalidArchive, 'no matching outbox'):
            export_archive(self.library, output, producer_origin=ORIGIN, reserve=0)
        self.assertFalse(output.exists())

    def test_corrupt_download_archive_stops_before_outbox_and_library(self):
        corrupt = self.root / 'corrupt.sqlite'
        corrupt.write_bytes(b'not a SQLite database')
        output = self.root / 'rejected'
        phases = []

        def snapshot(source, target, role, reserve):
            phases.append(role)
            return snapshot_database(source, target, role, reserve)

        with patch('stash_archive.bundle.snapshot_database', side_effect=snapshot):
            with self.assertRaises(sqlite3.DatabaseError):
                export_archive(self.library, output, components=[self.component('producer_outbox', self.outbox),
                    self.component('download_archive', corrupt)], producer_origin=ORIGIN, reserve=0)
        self.assertEqual(phases, ['download_archive'])
        self.assertFalse(output.exists())

    def test_cli_requires_matching_origin_and_queues_before_success(self):
        self.accepted(self.event())
        components = self.root / 'components.json'
        components.write_text(json.dumps([dict(self.component('producer_outbox', self.outbox), path=str(self.outbox))]))
        archive = self.root / 'bundle'
        arguments = ['export', '--database', str(self.library), '--output', str(archive),
                     '--components', str(components), '--reserve-bytes', '0', '--producer-origin']
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors), self.assertRaises(SystemExit) as exited:
            main(arguments + ['https://wrong.example'])
        self.assertEqual(exited.exception.code, 1)
        self.assertFalse(archive.exists())
        self.assertEqual(output.getvalue(), '')
        with redirect_stdout(output):
            main(arguments + [ORIGIN])
        self.assertEqual(json.loads(output.getvalue())['coverage'], 'declared-components')
        self.assertTrue((archive / 'manifest.json').is_file())

    def test_invalid_declarations_fail_before_any_snapshot(self):
        queue = self.component('producer_outbox', self.outbox)
        for components, origin in [([queue, queue], ORIGIN), ([dict(queue, role='library')], ORIGIN),
                                   ([dict(queue, name='../escape')], ORIGIN), ([queue], '')]:
            with self.subTest(components=components, origin=origin):
                output = self.root / 'invalid'
                with patch('stash_archive.bundle.snapshot_database', side_effect=AssertionError('unexpected copy')):
                    with self.assertRaises(InvalidArchive):
                        export_archive(self.library, output, components=components, producer_origin=origin, reserve=0)
                self.assertFalse(output.exists())
