"""Stop routine chronological checks at a wholly downloaded, unpinned post."""

from urllib.parse import parse_qs, urlsplit


def chronological(target, producer):
    if producer.scan_mode != 'routine':
        return False
    if target.category == 'twitter':
        return target.subcategory in ('media', 'tweets', 'replies', 'timeline')
    if target.category == 'reddit':
        return (target.subcategory in ('user', 'user-submitted', 'subreddit', 'subreddit-new', 'home-search', 'subreddit-search')
                and parse_qs(urlsplit(target.url).query).get('sort', ['new'] if target.subcategory in ('user', 'user-submitted', 'subreddit-new') else []) == ['new'])
    if target.category == 'instagram':
        # Stories are oldest first and highlight containers can gain items.
        # Neither can stop at an archived item. Only the profile post feed can.
        return target.subcategory in ('posts', 'photos')
    return False


def posts(iterator, producer, category, *, resume_scope=True):
    previous = None
    for entry in iterator:
        data = entry[0] if category == 'reddit' else entry
        if not isinstance(data, dict):
            yield entry
            continue
        legacy = data.get('legacy', data)
        key = data.get('pk') if category == 'instagram' else data.get('rest_id') or data.get('id') or legacy.get('id_str')
        if not key or key == previous:
            yield entry
            continue
        previous = key
        pinned = data.get('stickied') or data.get('pinned') or data.get('_pinned')
        if category == 'instagram':
            pinned = pinned or data.get('timeline_pinned_user_ids') or data.get('clips_tab_pinned_user_ids')
        replaying = (resume_scope and bool(producer.resume_cursor)) or producer.replay_archive
        # A yielded post is consumed fully, with all media and external-host
        # children, before deciding to stop. Attachment counts are not posts.
        producer.routine_post_items = 0
        producer.routine_post_new = False
        producer.routine_post_attachments = set()
        yield entry
        if (not pinned and not replaying and (not resume_scope or not producer.resume_cursor)
                and producer.routine_post_items and not producer.routine_post_new
                and complete_manifest(data, category, producer.routine_post_attachments)):
            return


def complete_manifest(data, category, downloaded):
    """A silently omitted album item must not turn a partial post into a boundary."""
    if category == 'instagram':
        from .instagram import original_media
        _, _, entries = original_media(data)
        key = 'pk'
    elif category == 'reddit':
        parents = data.get('crosspost_parent_list')
        if parents:
            if not isinstance(parents, list) or not isinstance(parents[-1], dict):
                return False
            data = parents[-1]
        if data.get('is_gallery') or 'gallery_data' in data:
            gallery = data.get('gallery_data')
            entries = gallery.get('items') if isinstance(gallery, dict) else None
            key = 'media_id'
        else:
            return True
    else:
        legacy = data.get('legacy', data)
        entities = legacy.get('extended_entities')
        if entities is None:
            return True
        entries = entities.get('media') if isinstance(entities, dict) else None
        key = 'id_str'
    if not isinstance(entries, list) or not entries:
        return False
    for entry in entries:
        if not isinstance(entry, dict):
            return False
        identity = entry.get(key, entry.get('id') if category == 'twitter' else None)
        if identity is None or ('native:' + category, str(identity)) not in downloaded:
            return False
    return True


def configure(target, producer):
    original = target.config

    def configuration(key, default=None):
        # A child extractor's skip:N must not abort halfway through an album.
        # Only the listing's whole-post boundary is allowed to end a routine run.
        if key == 'skip' and producer.scan_mode == 'routine':
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
    elif target.category == 'instagram':
        original = target.posts
        saved = producer.saved_pagination
        resume_scope = saved is None or (saved['extractor'], saved['url']) == ('instagram:' + target.subcategory, target.url)
        target.posts = lambda: posts(original(), producer, 'instagram', resume_scope=resume_scope)
    else:
        original = target.submissions
        target.submissions = lambda: posts(original(), producer, 'reddit')
