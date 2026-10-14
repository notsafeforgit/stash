"""Patch updates preserve policies, checkpoint prefixes and bounded discovery."""
from contextlib import closing
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from stash_ingest.configuration import Configuration
from stash_ingest.configuration_store import Configurations
from stash_ingest.metadata_bundle import Bundle
from stash_ingest.metadata_fetch import collect
from stash_ingest.outbox import Outbox
from stash_ingest.runtime import CAPTURE_VERSION, LEGACY_CAPTURE_VERSIONS, REPOSITORIES, ready_jobs
from helpers import PRODUCER
from test_configuration import profile_fixture
from test_metadata_fetch import URL, CHILD, Post, Child, factory, post_data
from gallery_dl.extractor.common import Message


class RuntimeTests(unittest.TestCase):
    def test_saved_policy_survives_upstream_revision_and_version_changes(self):
        with tempfile.TemporaryDirectory() as temporary:
            path, _ = profile_fixture(temporary)
            before = Configuration(path)
            with closing(Outbox(Path(temporary) / 'outbox.sqlite', 'http://127.0.0.1:9999', PRODUCER)) as box:
                saved = Configurations(box)
                saved.remember(before)
                def package(name):
                    return SimpleNamespace(version='2.0.dev0', read_text=lambda _: json.dumps({
                        'url': REPOSITORIES[name], 'vcs_info': {'vcs': 'git', 'commit_id': 'f' * 40}}))
                with patch('stash_ingest.runtime.distribution', side_effect=package):
                    row, = saved.rows(path)
                    self.assertEqual(saved.load(row).policy_sha256, before.policy_sha256)

    def test_old_job_pages_merge_in_sequence_order_without_skipping_next_page(self):
        client = Mock()
        client.ready_jobs.side_effect = lambda c, p, v, **kw: {
            CAPTURE_VERSION: [{'uuid': 'new', 'sequence': 4}],
            LEGACY_CAPTURE_VERSIONS[0]: [{'uuid': 'old', 'sequence': 2}, {'uuid': 'later', 'sequence': 5}],
            LEGACY_CAPTURE_VERSIONS[1]: [{'uuid': 'oldest', 'sequence': 3}],
        }[v]
        config = SimpleNamespace(policy_sha256='a' * 64, extractor_version=CAPTURE_VERSION)
        self.assertEqual([row['sequence'] for row in ready_jobs(client, 'collection', config, after=1, limit=2)], [2, 3])
        for call in client.ready_jobs.call_args_list:
            self.assertEqual(call.kwargs, {'after': 1, 'limit': 2})

    def test_legacy_checkpoint_keeps_prefix_and_fetches_only_unfinished_child(self):
        legacy = LEGACY_CAPTURE_VERSIONS[0]
        messages = [(Message.Directory, '', post_data()), (Message.Queue, CHILD, post_data())]
        parent = factory(Post, URL, messages)
        child = factory(Child, CHILD, [(Message.Directory, '', {'id': 'clip'})])
        first = collect(URL, {}, factory=lambda url: parent if url == URL else child,
                        reserve_source=lambda url: url == URL, checkpoint_version=legacy)
        self.assertEqual(first['pending'][0]['reason'], 'source_busy')
        prefix = first['records'][:]
        requested = []
        def resume(url):
            requested.append(url)
            self.assertEqual(url, CHILD)
            return child
        result = collect(URL, {}, resume=first, factory=resume, checkpoint_version=legacy)
        self.assertEqual(result['extractor_version'], legacy)
        self.assertEqual(result['records'][:len(prefix)], prefix)
        self.assertEqual(result['pending'], [])
        self.assertEqual(requested, [CHILD])
        Bundle(URL, legacy, result)
