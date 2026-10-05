"""Bluesky/TikTok source membership before downloader selection or mutation."""

import copy
import re
import types
from urllib.parse import unquote, urlsplit

from .encoding import InvalidData


def _array(value):
    if not isinstance(value, (list, tuple)) or len(value) > 4096:
        raise InvalidData('Source media requires a bounded original attachment list')
    return value


def _blob(item, kind):
    if item is None:
        return None
    if not isinstance(item, dict):
        raise InvalidData('Invalid Bluesky source media')
    blob = item.get(kind)
    if blob is None:
        return None
    if not isinstance(blob, dict):
        raise InvalidData('Invalid Bluesky source blob')
    ref = blob.get('ref')
    if ref is None:
        ref = {}
    if not isinstance(ref, dict):
        raise InvalidData('Invalid Bluesky blob reference')
    values = {value for value in (ref.get('$link'), blob.get('cid')) if isinstance(value, str) and value}
    if not values and ref.get('$link') is None and blob.get('cid') is None:
        return None
    if (len(values) != 1 or any(value is not None and not isinstance(value, str) for value in (ref.get('$link'), blob.get('cid')))
            or not re.fullmatch(r'[A-Za-z0-9]{1,256}', next(iter(values)))
            or not isinstance(blob.get('mimeType'), str) or not blob['mimeType'].startswith(kind + '/')):
        raise InvalidData('Bluesky blob identity or media kind is contradictory')
    return {'id': next(iter(values)), 'kind': kind}


def bluesky_membership(data):
    media = data.get('embed')
    if media is None:
        return []
    if not isinstance(media, dict):
        raise InvalidData('Invalid Bluesky source embed')
    if 'media' in media:
        media = media['media']
        if not isinstance(media, dict):
            raise InvalidData('Invalid Bluesky embedded media')
    items = []
    if 'images' in media:
        items.extend(_blob(item, 'image') for item in _array(media['images']))
    if 'items' in media:
        for item in _array(media['items']):
            if item is not None and (not isinstance(item, dict) or 'image' in item and 'video' in item):
                raise InvalidData('Ambiguous Bluesky source media')
            items.append(_blob(item, 'image' if isinstance(item, dict) and 'image' in item else 'video'))
    if 'video' in media:
        items.append(_blob(media, 'video'))
    return list(_array(items))


def tiktok_image_key(url):
    if (not isinstance(url, str) or len(url.encode('utf-8')) > 16384
            or any(c.isspace() or ord(c) < 32 or 127 <= ord(c) < 160 for c in url)):
        raise InvalidData('Invalid TikTok original image URL')
    try:
        parsed = urlsplit(url)
        if (parsed.scheme not in ('https', 'http') or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.port is not None or parsed.fragment
                or re.search(r'%(?![0-9a-fA-F]{2})', parsed.path)):
            raise ValueError()
        filename = unquote(parsed.path.rsplit('/', 1)[-1])
        name, dot, extension = filename.rpartition('.')
        # Match the pinned nameext_from_url/file_id transformation. Signed
        # query parameters, hosts and rendition suffixes do not identify media.
        key = (name if dot and name and len(extension) <= 16 else filename).partition('~')[0]
        if not re.fullmatch(r'[A-Za-z0-9_.-]{1,256}', key) or key in ('.', '..'):
            raise ValueError()
        return 'image:' + key
    except ValueError:
        raise InvalidData('TikTok image has no stable source key') from None


def tiktok_image(item):
    if item is None:
        return None
    if not isinstance(item, dict):
        raise InvalidData('Invalid TikTok source image')
    image = item.get('imageURL')
    if image is None:
        return None
    if not isinstance(image, dict):
        raise InvalidData('Invalid TikTok source image URLs')
    urls = image.get('urlList')
    if urls is None or urls == []:
        return None
    urls = _array(urls)
    return {'id': tiktok_image_key(urls[0]), 'kind': 'image'}


def tiktok_membership(data):
    from .source import _id, _numeric

    if 'imagePost' in data:
        images = data['imagePost']
        if not isinstance(images, dict):
            raise InvalidData('Invalid TikTok image post')
        return [tiktok_image(item) for item in _array(images.get('images'))]
    if 'video' in data:
        if not isinstance(data['video'], dict):
            raise InvalidData('Invalid TikTok video post')
        return [{'id': 'video:' + _numeric(_id(data.get('id'))), 'kind': 'video'}]
    return []


def membership(data, category):
    return (bluesky_membership if category == 'bluesky' else tiktok_membership)(data)


def manifest(data, category):
    from .source import post

    evidence = data.get(category + '_media')
    if evidence is None:
        return None
    if (not isinstance(evidence, dict) or set(evidence) != {'version', 'post', 'items'}
            or type(evidence['version']) is not int or evidence['version'] != 1
            or evidence['post'] != post(data) or not isinstance(evidence['items'], list)
            or evidence['items'] != membership(data, category)):
        raise InvalidData('Source attachment manifest differs from its original post')
    return evidence


def attachment(data, category, download_url):
    evidence = manifest(data, category)
    if evidence is None:
        return None
    if category == 'bluesky':
        selected = data.get('filename')
    elif data.get('type') == 'image':
        item = tiktok_image(data.get('image'))
        selected = tiktok_image_key(download_url)
        if item is None or selected != item['id']:
            raise InvalidData('Selected TikTok image differs from original media')
    elif data.get('type') == 'video' and 'imagePost' not in data:
        selected = 'video:' + evidence['post']['value']
    else:
        return None
    if any(item is not None and item['id'] == selected for item in evidence['items']):
        return {'namespace': evidence['post']['namespace'], 'value': selected}
    return None


