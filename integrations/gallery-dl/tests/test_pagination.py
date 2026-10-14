import json
import unittest
from unittest.mock import Mock

from gallery_dl import config, extractor
from gallery_dl.extractor import instagram, reddit, twitter
from stash_ingest import pagination, recent
from stash_ingest.producer import Producer
from stash_ingest.runs import SourcePaused
from stash_ingest.source_window import SourceWindow
import test_producer
from test_producer import reddit_data

PAGE = {'extractor': 'reddit:user-submitted', 'url': 'https://www.reddit.com/user/example/submitted/?sort=new',
        'cursor': 't3_previous_page', 'item': ''}


class PaginationTests(unittest.TestCase):
    def setUp(self):
        test_producer.ProducerTests.setUp(self)
        # Register complete modules before directly constructing their classes;
        # gallery-dl otherwise mixes compiled and uncompiled regexes by order.
        list(extractor.extractors())

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

    def test_instagram_real_feed_resumes_page_without_affecting_profile_or_story_requests(self):
        config.clear()
        self.addCleanup(config.clear)
        target = instagram.InstagramPostsExtractor.from_url('https://www.instagram.com/example/posts/')
        target.initialize()
        page = {'extractor': 'instagram:posts', 'url': target.url, 'cursor': 'saved-remote-page', 'item': ''}
        producer, anchor = self.resumed(page)
        calls = []
        target.api.user = Mock(return_value={'username': 'example', 'id': '456'})
        target.api._extract_fb_tokens = Mock(return_value=('fixture', 'fixture'))
        target.api._extract_docid = Mock(return_value=None)
        target.cache = lambda fn, *args, **kwargs: fn(*args)

        def request(url, **kwargs):
            body = kwargs.get('data', {})
            calls.append(body)
            if body.get('fb_api_req_friendly_name') == 'PolarisProfilePostsTabContentQuery_connection':
                # The original page must be saved before the request, without
                # pretending its partially downloaded last item was completed.
                variables = json.loads(body['variables'])
                current = variables['after']
                if len([b for b in calls if b.get('fb_api_req_friendly_name') == body['fb_api_req_friendly_name']]) == 1:
                    self.assertEqual(current, 'saved-remote-page')
                    self.assertEqual(producer.resume_cursor, anchor)
                data = {'edges': [{'node': {'pk': current}}],
                        'page_info': {'has_next_page': current == 'saved-remote-page', 'end_cursor': 'following-page'}}
                return Mock(text=json.dumps({'data': {'xdt_api__v1__feed__user_timeline_graphql_connection': data}}))
            return Mock()

        target.request = request
        pagination.install(target, producer)
        target.request(target.root + '/api/graphql', data={'fb_api_req_friendly_name': 'PolarisProfilePageContentQuery'})
        target.request(target.root + '/graphql/query', data={'fb_api_req_friendly_name': 'PolarisStoriesV3ReelPageGalleryQuery'})
        self.assertFalse(producer.pagination_started)
        self.assertEqual(list(target.posts()), [{'pk': 'saved-remote-page'}, {'pk': 'following-page'}])
        self.assertEqual(self.lease.pages[-1]['cursor'], 'following-page')
        self.assertEqual(producer.resume_cursor, '')

    def test_instagram_stories_and_highlights_do_not_use_feed_cursor(self):
        for cls, url in ((instagram.InstagramStoriesExtractor, 'https://www.instagram.com/stories/example/'),
                         (instagram.InstagramHighlightsExtractor, 'https://www.instagram.com/example/highlights/')):
            target = cls.from_url(url)
            target.initialize()
            original = target.request
            pagination.install(target, self.producer)
            self.assertEqual(target.request, original)
            self.producer.scan_mode = 'routine'
            self.assertFalse(recent.chronological(target, self.producer))

    def test_prior_instagram_feed_cannot_consume_another_feeds_saved_attachment(self):
        config.clear()
        self.addCleanup(config.clear)
        target = instagram.InstagramPostsExtractor.from_url('https://www.instagram.com/example/posts/')
        target.initialize()
        page = {'extractor': 'instagram:photos', 'url': 'https://www.instagram.com/example/photos/',
                'cursor': 'saved-photo-page', 'item': ''}
        producer, anchor = self.resumed(page)
        producer.scan_mode = 'routine'
        target.request = Mock()
        pagination.install(target, producer)
        target.request(target.root + '/graphql/query', data={
            'fb_api_req_friendly_name': 'PolarisProfilePostsTabContentQuery_connection',
            'variables': json.dumps({'after': None})})
        self.assertFalse(producer.pagination_started)
        key, replay = producer.cursor(producer.prepare(reddit_data('anchor')))
        producer.checkpoint(key, replay, True)
        self.assertEqual(producer.resume_cursor, anchor)
        self.assertEqual(self.lease.checkpoints, [])
        rows = [{'pk': '1'}, {'pk': '2'}]
        target.posts = lambda: iter(rows)
        recent.install(target, producer)
        visited = []
        for record in target.posts():
            visited.append(record['pk'])
            producer.routine_post_items = 1
            producer.routine_post_attachments = {('native:instagram', record['pk'])}
        self.assertEqual(visited, ['1'], 'a preceding feed can finish at its known boundary')
        with self.assertRaises(SourcePaused):
            producer.traversed()
        producer.begin_page(page)
        key, replay = producer.cursor(producer.prepare(reddit_data('anchor')))
        producer.checkpoint(key, replay, True)
        self.assertEqual(producer.resume_cursor, '')

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
