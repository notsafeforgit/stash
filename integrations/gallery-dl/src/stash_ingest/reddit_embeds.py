"""Reddit inline media, ordered by references in the captured post body."""

import re

from .encoding import InvalidData

REFERENCES = re.compile(r'!\[(?:img|video)\]\(([A-Za-z0-9]+)\)|https?://(?:i|preview|v)\.redd\.it/[^\s<>()[\]\\]+')


def entries(data):
    from .source import reddit_media
    metadata = data.get('media_metadata')
    if not isinstance(metadata, dict) or not metadata or data.get('comment'):
        return None
    if len(metadata) > 4096:
        raise InvalidData('Too many Reddit embedded attachments')
    for key, item in metadata.items():
        if (not re.fullmatch(r'[A-Za-z0-9]+', key) or not isinstance(item, dict)
                or item.get('id', key) != key):
            raise InvalidData('Reddit embedded media identifiers disagree')
        rendition = item.get('s') or {}
        if not isinstance(rendition, dict):
            raise InvalidData('Invalid Reddit embedded media rendition')
        for value in [rendition.get(k) for k in ('u', 'gif', 'mp4')] + [item.get('dashUrl'), item.get('hlsUrl')]:
            selected = reddit_media(value)
            if selected is not None and selected != key:
                raise InvalidData('Reddit embedded media identifiers disagree')
    ordered = []
    for match in REFERENCES.finditer(data.get('selftext') or ''):
        key = match[1] or reddit_media(match[0])
        if key in metadata and key not in ordered:
            ordered.append(key)
    if len(metadata) == 1:
        return list(metadata)
    # Object iteration order and the downloader's num are not post ordering.
    return ordered if len(ordered) == len(metadata) else None


def attachment(data, download):
    from .source import reddit_media
    ordered = entries(data)
    selected = reddit_media(download)
    if not ordered or selected not in ordered:
        return None
    item = data['media_metadata'][selected]
    if item.get('status') != 'valid':
        return None
    rendition = item.get('s') or {}
    values = [rendition.get(k) for k in ('u', 'gif', 'mp4')] + [item.get('dashUrl'), item.get('hlsUrl')]
    if any(reddit_media(value) == selected for value in values):
        return {'namespace': 'native:reddit', 'value': selected}
    return None
