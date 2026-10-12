import json
import unittest
from unittest.mock import Mock

from gallery_dl import config
from gallery_dl.extractor import reddit, twitter
from stash_ingest import pagination, recent
from stash_ingest.producer import Producer
from stash_ingest.runs import SourcePaused
from stash_ingest.source_window import SourceWindow
import test_producer
from test_producer import reddit_data

PAGE = {'extractor': 'reddit:user-submitted', 'url': 'https://www.reddit.com/user/example/submitted/?sort=new',
        'cursor': 't3_previous_page', 'item': ''}


class PaginationTests(unittest.TestCase):
    setUp = test_producer.ProducerTests.setUp

    def resumed(self, page=None):
        prepared = self.producer.prepare(reddit_data('anchor'))
        anchor, _ = self.producer.cursor(prepared)
        self.lease.run['progress'] = {'items_seen': 10, 'files_completed': 10, 'cursor': anchor}
        if page is not None:
            self.lease.run['progress']['pagination'] = {**page, 'item': anchor}
        return Producer(self.box, self.lease, self.root, extractor_version='fixture'), anchor

    def test_partial_page_keeps_checkpoint_on_http_retry_then_advances(self):
        producer, anchor = self.resumed(PAGE)
        producer.begin_page(PAGE)
        producer.begin_page(PAGE)
        self.assertEqual(producer.resume_cursor, anchor)
        self.assertEqual(self.lease.checkpoints, [])
        prepared = producer.prepare(reddit_data('anchor'))
        key, replay = producer.cursor(prepared)
        producer.checkpoint(key, replay, False)
        prepared = producer.prepare(reddit_data('new'))
        key, replay = producer.cursor(prepared)
        producer.checkpoint(key, replay, True)
        self.assertEqual(self.lease.pages[-1], {**PAGE, 'item': key})
        next_page = {**PAGE, 'cursor': 't3_next'}
        producer.begin_page(next_page)
        self.assertEqual(self.lease.pages[-1], next_page)
        self.assertEqual(self.lease.checkpoints[-1], (11, 11, key))

    def test_legacy_item_checkpoint_is_not_clobbered_by_earlier_pages(self):
        producer, anchor = self.resumed()
        producer.begin_page({**PAGE, 'cursor': ''})
        producer.begin_page(PAGE)
        self.assertEqual(self.lease.checkpoints, [])
        self.assertEqual(producer.resume_cursor, anchor)
        with self.assertRaises(SourcePaused):
            producer.traversed()
        key, replay = producer.cursor(producer.prepare(reddit_data('anchor')))
        producer.checkpoint(key, replay, False)
        self.assertEqual(self.lease.pages[-1], {**PAGE, 'item': anchor})
        producer.traversed()

    def test_missing_anchor_can_advance_only_after_restored_page_was_replayed(self):
        producer, anchor = self.resumed(PAGE)
        with self.assertRaises(SourcePaused):
            producer.traversed()
        producer.begin_page(PAGE)
        key, replay = producer.cursor(producer.prepare(reddit_data('other')))
        producer.checkpoint(key, replay, False)
        self.assertEqual(producer.resume_cursor, anchor)
        producer.begin_page({**PAGE, 'cursor': 't3_next'})
        self.assertEqual(producer.resume_cursor, '')
        self.assertEqual(self.lease.pages[-1]['cursor'], 't3_next')

    def test_pinned_reddit_api_receives_saved_after_on_first_listing_only(self):
        producer, _ = self.resumed(PAGE)
        target = reddit.RedditUserExtractor.from_url(PAGE['url'])
        target.initialize()
        calls = []
        target.api = reddit.RedditAPI(target)
        def call(endpoint, params):
            calls.append((endpoint, dict(params)))
            return {'data': {'children': [], 'after': 't3_next' if len(calls) == 1 else None}}
        target.api._call = call
        target.submissions = lambda: target.api.submissions_user('example/submitted', {})
        pagination.install(target, producer)
        self.assertEqual(list(target.submissions()), [])
        self.assertEqual([c[1]['after'] for c in calls], ['t3_previous_page', 't3_next'])
        self.assertEqual(self.lease.pages[-1]['cursor'], 't3_next')

    def test_twitter_page_cursor_is_used_without_replaying_previous_pages(self):
        config.clear()
        self.addCleanup(config.clear)
        target = twitter.TwitterMediaExtractor.from_url('https://x.com/example/media')
        target.initialize()
        page = {'extractor': 'twitter:media', 'url': target.url, 'cursor': 'actual-remote-cursor', 'item': ''}
        producer, anchor = self.resumed(page)
        target.api._call = Mock(return_value={})
        original = target.api._call
        pagination.install(target, producer)
        self.assertEqual(target._init_cursor(), 'actual-remote-cursor')
        target.api._call('graphql/hash/UserMedia', {'variables': json.dumps({'cursor': target._init_cursor()})})
        self.assertEqual(original.call_count, 1)
        self.assertEqual(producer.resume_cursor, anchor)
        target.api._call('graphql/hash/UserByScreenName', {'variables': '{}'})
        self.assertEqual(producer.pagination, {**page, 'item': anchor})

    def test_old_dates_alone_cannot_stop_a_routine_check(self):
        self.producer.window = SourceWindow({'since': '2026-10-02T00:00:00Z', 'until': '2026-10-04T00:00:00Z'})
        rows = [reddit_data(str(i), id=str(i), stickied=(i == 0)) for i in range(8)]
        seen = []
        for data, _ in recent.posts(((data, ()) for data in rows), self.producer, 'reddit'):
            seen.append(data['id'])
        self.assertEqual(seen, [str(i) for i in range(8)])

    def test_first_wholly_archived_post_stops_without_fetching_more(self):
        rows = [reddit_data(str(i), id=str(i)) for i in range(10)]
        seen = []
        for data, _ in recent.posts(((data, ()) for data in rows), self.producer, 'reddit'):
            seen.append(data['id'])
            self.producer.routine_post_items = 8
            # The first four attachments might be archived, but a new fifth
            # attachment makes this post new and resets the old-post overlap.
            self.producer.routine_post_new = data['id'] == '0'
        self.assertEqual(seen, ['0', '1'])

    def test_pinned_and_replayed_known_posts_do_not_end_the_run(self):
        rows = [reddit_data(str(i), id=str(i), stickied=i == 0) for i in range(5)]
        seen = []
        for data, _ in recent.posts(((data, ()) for data in rows), self.producer, 'reddit'):
            seen.append(data['id'])
            self.producer.routine_post_items = 1
            if data['id'] == '0':
                self.producer.resume_cursor = 'previous-checkpoint'
            if data['id'] == '1':
                self.producer.resume_cursor = ''
        self.assertEqual(seen, ['0', '1', '2'])

    def test_known_twitter_album_is_checked_in_full_before_stopping(self):
        rows = [{'rest_id': str(i)} for i in range(5)]
        seen = []
        for data in recent.posts(iter(rows), self.producer, 'twitter'):
            seen.append(data['rest_id'])
            self.producer.routine_post_items = 4
            self.producer.routine_post_new = data['rest_id'] == '0'
        self.assertEqual(seen, ['0', '1'])

    def test_known_posts_do_not_stop_explicit_archive_recovery(self):
        self.producer.replay_archive = True
        seen = []
        for data, _ in recent.posts(((reddit_data(str(i)), ()) for i in range(5)), self.producer, 'reddit'):
            self.producer.routine_post_items = 1
            seen.append(data['id'])
        self.assertEqual(len(seen), 5)

    def test_silently_omitted_album_items_do_not_establish_a_boundary(self):
        for category, data in [('reddit', {'is_gallery': True, 'gallery_data': {'items': [{'media_id': 'a'}, {'media_id': 'b'}]}}),
                               ('twitter', {'legacy': {'extended_entities': {'media': [{'id_str': 'a'}, {'id_str': 'b'}]}}})]:
            with self.subTest(category=category):
                one = {('native:' + category, 'a')}
                self.assertFalse(recent.complete_manifest(data, category, one))
                self.assertTrue(recent.complete_manifest(data, category, one | {('native:' + category, 'b')}))
