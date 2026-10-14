import copy
import json
from contextlib import closing
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from stash_ingest.configuration import Configuration, LAYERED_SCHEMA
from stash_ingest.configuration_store import Configurations
from stash_ingest.encoding import InvalidData
from stash_ingest.outbox import Outbox
from stash_ingest.profile_dispatch import ProfileDispatcher
from stash_ingest.source_calls import SourceCalls
from helpers import PRODUCER
from test_configuration import profile_fixture


def layered_fixture(directory):
    filename, flat = profile_fixture(directory)
    base = filename.parent / 'gallery.json'
    overlay = filename.parent / 'override.json'
    raw = {'extractor': {'base-directory': flat['root']['path'],
                        'filename': {'extension == "jpg"': 'photo.{extension}', '': '{id}.{extension}'},
                        'sleep': 1, 'postprocessors': ['mtime'],
                        'twitter': {'cookies': {'auth_token': 'private-token'}, 'retweets': False}}}
    base.write_text(json.dumps(raw))
    overlay.write_text(json.dumps({'extractor': {'sleep': 2, 'postprocessors': [], 'twitter': {'retweets': True}}}))
    profile = {'schema': LAYERED_SCHEMA, 'root': flat['root'], 'locks': flat['locks'],
               'config_files': ['gallery.json', 'override.json'], 'source_category': 'twitter',
               'scan_mode': 'initial', 'full_history': True}
    filename.write_text(json.dumps(profile))
    return filename, base, overlay, raw


class LayeredConfigurationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.filename, self.base, self.overlay, self.raw = layered_fixture(self.directory)
        self.now = 1000
        self.client = Mock(endpoint='http://fixture.invalid', producer=PRODUCER)

    def open(self):
        return Outbox(self.directory / 'outbox.sqlite', self.client.endpoint, PRODUCER, clock=lambda: self.now)

    def edit(self):
        value = copy.deepcopy(self.raw)
        value['extractor']['filename'] = 'changed-{id}.{extension}'
        self.base.write_text(json.dumps(value))

    def test_native_merge_order_lists_and_new_config_edits(self):
        first = Configuration(self.filename)
        self.assertEqual(first._gallery['extractor']['sleep'], 2)
        self.assertEqual(first._gallery['extractor']['postprocessors'], [])
        self.assertTrue(first._gallery['extractor']['twitter']['retweets'])
        self.assertTrue(first._gallery['skip'])
        self.assertEqual(list(first._gallery['extractor']['filename']), ['extension == "jpg"', ''])
        self.assertNotIn('private-token', json.dumps(first.document))
        self.edit()
        second = Configuration(self.filename)
        self.assertNotEqual(first.policy_sha256, second.policy_sha256)
        self.assertEqual(second._gallery['extractor']['filename'], 'changed-{id}.{extension}')
        self.assertIsInstance(first._gallery['extractor']['filename'], dict)

    def test_caller_replay_and_restarts_keep_old_policy_without_copying_secrets(self):
        calls = ['1bb79318-04cf-459a-9cdc-5cfa4f5b9c83', 'b831b233-8502-48f3-9b8e-1eb7a2fc3ee8']
        with closing(self.open()) as box:
            def prepare():
                config = Configuration(self.filename)
                Configurations(box).remember(config)
                return {'root_uuid': config.root_uuid, 'policy_sha256': config.policy_sha256, 'operation': 'download',
                        'targets': ['https://x.com/example'], 'cooldown_seconds': 0,
                        'window': {'since': None, 'until': '2026-10-11T00:00:00Z'}}
            first = SourceCalls(box).record(calls[0], 'a' * 64, prepare)
            self.edit()
            self.assertEqual(SourceCalls(box).record(calls[0], 'a' * 64, lambda: self.fail('re-read a queued caller')), first)
            second = SourceCalls(box).record(calls[1], 'b' * 64, prepare)
            self.assertNotEqual(first['definition']['policy_sha256'], second['definition']['policy_sha256'])
        with closing(self.open()) as box:
            rows = Configurations(box).rows(self.filename)
            self.assertEqual(len(rows), 2)
            self.assertTrue(all(b'private-token' not in row['document'] for row in rows))
            old = next(Configurations.load(r) for r in rows if r['policy_sha256'] == first['definition']['policy_sha256'])
            self.assertIsInstance(old._gallery['extractor']['filename'], dict)
            self.assertEqual(box.db.execute('PRAGMA user_version').fetchone()[0], 18)

    def test_dispatch_rotates_retained_jobs_and_recovers_when_new_layer_is_broken(self):
        with closing(self.open()) as box:
            old = Configuration(self.filename)
            Configurations(box).remember(old)
        self.edit()
        used = []
        def dispatcher(box, client, config):
            used.append(config.policy_sha256)
            return Mock(once=lambda: {'state': 'yielded'})
        with patch('stash_ingest.profile_dispatch.Dispatcher', side_effect=dispatcher):
            for _ in range(2):
                with closing(self.open()) as box:
                    self.assertEqual(ProfileDispatcher(box, self.client, self.filename).once()['state'], 'yielded')
            self.assertEqual(set(used), {old.policy_sha256, Configuration(self.filename).policy_sha256})
            # A non-credential layer can be missing while the saved policy's
            # credential reference in gallery.json is still usable.
            self.overlay.unlink()
            with closing(self.open()) as box:
                self.assertEqual(ProfileDispatcher(box, self.client, self.filename).once()['state'], 'yielded')

    def test_saved_policy_rejects_tampering_and_runtime_changes(self):
        with closing(self.open()) as box:
            saved = Configurations(box)
            saved.remember(Configuration(self.filename))
            row = dict(saved.rows(self.filename)[0])
            row['document'] += b' '
            with self.assertRaises(InvalidData):
                saved.load(row)
            with patch('stash_ingest.configuration.policy_identity', return_value={'adapter_sha256': 'different'}):
                with self.assertRaises(InvalidData):
                    saved.load(saved.rows(self.filename)[0])

    def test_old_empty_policies_are_not_polled_on_every_tick(self):
        with closing(self.open()) as box:
            Configurations(box).remember(Configuration(self.filename))
            self.edit()
            with patch('stash_ingest.profile_dispatch.Dispatcher') as dispatch:
                dispatch.return_value.once.return_value = {'state': 'idle'}
                ProfileDispatcher(box, self.client, self.filename).once()
                self.assertEqual(dispatch.call_count, 2)
                ProfileDispatcher(box, self.client, self.filename).once()
                self.assertEqual(dispatch.call_count, 3)
