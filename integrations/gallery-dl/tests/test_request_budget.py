import unittest
from types import SimpleNamespace
from unittest.mock import Mock

from stash_ingest import request_budget
from stash_ingest.client import Unavailable
from stash_ingest.encoding import decode
from stash_ingest.runs import SourcePaused
import test_producer


class BudgetTests(unittest.TestCase):
    setUp = test_producer.ProducerTests.setUp

    def test_wait_reuses_one_request_identity_and_does_not_hold_heartbeat_lock(self):
        calls = []
        def request(method, path, body):
            value = decode(body)
            calls.append(value)
            return {'request_uuid': value['request_uuid'], 'source_scope': 'service:twitter',
                    'ready': len(calls) > 1, 'wait_ms': 0 if len(calls) > 1 else 20}
        self.lease.client._request = request
        self.lease.stop = Mock()
        self.lease.stop.wait.return_value = False
        self.producer.scan_mode = 'routine'
        permit = self.producer.request_source('https://x.com/i/api/graphql/hash/UserMedia')
        self.assertEqual(len(calls), 2)
        self.assertEqual(calls[0]['request_uuid'], calls[1]['request_uuid'])
        self.assertEqual((calls[0]['class'], calls[0]['operation']), ('routine', 'listing'))
        self.assertTrue(permit['ready'])
        self.lease.stop.wait.assert_called_once()

    def test_server_outage_pauses_without_making_a_source_request(self):
        self.lease.client._request = Mock(side_effect=Unavailable('offline'))
        with self.assertRaises(SourcePaused):
            self.producer.request_source('https://x.com/i/api/graphql/hash/UserMedia')
        self.assertEqual(self.lease.failure, 'source_request_unavailable')

    def test_rate_limit_hints_are_shared_and_bounded(self):
        response = SimpleNamespace(status_code=200, headers={'x-ratelimit-remaining': '0', 'x-ratelimit-reset': '45.5'})
        self.assertEqual(request_budget.retry_after(response), 46)
        response = SimpleNamespace(status_code=429, headers={'Retry-After': '120'})
        self.assertEqual(request_budget.retry_after(response), 120)
        response.headers['Retry-After'] = '99999999'
        self.assertEqual(request_budget.retry_after(response), 86400)
        response.headers['Retry-After'] = 'invalid'
        self.assertEqual(request_budget.retry_after(response), 60)
