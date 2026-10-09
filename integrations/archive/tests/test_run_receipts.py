"""Native Go digest vectors and actual durable producer request transitions."""

from contextlib import closing
import copy
import hashlib
import json
from pathlib import Path
import sqlite3
import uuid
from unittest.mock import patch

from stash_ingest.encoding import decode, encode
from stash_ingest.run_queue import RunQueue
from stash_archive.run_receipts import native_request_digest
from stash_archive.storage import InvalidArchive
from test_receipts import ReceiptFixture, ROOT, PRODUCER, MEDIA_ROOT


class RunReceiptTests(ReceiptFixture):
    def setUp(self):
        super().setUp()
        self.cases = json.loads((Path(__file__).parent / 'fixtures/source-run-digests.json').read_text())
        migration = (ROOT / 'pkg/sqlite/migrations/1000023_source_runs.up.sql').read_text()
        self.db.execute("CREATE TABLE media_root_revisions(root_uuid TEXT,revision INTEGER,PRIMARY KEY(root_uuid,revision))")
        self.db.execute("INSERT INTO media_root_revisions VALUES(?,1)", (MEDIA_ROOT,))
        for name in ('source_runs', 'source_run_requests'):
            body = migration.split(f'CREATE TABLE {name} (', 1)[1].split('\n);', 1)[0]
            self.db.executescript(f'CREATE TABLE {name} (' + body + '\n);')
        self.db.execute('CREATE TABLE source_run_retrievals(run_uuid TEXT PRIMARY KEY, url TEXT NOT NULL)')
        value = self.cases[0]['input']
        self.db.execute("INSERT INTO source_collection_revisions VALUES(?,?)",
                        (value['collection_uuid'], value['collection_revision']))
        self.db.commit()
        self.queue = RunQueue(self.box)

    def request(self, **changes):
        value = copy.deepcopy(self.cases[0]['input'])
        value.pop('request_uuid')
        value.update(changes)
        return value

    def submit(self, value=None, *, fixed_uuid=False):
        self.queue.enqueue(value or self.request())
        owner = str(uuid.uuid4())
        if fixed_uuid:
            with patch('stash_ingest.run_queue.uuid.uuid4', return_value=uuid.UUID(self.cases[0]['input']['request_uuid'])):
                return self.queue.claim(owner)
        return self.queue.claim(owner)

    def admit(self, delivery, *, acknowledge=True, run_uuid=None):
        request = decode(delivery.body)
        run = run_uuid or str(uuid.uuid4())
        native_hash = native_request_digest(request)
        self.db.execute("""INSERT OR IGNORE INTO source_runs(uuid,collection_uuid,collection_revision,
            root_uuid,root_revision,operation,policy_sha256,cooldown_seconds,work_key,target_key,pending,
            available_at_ms,created_at_ms,updated_at_ms) VALUES(?,?,?,?,1,?,?,?,?,?,?,1,1,1)""",
            (run,request['collection_uuid'],request['collection_revision'],MEDIA_ROOT,request['operation'],
             request['policy_sha256'],request['cooldown_seconds'],'a'*64,'b'*64,json.dumps([request['window']])))
        self.db.execute("INSERT INTO source_run_requests VALUES(?,?,?,?,1)",
                        (PRODUCER,request['request_uuid'],native_hash,run))
        if 'retrieval_url' in request:
            self.db.execute('INSERT OR IGNORE INTO source_run_retrievals VALUES(?,?)', (run, request['retrieval_url']))
        self.db.commit()
        receipt = dict(request,uuid=run,root_uuid=MEDIA_ROOT,root_revision=1,state='queued')
        if acknowledge:
            self.queue.admit(delivery, receipt)
        else:
            self.queue.fail(delivery,'network_unavailable')
        return receipt

    def admissions(self, **kwargs):
        return self.verify(**kwargs)['producers'][0]['source_admissions']

    def test_go_digest_corpus_includes_offsets_fractions_and_year_limits(self):
        for case in self.cases:
            with self.subTest(name=case['name']):
                self.assertEqual(native_request_digest(case['input']),case['native_sha256'])
        value = copy.deepcopy(self.cases[0]['input'])
        for changed in (dict(value,collection_revision=True),dict(value,window={'since':None,'until':'2026-10-01T00:00:00.0001Z'}),
                        dict(value,window={'since':None,'until':'0001-01-01T00:00:00Z'}),dict(value,extra=True)):
            with self.assertRaises(InvalidArchive): native_request_digest(changed)

    def test_reconstructs_admitted_request_and_matches_native_digest(self):
        delivery = self.submit(fixed_uuid=True)
        self.admit(delivery)
        self.assertNotEqual(delivery.sha256,self.cases[0]['native_sha256'])
        self.assertIsNone(self.box.db.execute("SELECT body FROM run_requests").fetchone()[0])
        self.assertEqual(self.admissions()['counts']['admitted'],1)
        self.db.execute("UPDATE source_run_requests SET digest=?", (delivery.sha256,))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive,'does not match the original request'):
            self.admissions()

    def test_missing_native_admission_and_conflicting_run_are_rejected(self):
        delivery = self.submit()
        old = self.root / 'older-library.sqlite'
        with closing(sqlite3.connect(old)) as target:
            self.db.backup(target)
        self.admit(delivery)
        with self.assertRaisesRegex(InvalidArchive,'missing from the native snapshot'):
            self.admissions(library=old)
        self.db.execute("UPDATE source_run_requests SET run_uuid=?", (str(uuid.uuid4()),))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive,'does not match the original request'):
            self.admissions()

    def test_profile_retrieval_is_preserved_and_verified_separately(self):
        target = 'https://www.reddit.com/user/example/submitted/?sort=top&t=year'
        receipt = self.admit(self.submit(self.request(retrieval_url=target)))
        self.assertEqual(self.admissions()['counts']['admitted'], 1)
        self.box.db.execute('UPDATE run_requests SET receipt=?', (encode(dict(receipt, retrieval_url=target + 'x')),))
        with self.assertRaisesRegex(InvalidArchive, 'another request or run'):
            self.admissions()
        self.box.db.execute('UPDATE run_requests SET receipt=?', (encode(receipt),))
        self.db.execute('UPDATE source_run_retrievals SET url=?', (target + 'x',))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, 'does not match the original request'):
            self.admissions()

    def test_old_snapshot_without_profile_retrievals_remains_readable(self):
        self.admit(self.submit())
        self.db.execute('DROP TABLE source_run_retrievals')
        self.db.commit()
        self.assertEqual(self.admissions()['counts']['admitted'], 1)

    def test_lost_response_preserves_unadmitted_original_and_later_run_state(self):
        delivery = self.submit()
        self.admit(delivery,acknowledge=False)
        self.db.execute("UPDATE source_runs SET state='succeeded',pending='[]'")
        self.db.commit()
        before = self.box.db.execute("SELECT * FROM run_requests").fetchall()
        result = self.admissions()
        self.assertEqual(result['counts']['pending'],1)
        self.assertEqual(result['counts']['accepted_unacknowledged'],1)
        self.assertEqual(result['counts']['admitted'],0)
        self.assertEqual(before,self.box.db.execute("SELECT * FROM run_requests").fetchall())
        self.assertEqual(self.box.db.execute("SELECT body FROM run_requests").fetchone()[0],delivery.body)

    def test_coalesced_run_keeps_both_requests_and_does_not_claim_completion(self):
        first = self.admit(self.submit())
        second = self.submit(self.request(window={'since':'2026-10-01T00:00:00Z','until':'2026-10-02T00:00:00Z'}))
        self.admit(second,run_uuid=first['uuid'])
        self.db.execute("UPDATE source_runs SET state='succeeded',pending='[]'")
        self.db.commit()
        report = self.admissions()
        self.assertEqual((report['requests'],report['counts']['admitted']),(2,2))
        self.assertNotIn('complete',report)

    def test_unfrozen_windows_and_pending_request_bytes_are_preserved(self):
        self.queue.enqueue(self.request())
        initial = self.admissions()
        self.assertEqual((initial['intents'],initial['pending_windows'],initial['requests']),(1,1,0))
        delivery = self.queue.claim(str(uuid.uuid4()))
        sending = self.admissions()
        self.assertEqual((sending['pending_windows'],sending['counts']['sending']),(0,1))
        self.queue.fail(delivery,'invalid_request',review=True)
        self.assertEqual(self.admissions()['counts']['review'],1)
        self.box.db.execute("UPDATE run_requests SET body=?", (b'{}',))
        with self.assertRaisesRegex(InvalidArchive,'exact original bytes'):
            self.admissions()

    def test_traversal_admissions_restore_without_becoming_published_windows(self):
        first = self.request(window={'since': None, 'until': '2026-10-01T00:00:00Z', 'basis': 'traversal'})
        self.queue.enqueue(first)
        self.assertEqual(self.admissions()['pending_windows'], 1)
        delivery = self.queue.claim(str(uuid.uuid4()))
        self.admit(delivery)
        self.assertEqual(self.admissions()['counts']['admitted'], 1)
        self.assertIsNone(self.box.db.execute('SELECT body FROM run_requests').fetchone()[0])
        self.queue.enqueue(self.request(window={'since': None, 'until': '2026-10-02T00:00:00Z', 'basis': 'traversal'}))
        self.queue.enqueue(self.request())
        self.assertEqual(self.admissions()['intents'], 2)
        self.assertEqual(self.admissions()['pending_windows'], 2)
        row = self.box.db.execute('SELECT intent_uuid,"window" FROM run_requests').fetchone()
        original = decode(row[1])
        self.box.db.execute('UPDATE run_intents SET windows=? WHERE uuid=?',
                            (encode([{**original, 'basis': ''}]), row[0]))
        with self.assertRaisesRegex(InvalidArchive, 'normalized and disjoint'):
            self.admissions()

    def test_traversal_basis_cannot_be_discarded_after_releasing_the_http_body(self):
        delivery = self.submit(self.request(window={'since': None, 'until': '2026-10-01T00:00:00Z', 'basis': 'traversal'}))
        self.admit(delivery)
        kept = decode(self.box.db.execute('SELECT "window" FROM run_requests').fetchone()[0])
        kept.pop('basis')
        self.box.db.execute('UPDATE run_requests SET "window"=?', (encode(kept),))
        with self.assertRaisesRegex(InvalidArchive, 'coverage basis'):
            self.admissions()

    def test_template_window_and_retained_receipt_cannot_drift(self):
        delivery = self.submit()
        receipt = self.admit(delivery)
        changed = dict(receipt,root_revision=2)
        self.box.db.execute("UPDATE run_requests SET receipt=?", (encode(changed),))
        with self.assertRaisesRegex(InvalidArchive,'another request or run'): self.admissions()
        self.box.db.execute("UPDATE run_requests SET receipt=?", (encode(receipt),))
        self.box.db.execute("UPDATE run_intents SET config_sha256=?", ('b'*64,))
        with self.assertRaisesRegex(InvalidArchive,'original identity'): self.admissions()

    def test_schema_downgrade_cannot_hide_source_admissions(self):
        self.admit(self.submit())
        self.box.db.execute('PRAGMA user_version=1')
        with self.assertRaisesRegex(InvalidArchive,'omit retained source admission state'):
            self.admissions()