def supported(extractor):
    from gallery_dl.extractor import bluesky, tiktok

    names = {
        'bluesky': ('User', 'Posts', 'Replies', 'Media', 'Video', 'Likes', 'Feed', 'List', 'Post', 'Search', 'Hashtag', 'Bookmark'),
        'tiktok': ('User', 'Posts', 'Reposts', 'Stories', 'Likes', 'Saved', 'Post', 'Vmpost'),
    }
    module = bluesky if extractor.category == 'bluesky' else tiktok
    prefix = 'Bluesky' if extractor.category == 'bluesky' else 'Tiktok'
    return isinstance(extractor, tuple(getattr(module, prefix + name + 'Extractor') for name in names.get(extractor.category, ())))


class StrictTiktokLog:
    def __init__(self, logger, error):
        self.logger, self.error_type = logger, error

    def __getattr__(self, name):
        return getattr(self.logger, name)

    def error(self, *args, **kwargs):
        # The pinned extractor catches ordinary exceptions inside items(). Its
        # final error log must not turn that failed post into source success.
        raise self.error_type('TikTok source extraction did not finish')


def install(extractor):
    from gallery_dl.extractor.common import Message
    from gallery_dl.extractor import bluesky, tiktok
    from .source import post

    if getattr(extractor, '_native_social_evidence', False):
        return
    if not supported(extractor):
        raise InvalidData('This source collection needs a separate native media adapter')
    category = extractor.category
    field = category + '_media'
    if isinstance(extractor, (bluesky.BlueskyUserExtractor, tiktok.TiktokUserExtractor)):
        allowed = ({'posts', 'replies', 'media', 'video', 'likes'} if category == 'bluesky'
                   else {'posts', 'reposts', 'stories', 'likes', 'saved'})
        include = extractor.config('include')
        if include is not None:
            requested = include.replace(' ', '').split(',') if isinstance(include, str) else include
            if not isinstance(requested, (list, tuple)) or any(not isinstance(item, str) or item not in allowed for item in requested):
                raise InvalidData('Native source profiles can include post collections only; artwork and following need separate adapters')
        elif category == 'tiktok':
            # The upstream default also downloads the profile avatar as media.
            # Native post ingestion defaults to posts; profile art is not a post.
            configuration = extractor.config
            extractor.config = lambda key, default=None: ('posts',) if key == 'include' else configuration(key, default)
    if category == 'bluesky':
        extract = extractor._extract_files

        def files(self, data):
            context = {**data, 'category': category}
            entries = membership(context, category)
            data[field] = {'version': 1, 'post': post(context), 'items': entries}
            selected = extract(data)
            if not self.videos:
                videos = {item['id'] for item in entries if item is not None and item['kind'] == 'video'}
                selected = [item for item in selected if item['filename'] not in videos]
                data['count'] = len(selected)
            return selected
        extractor._extract_files = types.MethodType(files, extractor)
    else:
        if extractor.config('audio', True) or extractor.config('covers', False) or extractor.config('subtitles', False):
            raise InvalidData('Native TikTok intake requires audio, covers and subtitles disabled')
        extractor.log = StrictTiktokLog(extractor.log, extractor.exc.ExtractionError)
        original = extractor.items

        def items(self):
            images, positions = None, []
            for kind, url, data in original():
                if kind == Message.Directory:
                    context = {**data, 'category': category}
                    entries = membership(context, category)
                    data[field] = {'version': 1, 'post': post(context), 'items': entries}
                    images = copy.deepcopy(data.get('imagePost'))
                    positions = [i for i, entry in enumerate(entries, 1) if entry is not None]
                    if images is not None:
                        # Upstream dereferences every photo directly. Skip unavailable
                        # entries for download while retaining their source positions.
                        selected = [dict(images['images'][i-1]) for i in positions]
                        for image in selected:
                            image.setdefault('imageWidth', 0)
                            image.setdefault('imageHeight', 0)
                        data['imagePost'] = {**images, 'images': selected}
                emitted = dict(data)
                if images is not None:
                    emitted['imagePost'] = images
                    if kind == Message.Url and data.get('type') == 'image':
                        emitted['num'] = positions[data['num']-1]
                yield kind, url, emitted
        extractor.items = types.MethodType(items, extractor)
    extractor._native_social_evidence = True


def collection_queue(extractor, url, data):
    from gallery_dl.extractor import bluesky, tiktok

    category = extractor.category
    if category == 'bluesky' and isinstance(extractor, bluesky.BlueskyUserExtractor):
        allowed = (bluesky.BlueskyPostsExtractor, bluesky.BlueskyRepliesExtractor, bluesky.BlueskyMediaExtractor,
                   bluesky.BlueskyVideoExtractor, bluesky.BlueskyLikesExtractor)
    elif category == 'tiktok' and isinstance(extractor, tiktok.TiktokUserExtractor):
        allowed = (tiktok.TiktokPostsExtractor, tiktok.TiktokRepostsExtractor, tiktok.TiktokStoriesExtractor,
                   tiktok.TiktokLikesExtractor, tiktok.TiktokSavedExtractor)
    elif category == 'tiktok' and isinstance(extractor, tiktok.TiktokVmpostExtractor):
        allowed = (tiktok.TiktokPostExtractor,)
    else:
        return False
    child = data.get('_extractor')
    return (set(data) <= {'_extractor', 'category', 'subcategory'} and data.get('category', category) == category
            and data.get('subcategory', extractor.subcategory) == extractor.subcategory
            and child in allowed and child.from_url(url) is not None)
