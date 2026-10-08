"""Identity of resolved yt-dlp leaves, independent of collection/playlist labels."""

from datetime import datetime, timedelta, timezone
import hashlib
import math
import re
from urllib.parse import urlsplit, urlunsplit

from .encoding import InvalidData

_SITE = re.compile(r'[a-z0-9][a-z0-9_.-]*\Z')
EPOCH = datetime(1970, 1, 1, tzinfo=timezone.utc)


def post(data):
    if 'entries' in data:
        raise InvalidData('A yt-dlp collection is not a resolved video')
    site, key = data.get('extractor_key'), data.get('id')
    if isinstance(key, int) and not isinstance(key, bool):
        key = str(key)
    if (not isinstance(key, str) or not key or key != key.strip()
            or len(key.encode('utf-8')) > 1024
            or any(ord(c) < 32 or 127 <= ord(c) < 160 for c in key)):
        raise InvalidData('yt-dlp leaf has no usable video ID')
    if not isinstance(site, str) or not site.isascii() or not _SITE.fullmatch(site.lower()):
        raise InvalidData('yt-dlp leaf has no usable extractor key')
    site = site.lower()
    if site == 'generic':
        page = data.get('webpage_url')
        if (not isinstance(page, str) or len(page) > 8192 or not page.isascii()
                or any(ord(c) <= 32 or ord(c) >= 127 or c == '\\' for c in page)
                or re.search(r'%(?![0-9A-Fa-f]{2})', page)):
            raise InvalidData('Generic yt-dlp identity requires its source webpage URL')
        try:
            url = urlsplit(page)
            site = url.hostname
            if (url.scheme not in ('https', 'http') or url.username is not None
                    or url.password is not None or url.port is not None
                    or not site or not _SITE.fullmatch(site) or url.netloc.lower() != site):
                raise ValueError()
            # Keep meaningful query/path differences. A CDN URL, basename or
            # playlist ID cannot establish the identity of a generic video.
            page = urlunsplit((url.scheme, site, url.path or '/', url.query, ''))
        except ValueError:
            raise InvalidData('Generic yt-dlp identity requires its source webpage URL') from None
        key = 'page:' + hashlib.sha256((page + '\0' + key).encode()).hexdigest()
    if len(site) > 123:
        raise InvalidData('yt-dlp extractor namespace exceeds limit')
    return {'namespace': 'ytdl:' + site, 'value': key}


def attachment(data):
    marker = data.get('ytdl_media')
    if (not isinstance(marker, dict) or set(marker) != {'version', 'type'}
            or type(marker['version']) is not int or marker['version'] != 1 or marker['type'] != 'video'):
        raise InvalidData('yt-dlp attachment requires a resolved video leaf')
    return post(data)


def mark_leaf(data):
    if data.get('_type', 'video') != 'video' or 'entries' in data:
        raise InvalidData('yt-dlp did not resolve a downloadable video leaf')
    post(data)
    data['ytdl_media'] = {'version': 1, 'type': 'video'}


def published(data):
    value = data.get('timestamp')
    if value is None:
        return None
    if (not isinstance(value, (int, float)) or isinstance(value, bool) or not -62135596800 <= value < 253402300800
            or not math.isfinite(value)):
        raise InvalidData('Invalid yt-dlp publication timestamp')
    try:
        return EPOCH + timedelta(milliseconds=math.floor(value * 1000))
    except (OverflowError, ValueError):
        raise InvalidData('Invalid yt-dlp publication timestamp') from None


def publication(data):
    value = published(data)
    if value is not None:
        return value.isoformat(timespec='milliseconds').replace('+00:00', 'Z')
    value = data.get('upload_date')
    if value is None:
        return None
    if not isinstance(value, str) or not re.fullmatch(r'[0-9]{8}', value):
        raise InvalidData('Invalid yt-dlp upload date')
    try:
        return datetime.strptime(value, '%Y%m%d').date().isoformat()
    except ValueError:
        raise InvalidData('Invalid yt-dlp upload date') from None
