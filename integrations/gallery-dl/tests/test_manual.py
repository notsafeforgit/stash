from copy import deepcopy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest import manual
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.outbox import Conflict, Outbox
from stash_ingest.source_calls import SourceCalls, resolve_once
from test_configuration import profile_fixture
from test_n8n_sources import SourcesRemote, policy_definition
from test_source_management import ENDPOINT
from helpers import ROOT, PRODUCER


def runtime_fixture(path):
    path = Path(path)
    profile_path, profile = profile_fixture(path)
    profile['source_mode'] = 'traversal'
    profile['source_adapter'] = 'gallery-dl'
    profile_path.write_bytes(encode(profile))
    bridge = deepcopy(profile)
    bridge['source_adapter'] = 'yt-dlp'
    bridge['gallery']['extractor']['ytdl'] = {'module': 'yt_dlp'}
    bridge_path = path / 'ytdlp.json'
    bridge_path.write_bytes(encode(bridge))
    (path / 'manual').mkdir()
    config = {'schema': manual.FORMAT, 'state': str(path / 'manual'),
              'profiles': {'gallery-dl': str(profile_path), 'yt-dlp': str(bridge_path)},
              'new_source_policy': policy_definition()}
    config_path = path / 'manual-runtime.json'
    config_path.write_bytes(encode(config))
    return config_path


class ManualIntakeTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.path = Path(temp.name)
        self.config = runtime_fixture(self.path)
        self.runtime = manual.Runtime(self.config)
        self.remote = SourcesRemote()
        self.box = Outbox(self.path / 'outbox.sqlite', ENDPOINT, PRODUCER)
        self.addCleanup(self.box.close)
        self.call = str(uuid.uuid4())
        self.url = 'https://www.reddit.com/user/example/submitted/?sort=new'

    def admit(self, supplied=None):
        return manual.admit(self.remote.app, self.remote.policies, self.remote.producer,
                            self.runtime, self.box, self.call, supplied)

    def test_registration_and_policy_precede_runnable_calls_and_survive_response_loss(self):
        value = manual.options([self.url, 'ytdl:https://thisvid.com/videos/example/'], [], self.config)
        self.remote.drop_next = True
        with self.assertRaises(Unavailable):
            self.admit(value)
        self.assertEqual(SourceCalls(self.box).summary()['calls'], 0)
        original = manual.read(self.runtime.state / self.call / 'request.json')
        self.remote.drop_policy = True
        with self.assertRaises(Unavailable):
            self.admit()
        self.assertEqual(SourceCalls(self.box).summary()['calls'], 0)
        self.runtime = manual.Runtime(self.config)
        groups = self.admit()
        self.assertEqual([group['adapter'] for group in groups], list(manual.ADAPTERS))
        self.assertEqual(len(self.remote.puts), 2)
        self.assertEqual(len(self.remote.policy_puts), 2)
        for source in self.remote.puts:
            self.assertEqual(source['namespace'], '')
            self.assertIsNone(source['account_uuid'])
            self.assertEqual(source['path_prefix'], '.')
        for group in groups:
            summary = SourceCalls(self.box).summary(group['call_uuid'])
            self.assertEqual(summary['definition']['window'], original['window'])
            self.assertEqual(summary['definition']['policy_sha256'], original['profiles'][group['adapter']]['policy_sha256'])
        self.assertEqual(self.admit(), groups)
        self.assertEqual(len(self.remote.puts), 2)
        self.assertEqual(len(self.remote.policy_puts), 2)

    def test_file_inputs_remain_frozen_after_restart(self):
        path = self.path / 'targets.txt'
        path.write_text('# manual targets\n' + self.url + '\n')
        supplied = manual.options([], [path], self.config)
        groups = self.admit(supplied)
        path.unlink()
        self.box.close()
        self.box = Outbox(self.path / 'outbox.sqlite', ENDPOINT, PRODUCER)
        self.addCleanup(self.box.close)
        self.assertEqual(self.admit(supplied), groups)
        with self.assertRaises(Conflict):
            self.admit(manual.options(['https://different.invalid/'], [], self.config))
        self.assertEqual(len(self.remote.puts), 1)

    def test_existing_source_metadata_and_policy_remain_owned_by_stash(self):
        definition = manual.source_definition(self.url, ROOT)
        definition.update(label='Owner label', kind='account', namespace='native:reddit', path_prefix='Original directory')
        source = self.remote.add(definition)
        groups = self.admit(manual.options([self.url], [], self.config))
        self.assertEqual(self.remote.puts, [])
        self.assertEqual(self.remote.policy_puts, [])
        self.assertEqual(self.remote.app.collection(source['uuid']), source)
        self.remote.edit(source['uuid'], state='disabled')
        self.assertEqual(self.admit(), groups, 'a replay never reapplies registration after owner changes')
        resolve_once(SourceCalls(self.box), self.remote.producer)
        page = SourceCalls(self.box).page(groups[0]['call_uuid'])
        self.assertEqual(page[0]['state'], 'review')
        self.assertEqual(self.remote.app.collection(source['uuid'])['state'], 'disabled')

    def test_disabled_or_ambiguous_sources_never_queue_work(self):
        self.remote.add({**manual.source_definition(self.url, ROOT), 'state': 'disabled'})
        with self.assertRaises(Unavailable):
            self.admit(manual.options([self.url], [], self.config))
        self.assertEqual(SourceCalls(self.box).summary()['calls'], 0)
        self.assertEqual(self.remote.puts, [])
        self.remote.add(manual.source_definition(self.url, ROOT))
        with self.assertRaises(Unavailable):
            self.admit()
        self.assertEqual(self.remote.puts, [])

    def test_changed_worker_or_plan_cannot_rewrite_saved_request(self):
        self.remote.drop_next = True
        with self.assertRaises(Unavailable):
            self.admit(manual.options([self.url], [], self.config))
        source_path = self.runtime.state / self.call / 'sources.json'
        body = manual.read(source_path)
        body['intent']['targets'][0]['label'] = 'Changed label'
        body['entries'][0]['input']['label'] = 'Changed label'
        source_path.write_bytes(encode(body))
        with self.assertRaises(InvalidData):
            self.admit()
        self.assertEqual(len(self.remote.puts), 1)
        self.assertEqual(SourceCalls(self.box).summary()['calls'], 0)

    def test_targets_reject_ambiguous_adapter_and_unsupported_cli_flags(self):
        self.assertEqual(manual.targets([self.url, self.url]), [{'url': self.url, 'adapter': 'gallery-dl'}])
        for values in ([self.url, 'ytdl:' + self.url], ['file:///tmp/input'], ['https://user:password@example.invalid/'], []):
            with self.subTest(values=values), self.assertRaises(InvalidData):
                manual.targets(values)
        with patch('stash_ingest.manual.Runtime') as runtime, self.assertRaises(SystemExit) as exited:
            manual.main(['--range', '1-5', self.url])
        self.assertEqual(exited.exception.code, 2)
        runtime.assert_not_called()

    def test_dry_run_has_no_outbox_or_server_writes(self):
        with patch('stash_ingest.manual.Outbox') as outbox, patch('stash_ingest.manual.Client') as client, patch('builtins.print') as output:
            self.assertEqual(manual.main(['--runtime', str(self.config), '--dry-run', 'ytdl:https://thisvid.com/videos/example/']), 0)
        client.assert_not_called()
        outbox.assert_not_called()
        self.assertEqual(list(self.runtime.state.iterdir()), [])
        preview = json.loads(output.call_args.args[0])
        self.assertEqual(preview['targets'][0]['adapter'], 'yt-dlp')

    def test_profile_change_requires_a_new_reviewed_invocation(self):
        self.admit(manual.options([self.url], [], self.config))
        profile_path = self.path / 'worker.json'
        profile = manual.read(profile_path)
        profile['gallery']['extractor']['filename'] = 'changed-{id}.{extension}'
        profile_path.write_bytes(encode(profile))
        self.runtime = manual.Runtime(self.config)
        with self.assertRaises(Conflict):
            self.admit()
        self.assertEqual(len(self.remote.puts), 1)


if __name__ == '__main__':
    unittest.main()
