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


def linked(value):
    if not isinstance(value, str):
        return None
    value = html.unescape(value)
    try:
        identity = reference(value)
    except InvalidData:
        return None
    parsed = urlsplit(value)
    if parsed.hostname in ('redgifs.com', 'www.redgifs.com', 'm.redgifs.com'):
        match = re.fullmatch(r'/(?:watch|ifr)/([A-Za-z]{1,256})/?', parsed.path)
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
    if ref['namespace'] == 'native:redgifs':
        if source.get('category') == 'redgifs' and str(source.get('id', '')).lower() == ref['value']:
            urls = source.get('urls')
            if isinstance(urls, dict):
                for key in ('hd', 'sd', 'gif', 'silent'):
                    value = urls.get(key)
                    if isinstance(value, str) and same_url(value.replace('//thumbs2.', '//thumbs3.', 1), download):
                        return ref
    elif any(same_url(data.get(key), download) for key in ('url', 'url_overridden_by_dest')):
        return ref
    if preview_matches(data, download):
        return ref
    return None
