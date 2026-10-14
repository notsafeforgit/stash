from contextlib import closing
from copy import deepcopy
import json
from unittest.mock import Mock
from urllib.parse import parse_qs, urlsplit
import unittest

from stash_ingest import n8n_sources
from stash_ingest.client import Unavailable
from stash_ingest.encoding import encode
from stash_ingest.launchers import reddit_main
from stash_ingest.outbox import Outbox
from stash_ingest.source_calls import SourceCalls
from stash_ingest.subscriptions import subscribed_targets
import test_launchers
import test_n8n_sources
from helpers import ROOT


class SubscriptionRemote(test_n8n_sources.SourcesRemote):
    def __init__(self):
        super().__init__()
        self.subscriptions = {}
        self.drop_subscription = False

    def request(self, method, path, value=None):
        route = urlsplit(path)
        if route.path == '/subscriptions':
            query = {k: v[0] for k, v in parse_qs(route.query).items()}
            items = []
            for cid, history in sorted(self.histories.items()):
                c = {k: v for k, v in history[-1].items() if k not in ('origin', 'reason', 'recorded_at')}
                if any(c[key] != query[param] for param, key in (('collection', 'uuid'), ('root', 'root_uuid'), ('namespace', 'namespace')) if param in query):
                    continue
                items.append({'collection': c, **self.subscriptions.get(cid, {'enabled': False, 'revision': 0})})
            return {'items': deepcopy(items), 'total': len(items)}
        if route.path.endswith('/subscription'):
            cid = route.path.split('/')[2]
            assert method == 'PUT'
            current = self.subscriptions.setdefault(cid, {'enabled': False, 'revision': 0})
            if value['expected_revision'] != current['revision']:
                raise Unavailable('conflict', 409)
            if current['enabled'] != value['enabled']:
                current.update(enabled=value['enabled'], revision=current['revision'] + 1)
            if self.drop_subscription:
                self.drop_subscription = False
                raise Unavailable('network_unavailable')
            return self.request('GET', '/subscriptions?collection=' + cid)['items'][0]
        return super().request(method, path, value)


class NativeSubscriptionWorkflowsTests(unittest.TestCase):
    setUp = test_n8n_sources.NativeSourceWorkflowsTests.setUp
    request = test_n8n_sources.NativeSourceWorkflowsTests.request
    run_request = test_n8n_sources.NativeSourceWorkflowsTests.run_request

    def configured(self):
        value = json.loads(self.config.read_bytes())
        value['version'] = 2
        del value['lists']
        self.config.write_bytes(encode(value))
        self.runtime = n8n_sources.Runtime(self.config)
        self.remote = SubscriptionRemote()
        self.reddit.unlink()
        self.twitter.unlink()

    def test_add_remove_and_replay_use_only_stash_intent(self):
        self.configured()
        call, value = self.request()
        result = self.run_request(call, value)
        cid = next(iter(self.remote.histories))
        self.assertTrue(self.remote.subscriptions[cid]['enabled'])
        self.assertTrue(self.run_request(*self.request(action='remove', execution='101'))['complete'])
        self.assertFalse(self.remote.subscriptions[cid]['enabled'])
        self.assertEqual(self.remote.histories[cid][-1]['state'], 'active')
        self.assertEqual(self.run_request(call, value), result)
        self.assertFalse(self.remote.subscriptions[cid]['enabled'], 'completed add cannot undo a later unsubscribe')
        self.assertFalse(self.reddit.exists())

    def test_lost_subscription_response_recovers_without_resubscribing(self):
        self.configured()
        self.remote.drop_subscription = True
        call, value = self.request()
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        self.assertTrue(self.run_request(call, value)['complete'])
        self.assertEqual(next(iter(self.remote.subscriptions.values()))['revision'], 1)

    def test_later_owner_edit_conflicts_with_unfinished_workflow(self):
        self.configured()
        self.remote.drop_subscription = True
        call, value = self.request()
        with self.assertRaises(Unavailable):
            self.run_request(call, value)
        next(iter(self.remote.subscriptions.values())).update(enabled=False, revision=2)
        with self.assertRaises(Unavailable):
            self.run_request(call, value)


class SubscriptionLauncherTests(unittest.TestCase):
    setUp = test_launchers.LauncherTests.setUp
    invoke = test_launchers.LauncherTests.invoke

    def test_monitored_reddit_queues_daily_profile_and_search_for_one_subscription(self):
        cid = '10000000-0000-4000-8000-000000000001'
        item = {'enabled': True, 'revision': 7, 'collection': {'uuid': cid, 'root_uuid': ROOT,
            'namespace': 'native:reddit', 'target_url': 'https://www.reddit.com/user/example/', 'state': 'active'}}
        client = Mock()
        client.capabilities.return_value = {'source_subscriptions': True}
        client._request.return_value = {'items': [item], 'total': 1}
        targets, bindings = subscribed_targets(client, ROOT, 'reddit')
        self.assertEqual(targets, ['https://www.reddit.com/user/example/submitted/?sort=new',
            'https://www.reddit.com/search?q=author%3Aexample+nsfw%3Ayes&include_over_18=on&sort=new&t=all'])
        self.assertEqual(bindings, {url: {'collection_uuid': cid, 'revision': 7} for url in targets})
        spec = {'targets': targets, 'subscriptions': bindings, 'root_uuid': ROOT, 'operation': 'download',
                'policy_sha256': 'a' * 64, 'cooldown_seconds': 0, 'window': {'since': None, 'until': '2026-10-01T12:00:00Z'}}
        with closing(Outbox(self.outbox, 'http://fixture.invalid', self.base[self.base.index('--producer')+1])) as box:
            calls = SourceCalls(box)
            calls.record(self.call, 'b' * 64, lambda: spec)
            delivery = calls.claim('20000000-0000-4000-8000-000000000001')
            # Both passes are queued before either is scraped: an unavailable
            # profile must not prevent the independent search from running.
            for target in delivery.targets:
                url = target['target_url']
                calls.bind(delivery, target, {'target_url': url, 'state': 'resolved', 'has_more': False,
                    'candidates': [{'collection_uuid': cid, 'collection_revision': 3, 'state': 'active', 'retrieval_url': url}]})
            templates = [json.loads(row[0]) for row in box.db.execute('SELECT template FROM run_intents')]
            self.assertEqual({template['retrieval_url'] for template in templates}, set(targets))
            self.assertEqual(len(templates), 2)
            for template in templates:
                self.assertEqual(template['subscription_revision'], 7)
                self.assertEqual(template['collection_uuid'], cid)
            self.assertEqual(calls.summary(self.call)['counts']['queued'], 2)
        for changed in ({'enabled': False}, {'revision': 0}, {'collection': {**item['collection'], 'root_uuid': cid}}):
            client._request.return_value = {'items': [{**item, **changed}], 'total': 1}
            with self.assertRaises(Unavailable):
                subscribed_targets(client, ROOT, 'reddit')

    def test_subscription_source_refuses_local_list_or_saved_override(self):
        for args in (['--config-file', '/unused'], ['--saved']):
            status, _, errors = self.invoke(reddit_main, ['--subscriptions', *args])
            self.assertEqual(status, 1)
            self.assertIn('API list', errors)
