"""Resume pinned gallery-dl listing extractors at their current remote page.

Only listing requests own this state. Profile lookups and external media children
must never replace the parent page cursor. Each page start is durable before its
request, and the producer attaches its last completed item to the same checkpoint.
"""

import json
from urllib.parse import urlsplit

from .encoding import InvalidData

KEYS = {'extractor', 'url', 'cursor', 'item'}
TWITTER_LISTINGS = frozenset({'UserMedia', 'UserTweets', 'UserTweetsAndReplies',
                             'UserHighlightsTweets', 'Likes', 'SearchTimeline',
                             'ListLatestTweetsTimeline', 'Bookmarks'})


def validate(page):
    if not isinstance(page, dict) or set(page) != KEYS:
        raise InvalidData('Invalid source pagination checkpoint')
    for key, limit in (('extractor', 80), ('url', 4096), ('cursor', 8192), ('item', 1024)):
        value = page[key]
        if (not isinstance(value, str) or len(value.encode()) > limit
                or any(ord(char) < 32 or ord(char) == 127 for char in value)):
            raise InvalidData('Invalid source pagination value')
    parsed = urlsplit(page['url'])
    if (not page['extractor'].startswith(('twitter:', 'reddit:', 'instagram:')) or parsed.scheme != 'https'
            or not parsed.hostname or parsed.username is not None or parsed.password is not None or parsed.fragment):
        raise InvalidData('Invalid source pagination target')
    return dict(page)


def install(target, producer):
    if target.category not in ('twitter', 'reddit', 'instagram'):
        return
    category = target.category + ':' + target.subcategory
    saved = producer.saved_pagination
    resume = saved if saved is not None and (saved['extractor'], saved['url']) == (category, target.url) else None
    if target.category == 'instagram':
        # The pinned post/photo feed uses one GraphQL connection. Profile
        # lookups, story reels and individual media are different requests and
        # must neither consume its saved cursor nor replace its checkpoint.
        if target.subcategory not in ('posts', 'photos'):
            return
        original_config, original_request = target.config, target.request

        def configuration(key, default=None):
            if key == 'cursor' and resume is not None:
                return resume['cursor'] or True
            return original_config(key, default)

        def request(url, *args, **kwargs):
            body = kwargs.get('data')
            if (url == target.root + '/graphql/query' and isinstance(body, dict)
                    and body.get('fb_api_req_friendly_name') == 'PolarisProfilePostsTabContentQuery_connection'
                    and (saved is None or producer.pagination_started or resume is not None)):
                variables = json.loads(body['variables'])
                producer.begin_page({'extractor': category, 'url': target.url,
                                     'cursor': variables.get('after') or '', 'item': ''})
            return original_request(url, *args, **kwargs)

        target.config, target.request = configuration, request
    elif target.category == 'twitter' and hasattr(target, 'api'):
        original_config = target.config

        def configuration(key, default=None):
            if key == 'cursor' and resume is not None:
                return resume['cursor'] or True
            return original_config(key, default)

        target.config = configuration
        original_call = target.api._call

        def call(endpoint, params, *args, **kwargs):
            if endpoint.rsplit('/', 1)[-1] in TWITTER_LISTINGS:
                variables = json.loads(params.get('variables') or '{}')
                cursor = target._cursor or (resume['cursor'] if resume is not None else '')
                if target.subcategory != 'timeline':
                    cursor = variables.get('cursor') or ''
                producer.begin_page({'extractor': category, 'url': target.url, 'cursor': cursor, 'item': ''})
            return original_call(endpoint, params, *args, **kwargs)

        target.api._call = call
    elif hasattr(target, 'submissions'):
        original_submissions = target.submissions
        installed = False

        def submissions():
            nonlocal installed
            if not installed:
                installed = True
                original_call = target.api._call
                first = True

                def call(endpoint, params, *args, **kwargs):
                    nonlocal first
                    # about.json, comments and authentication are not pages.
                    listing = endpoint.endswith('/.json') and not endpoint.startswith('/comments/')
                    if listing:
                        if first and resume is not None:
                            if resume['cursor']:
                                params['after'] = resume['cursor']
                            else:
                                params.pop('after', None)
                        first = False
                        producer.begin_page({'extractor': category, 'url': target.url,
                                             'cursor': params.get('after') or '', 'item': ''})
                    return original_call(endpoint, params, *args, **kwargs)

                target.api._call = call
            return original_submissions()

        target.submissions = submissions
