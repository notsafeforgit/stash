import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest import album_backfill, post_media_backfill
from stash_ingest.album_client import POLICIES
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData
from test_album_backfill import MemoryAlbumClient
from test_post_media_backfill import MemoryPostClient


def identity(post, current):
    def item(key):
        return {"uuid": key, "canonical_uuid": current, "redirect_to": None if key == current else current,
                "state": "active", "revision": 1, "created_at": "2026-10-06T00:00:00Z"}
    return {"requested": item(post), "canonical": item(current)}


class CanonicalBackfillClientTests(unittest.TestCase):
    def variants(self):
        return [(MemoryAlbumClient, album_backfill), (MemoryPostClient, post_media_backfill)]

    def prepare(self, module, client, output, posts):
        args = (POLICIES[0],) if module is album_backfill else ()
        return module.prepare(client, output, *args, posts=posts)

    def records(self, module, plan):
        return [plan.record(post) for post in plan.items] if module is album_backfill else list(plan.records())

    def test_new_explicit_plans_resolve_aliases_once_and_saved_work_never_resolves_again(self):
        for factory, module in self.variants():
            with self.subTest(client=factory.__name__), tempfile.TemporaryDirectory() as directory:
                client = factory(2)
                current = list(client.previews)
                aliases = [str(uuid.uuid4()), str(uuid.uuid4())]
                owners = {aliases[0]: current[1], aliases[1]: current[1]}
                original_request = client.request
                lookups, allow_lookup = [], True

                def request(method, path, value=None, **kwargs):
                    if path.endswith('/identity'):
                        self.assertTrue(allow_lookup, "saved requests must keep their original post")
                        self.assertEqual('GET', method)
                        post = path.split('/')[2]
                        lookups.append(post)
                        result = identity(post, owners.get(post, post))
                        return (200, result) if module is album_backfill else result
                    return original_request(method, path, value, **kwargs)

                with patch.object(client, 'request', side_effect=request):
                    output = Path(directory) / 'plan'
                    report = self.prepare(module, client, output, [aliases[0], current[0], aliases[1], current[1]])
                    plan = module.Plan(output, report['plan_sha256'], client.endpoint)
                    records = self.records(module, plan)
                    self.assertEqual(current, [record['preview']['post_uuid'] for record in records])
                    self.assertEqual(2, report['posts'])
                    self.assertEqual(4, len(lookups))
                    self.assertEqual(2, sum('preview' in path for _, path, _ in client.calls))
                    self.assertFalse(client.jobs if module is album_backfill else client.receipts)
                    saved = {p.name: p.read_bytes() for p in output.iterdir()}
                    if module is album_backfill:
                        client.lost.add(f'/posts/{current[0]}/album-backfills')
                    else:
                        client.lost.add(current[0])
                    with self.assertRaises(Unavailable):
                        module.inspect_plan(client, plan, apply=True)
                    allow_lookup = False
                    owners[current[0]] = str(uuid.uuid4())
                    module.inspect_plan(client, module.Plan(output, plan.sha256, client.endpoint), apply=True)
                    self.assertEqual(2, client.post_requests if module is album_backfill else client.posts_count)
                    self.assertEqual(saved, {p.name: p.read_bytes() for p in output.iterdir()})
                    before = len(client.calls)
                    with self.assertRaises(InvalidData):
                        self.prepare(module, client, output, aliases)
                    self.assertEqual(before, len(client.calls))

    def test_new_merge_after_preparation_keeps_rejected_work_at_its_original_post(self):
        for factory, module in self.variants():
            with self.subTest(client=factory.__name__), tempfile.TemporaryDirectory() as directory:
                client = factory()
                post = next(iter(client.previews))
                output = Path(directory) / 'plan'
                original_request = client.request
                merged = False
                writes = []

                def request(method, path, value=None, **kwargs):
                    if path.endswith('/identity'):
                        self.assertFalse(merged)
                        result = identity(post, post)
                        return (200, result) if module is album_backfill else result
                    if method == 'POST' and path.endswith(('album-backfills', 'media-backfills')):
                        writes.append(path)
                        if merged:
                            raise Unavailable('preview_changed', 409)
                    return original_request(method, path, value, **kwargs)

                with patch.object(client, 'request', side_effect=request):
                    report = self.prepare(module, client, output, [post])
                    plan = module.Plan(output, report['plan_sha256'], client.endpoint)
                    saved = {p.name: p.read_bytes() for p in output.iterdir()}
                    merged = True
                    result = module.inspect_plan(client, plan, apply=True)
                    self.assertTrue(result['needs_review'])
                    self.assertEqual(1, len(writes))
                    self.assertIn('/posts/' + post + '/', writes[0])
                    self.assertFalse(client.jobs if module is album_backfill else client.receipts)
                    self.assertEqual(saved, {p.name: p.read_bytes() for p in output.iterdir()})

    def test_wrong_or_incomplete_identity_context_cannot_redirect_a_plan(self):
        post, current = str(uuid.uuid4()), str(uuid.uuid4())
        mutations = [
            lambda value: value['requested'].update(uuid=current),
            lambda value: value['requested'].update(canonical_uuid=post),
            lambda value: value['canonical'].update(redirect_to=post),
            lambda value: value['requested'].update(redirect_to=None),
            lambda value: value['canonical'].update(revision=True),
            lambda value: value['canonical'].pop('redirect_to'),
        ]
        for factory, module in self.variants():
            for change in mutations:
                client = factory()
                value = copy.deepcopy(identity(post, current))
                change(value)
                response = (200, value) if module is album_backfill else value
                with self.subTest(client=factory.__name__, value=value), patch.object(client, 'request', return_value=response):
                    with self.assertRaises(Unavailable):
                        client.current_post(post)
