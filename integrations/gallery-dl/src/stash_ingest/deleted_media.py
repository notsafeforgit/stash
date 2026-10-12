"""Confirmed external-media deletions, distinct from a successful download.

Only a single Redgifs item's explicit deletion response qualifies. A missing
profile, ordinary 404, authentication failure, timeout or CDN error does not.
The local cache avoids repeat API calls across worker restarts; a monthly check
allows the provider to restore an item without an indefinite local tombstone.
"""

from urllib.parse import urlsplit

from . import reddit_external
from .encoding import utc_now
from .retention import retain

RECHECK_SECONDS = 30 * 24 * 60 * 60
DELETED_MESSAGE = 'The requested gif was deleted and cannot be accessed.'


class DeletedMedia(Exception):
    def __init__(self, status):
        self.status = status
        super().__init__('Redgifs explicitly reports that this media was deleted')


def migrate(db):
    db.execute('''CREATE TABLE IF NOT EXISTS deleted_media (
        service TEXT NOT NULL CHECK(service='redgifs'), item TEXT NOT NULL,
        observed_at TEXT NOT NULL, http_status INTEGER NOT NULL CHECK(http_status IN (404,410)),
        recheck_at REAL NOT NULL, PRIMARY KEY(service,item))''')


def target(category, url, metadata):
    if category != 'reddit':
        return None
    data = retain(dict(metadata, category='reddit'))
    if data.get('comment'):
        return None
    media = data
    parents = media.get('crosspost_parent_list')
    if parents:
        if not isinstance(parents, list) or not isinstance(parents[-1], dict):
            return None
        media = parents[-1]
    ref = reddit_external.target(media)
    requested = reddit_external.linked(url)
    if ref is None or requested is None or ref != requested[0] or media.get('comment'):
        return None
    key = ref['value'] if ref['namespace'] == 'native:redgifs' else reddit_external.redgifs_image_id(url)
    return (key, ref, data) if key else None


def response(error, request_url, item):
    reply = error.response
    parsed = urlsplit(request_url)
    if (parsed.scheme != 'https' or parsed.netloc != 'api.redgifs.com'
            or parsed.path != '/v2/gifs/' + item or parsed.query
            or getattr(reply, 'status_code', None) not in (404, 410)):
        return None
    try:
        data = reply.json()
    except ValueError:
        return None
    if not isinstance(data, dict):
        return None
    # The live API returns {"error": {"code": ..., "description": ...}}.
    # Some older responses use top-level fields. Inspect only those known
    # error locations, rather than accepting a matching string anywhere in JSON.
    errors = (data, data.get('error'))
    if not any(isinstance(error, dict) and any(error.get(key) == DELETED_MESSAGE
               for key in ('message', 'description')) for error in errors):
        return None
    return {'state': 'deleted', 'service': 'redgifs', 'item': item,
            'observed_at': utc_now(), 'http_status': reply.status_code}


def cached(box, item):
    row = box.db.execute('SELECT observed_at,http_status FROM deleted_media WHERE service=? AND item=? AND recheck_at>?',
                         ('redgifs', item, box.clock())).fetchone()
    if row is not None:
        return {'state': 'deleted', 'service': 'redgifs', 'item': item,
                'observed_at': row['observed_at'], 'http_status': row['http_status']}
    return None


def remember(box, status):
    with box.transaction():
        box.db.execute('''INSERT INTO deleted_media(service,item,observed_at,http_status,recheck_at) VALUES(?,?,?,?,?)
            ON CONFLICT(service,item) DO UPDATE SET observed_at=excluded.observed_at,
            http_status=excluded.http_status,recheck_at=excluded.recheck_at''',
                       ('redgifs', status['item'], status['observed_at'], status['http_status'], box.clock() + RECHECK_SECONDS))
