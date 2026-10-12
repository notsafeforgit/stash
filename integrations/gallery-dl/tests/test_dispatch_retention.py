from contextlib import closing, redirect_stdout
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.cli import main
from stash_ingest.collection_dispatch import CollectionDispatcher
from stash_ingest.dispatch_retention import DispatchCapacity, retire_enrichment_policies
from stash_ingest.encoding import InvalidData
from stash_ingest.enrichment_dispatch import EnrichmentDispatcher
from stash_ingest.outbox import Outbox
from helpers import PRODUCER
import test_enrichment_dispatch as fixtures


class DispatchRetentionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / 'outbox.sqlite'
        self.now = 1000
        self.transport = SimpleNamespace(endpoint='http://fixture.invalid', producer=PRODUCER)
        self.collection = str(uuid.uuid4())
        self.profile = SimpleNamespace(policy_sha256='a' * 64)
        self.box = Outbox(self.path, self.transport.endpoint, PRODUCER, clock=lambda: self.now)
        self.addCleanup(self.box.close)

    def cursor(self, policy='b' * 64, collection=None, **state):
        worker = EnrichmentDispatcher(self.box, self.transport, collection or str(uuid.uuid4()),
                                      SimpleNamespace(policy_sha256=policy))
        worker.save(worker.state(), **state)
        return worker

    def stage(self, policy='a' * 64):
        return fixtures.EnrichmentDispatchTests.stage(self, self.box, policy=policy)

    def snapshot(self):
        return '\n'.join(self.box.db.iterdump())

    def test_retirement_is_previewed_idempotent_and_preserves_all_unfinished_evidence(self):
        value = self.stage(policy='b' * 64)
        protected = self.cursor(collection=self.collection, delivery_after=value.job_uuid)
        old = self.cursor(policy='c' * 64, local_after=str(uuid.uuid4()), job_after=123)
        current = self.cursor(policy=self.profile.policy_sha256, job_after=456)
        for policy in ('b' * 64, 'c' * 64, self.profile.policy_sha256):
            CollectionDispatcher(self.box, self.transport, SimpleNamespace(policy_sha256=policy))
        before = self.snapshot()
        preview = retire_enrichment_policies(self.box, ['b' * 64, 'c' * 64])
        self.assertEqual(preview['protected_policies'], 1)
        self.assertEqual(preview['cursors']['enrichment_dispatch']['eligible'], 1)
        self.assertEqual(self.snapshot(), before)
        journals = [tuple(r) for r in self.box.db.execute('SELECT * FROM enrichment_executions')]
        states = protected.state(), current.state()
        result = retire_enrichment_policies(self.box, ['b' * 64, 'c' * 64], apply=True)
        self.assertEqual(result['cursors']['enrichment_dispatch']['deleted'], 1)
        self.assertEqual(result['cursors']['enrichment_collection_dispatch']['deleted'], 1)
        self.assertEqual((protected.state(), current.state()), states)
        self.assertEqual([tuple(r) for r in self.box.db.execute('SELECT * FROM enrichment_executions')], journals)
        self.assertIsNone(old.state())
        self.assertEqual(retire_enrichment_policies(self.box, ['b' * 64, 'c' * 64], apply=True)
                         ['cursors']['enrichment_dispatch']['deleted'], 0)

    def test_review_jobs_are_also_protected(self):
        self.stage()
        self.box.db.execute("UPDATE enrichment_executions SET phase='review'")
        self.cursor(policy='a' * 64)
        result = retire_enrichment_policies(self.box, ['a' * 64], apply=True)
        self.assertEqual(result['protected_policies'], 1)
        self.assertEqual(result['cursors']['enrichment_dispatch']['deleted'], 0)

    def test_full_cache_reclaims_only_expired_empty_cursors_without_unfinished_work(self):
        self.stage()
        expired = self.cursor(available_at=900)
        keep = [
            self.cursor(available_at=1100),
            self.cursor(available_at=900, job_after=42),
            self.cursor(available_at=900, delivery_after=str(uuid.uuid4())),
            self.cursor(available_at=900, local_after=str(uuid.uuid4())),
            self.cursor(available_at=900, target_cursor={'priority': 20, 'not_before': '2026-10-01T00:00:00Z', 'uuid': str(uuid.uuid4())}),
            self.cursor(available_at=900, failures=1, error_code='network_unavailable'),
            self.cursor(available_at=900, collection=self.collection),
            self.cursor(policy='a' * 64, available_at=900),
        ]
        before = [w.state() for w in keep]
        with patch('stash_ingest.enrichment_dispatch.DISPATCH_LIMIT', 9):
            new = self.cursor(policy='a' * 64)
        self.assertIsNone(expired.state())
        self.assertIsNotNone(new.state())
        self.assertEqual([w.state() for w in keep], before)
        self.assertEqual(self.box.db.execute('SELECT count(*) FROM enrichment_executions').fetchone()[0], 1)

    def test_full_cache_preserves_partial_page_and_reports_resource_and_actual_limit(self):
        worker = self.cursor(job_after=123)
        before = self.snapshot()
        with patch('stash_ingest.enrichment_dispatch.DISPATCH_LIMIT', 1), self.assertRaises(DispatchCapacity) as raised:
            self.cursor(policy='a' * 64)
        self.assertEqual(raised.exception.result(), {'state': 'unavailable', 'error_code': 'dispatch_capacity_exhausted',
                                                   'resource': 'enrichment_dispatch', 'records': 1, 'limit': 1})
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(worker.state()['job_after'], 123)

    def test_evicted_and_recreated_cursor_rejects_stale_saves_and_selection(self):
        old = self.cursor(available_at=900)
        before = old.state()
        with closing(Outbox(self.path, self.transport.endpoint, PRODUCER, clock=lambda: self.now)) as peer:
            retire_enrichment_policies(peer, ['b' * 64], apply=True)
            self.assertEqual(old.once(), {'state': 'contended'})
            new = EnrichmentDispatcher(peer, self.transport, old.key[0], SimpleNamespace(policy_sha256='b' * 64))
            self.assertNotEqual(new.state()['revision'], before['revision'])
            self.assertFalse(old.save(before, job_after=987))
            self.assertEqual(new.state()['job_after'], 0)

    def test_collection_cursor_recreation_also_rejects_stale_save(self):
        old = CollectionDispatcher(self.box, self.transport, self.profile)
        before = old.state()
        retire_enrichment_policies(self.box, ['a' * 64], apply=True)
        self.assertEqual(old.once(), {'state': 'contended'})
        new = CollectionDispatcher(self.box, self.transport, self.profile)
        self.assertFalse(old.save(before, after_collection=str(uuid.uuid4())))
        self.assertEqual(new.state()['after_collection'], '')

    def test_cli_retirement_does_not_require_a_token_or_contact_stash(self):
        self.cursor()
        args = ['--outbox', str(self.path), '--endpoint', self.transport.endpoint, '--producer', PRODUCER,
                'retire-enrichment-progress', '--retired-policy', 'b' * 64]
        with patch('stash_ingest.cli.Client') as client, redirect_stdout(io.StringIO()) as output:
            self.assertEqual(main(args), 0)
        client.assert_not_called()
        self.assertEqual(json.loads(output.getvalue())['state'], 'preview')
        self.assertEqual(self.box.db.execute('SELECT count(*) FROM enrichment_dispatch').fetchone()[0], 1)

    def test_invalid_and_delivery_only_policies_are_rejected(self):
        for policies in ([], ['bad'], ['0' * 64]):
            with self.subTest(policies=policies), self.assertRaises(InvalidData):
                retire_enrichment_policies(self.box, policies, apply=True)


if __name__ == '__main__':
    unittest.main()
