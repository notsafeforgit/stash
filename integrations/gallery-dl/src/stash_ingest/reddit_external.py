"""Match linked Reddit media without deriving identities from output filenames.

A Redgifs watch URL identifies a clip, but does not prove a complete gallery.
Direct media URLs retain their full path/query identity. Reddit's own retained
preview references can identify a fallback encoding of that same linked item.
"""

import html
import re
from urllib.parse import urlsplit

from .encoding import InvalidData
from .web_media import reference

IMAGE = {'.jpg', '.jpeg', '.png', '.webp', '.avif', '.jxl', '.bmp'}
VIDEO = {'.mp4', '.webm', '.mov', '.m4v', '.mkv'}
IMGUR_ITEM = re.compile(r'/([A-Za-z0-9]{5,12})/?')
IMGUR_PAGES = {'gallery', 'search', 'upload', 'signin', 'register', 'account', 'settings'}


def linked(value):
    if not isinstance(value, str):
        return None
    value = html.unescape(value)
    try:
        identity = reference(value)
    except InvalidData:
        return None
    parsed = urlsplit(value)
    if parsed.hostname in ('imgur.com', 'www.imgur.com', 'm.imgur.com'):
        match = IMGUR_ITEM.fullmatch(parsed.path)
        if match and match[1].lower() not in IMGUR_PAGES:
            return {'namespace': 'native:imgur', 'value': match[1]}, 'unknown'
        return None
    if parsed.hostname in ('redgifs.com', 'www.redgifs.com', 'm.redgifs.com', 'v3.redgifs.com'):
        match = re.fullmatch(r'/(?:watch|ifr)/([A-Za-z0-9]{1,256})/?', parsed.path)
        if match:
            return {'namespace': 'native:redgifs', 'value': match[1].lower()}, 'unknown'
        return None
    # Native Reddit URLs have their own stable media-ID resolver.
    if parsed.hostname in ('i.redd.it', 'preview.redd.it', 'v.redd.it'):
        return None
    extension = '.' + parsed.path.rsplit('.', 1)[-1].lower()
    kind = 'image' if extension in IMAGE else 'video' if extension in VIDEO else 'unknown'
    if kind == 'unknown' and extension != '.gif':
        return None
    return {'namespace': 'native:reddit', 'value': identity}, kind


def target(data):
    values = [linked(data.get(key)) for key in ('url', 'url_overridden_by_dest')]
    values = [value for value in values if value is not None]
    if not values:
        return None
    if any(value[0] != values[0][0] for value in values[1:]):
        raise InvalidData('Reddit post media identifiers disagree')
    return values[0][0]


def same_url(left, right):
    try:
        return reference(html.unescape(left)) == reference(html.unescape(right))
    except (InvalidData, TypeError):
        return False


def redgifs_image_id(value):
    """The image permalink identifies the API item behind its CDN renditions."""
    if linked(value) is None:
        return None
    try:
        parsed = urlsplit(html.unescape(value))
    except ValueError:
        return None
    if parsed.hostname != 'i.redgifs.com':
        return None
    match = re.fullmatch(r'/i/([A-Za-z0-9]{1,256})\.(?:jpg|jpeg|png|webp|avif|jxl|bmp)', parsed.path, re.IGNORECASE)
    return match[1].lower() if match else None


def preview_matches(data, download):
    from .source import reddit_media
    preview = data.get('preview')
    if not isinstance(preview, dict):
        return False
    video = preview.get('reddit_video_preview')
    selected = reddit_media(download)
    if selected and isinstance(video, dict):
        known = {reddit_media(video.get(key)) for key in ('fallback_url', 'dash_url', 'hls_url')}
        known.discard(None)
        if len(known) > 1:
            raise InvalidData('Reddit preview media identifiers disagree')
        if known == {selected}:
            return True
    images = preview.get('images')
    if not isinstance(images, list) or len(images) != 1 or not isinstance(images[0], dict):
        return False
    image = images[0]
    candidates = [image]
    variants = image.get('variants')
    if isinstance(variants, dict):
        candidates.extend(v for v in variants.values() if isinstance(v, dict))
    for item in candidates:
        source = item.get('source')
        if isinstance(source, dict) and same_url(source.get('url'), download):
            return True
        for resolution in item.get('resolutions') or ():
            if isinstance(resolution, dict) and same_url(resolution.get('url'), download):
                return True
    return False


def attachment(data, source, download):
    ref = target(data)
    if ref is None or data.get('comment'):
        return None
    if ref['namespace'] == 'native:imgur' and source.get('category') == 'imgur':
        # A captured single-item API response ties the permalink to its CDN
        # rendition. Never substitute another album item or guess from a name.
        if (source.get('id') == ref['value'] and source.get('is_album') is False
                and source.get('image_count') == 1 and same_url(source.get('url'), download)):
            return ref
    # Keep the direct image URL's existing attachment identity, including its
    # query. Only the exact Redgifs API item and one of its observed renditions
    # may resolve that permalink; output filenames are not source evidence.
    image_id = next((identity for key in ('url', 'url_overridden_by_dest')
                     if (identity := redgifs_image_id(data.get(key))) is not None), None)
    redgifs_id = ref['value'] if ref['namespace'] == 'native:redgifs' else image_id
    if redgifs_id is not None:
        if source.get('category') == 'redgifs' and redgifs_item_matches(source, redgifs_id):
            urls = source.get('urls')
            if isinstance(urls, dict):
                for key in ('hd', 'sd', 'gif', 'silent'):
                    value = urls.get(key)
                    if isinstance(value, str) and same_url(value.replace('//thumbs2.', '//thumbs3.', 1), download):
                        return ref
    if ref['namespace'] != 'native:redgifs' and any(same_url(data.get(key), download) for key in ('url', 'url_overridden_by_dest')):
        return ref
    if preview_matches(data, download):
        return ref
    return None


def redgifs_item_matches(source, identity):
    item_id = str(source.get('id', '')).lower()
    if item_id == identity:
        return True
    # Numeric watch links resolve to named API items. The captured request and
    # API permalink establish that alias for a single-item response; a gallery
    # expansion must not attribute its other items to the original watch link.
    if (re.fullmatch(r'[0-9]{1,30}', identity) is None or source.get('gallery')
            or source.get('count') != 1):
        return False
    request = linked(source.get('source_extractor_url'))
    urls = source.get('urls')
    resolved = linked(urls.get('html')) if isinstance(urls, dict) else None
    return (request is not None and request[0] == {'namespace': 'native:redgifs', 'value': identity}
            and resolved is not None and resolved[0] == {'namespace': 'native:redgifs', 'value': item_id})
