"""Bound routine chronological checks by whole posts, including a small overlap."""

from urllib.parse import parse_qs, urlsplit

from .source_window import published

OVERLAP_POSTS = 4


def chronological(target, producer):
    if producer.scan_mode != 'routine':
        return False
    if target.category == 'twitter':
        return target.subcategory in ('media', 'tweets', 'replies', 'timeline')
    if target.category == 'reddit':
        return (target.subcategory in ('user', 'user-submitted', 'subreddit', 'subreddit-new', 'home-search', 'subreddit-search')
                and parse_qs(urlsplit(target.url).query).get('sort', ['new'] if target.subcategory in ('user', 'user-submitted', 'subreddit-new') else []) == ['new'])
    return False


def posts(iterator, producer, category):
    older, previous = 0, None
    for entry in iterator:
        data = entry[0] if category == 'reddit' else entry
        if not isinstance(data, dict):
            yield entry
            continue
        legacy = data.get('legacy', data)
        key = data.get('rest_id') or data.get('id') or legacy.get('id_str')
        if not key or key == previous:
            yield entry
            continue
        previous = key
        pinned = data.get('stickied') or data.get('pinned') or data.get('_pinned')
        if not pinned and producer.window.since is not None:
            value = published(data, category)
            older = older + 1 if producer.window.since is not None and value < producer.window.since else 0
        # A yielded post is consumed fully, with all media and external-host
        # children, before deciding to stop. Attachment counts are not posts.
        producer.routine_post_items = 0
        producer.routine_post_new = False
        yield entry
        if producer.window.since is None and not pinned:
            older = older + 1 if producer.routine_post_items and not producer.routine_post_new else 0
        if older >= OVERLAP_POSTS and not producer.resume_cursor:
            return


def configure(target, producer):
    original = target.config

    def configuration(key, default=None):
        if key == 'skip' and chronological(target, producer):
            return True
        if target.category == 'reddit' and key == 'comments':
            return 0
        if target.category == 'twitter':
            if key in ('expand', 'quoted-expand', 'showreplies', 'showmore', 'metadata-user'):
                return False
            if producer.scan_mode == 'routine' and key == 'include' and not producer.resume_cursor and producer.saved_pagination is None:
                return ('media',)
        return original(key, default)

    target.config = configuration


def install(target, producer):
    if not chronological(target, producer):
        return
    if target.category == 'twitter':
        # Direct UserMedia has no automatic search fall-through. A resumed
        # legacy timeline keeps its original traversal until it completes.
        original = target.tweets
        target.tweets = lambda: posts(original(), producer, 'twitter')
    else:
        original = target.submissions
        target.submissions = lambda: posts(original(), producer, 'reddit')
