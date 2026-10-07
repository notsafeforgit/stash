"""Durable cold-object proofs reused against one complete fresh S3 inventory.

The existing backup ledger owns this index and its source-path associations.
LIST identity can reuse previous checksum evidence, but cannot establish it.
New/changed objects require checksum evidence. Historical ETags additionally
require a complete local byte comparison before adoption. Nothing uploads or thaws.
"""

from contextlib import contextmanager
import json
import sqlite3
import threading

import media_objects as media
from object_receipts import identity, inventory


def initialize(conn):
    conn.execute('CREATE TABLE IF NOT EXISTS media_store(singleton INTEGER PRIMARY KEY CHECK(singleton=1), bucket TEXT NOT NULL, prefix TEXT NOT NULL)')
    conn.execute('CREATE TABLE IF NOT EXISTS media_objects(key TEXT PRIMARY KEY, sha256 TEXT, descriptor TEXT NOT NULL, identity TEXT)')
    conn.execute('CREATE INDEX IF NOT EXISTS media_objects_sha256 ON media_objects(sha256)')
    conn.execute('CREATE TABLE IF NOT EXISTS video_objects(path TEXT PRIMARY KEY, key TEXT NOT NULL)')
    conn.execute('CREATE INDEX IF NOT EXISTS video_objects_key ON video_objects(key)')
    # Includes intentions whose uploads or publications did not finish. Keep a
    # path until cleanup succeeds so a returning file protects its pending key.
    conn.execute('CREATE TABLE IF NOT EXISTS media_paths(key TEXT NOT NULL, path TEXT NOT NULL, PRIMARY KEY(key,path))')
    conn.execute('CREATE INDEX IF NOT EXISTS media_paths_path ON media_paths(path)')


class MediaIndex:
    def __init__(self, filename, client, store):
        self.store = dict(media.validate_store(store))
        self.client = client
        self.lock = threading.RLock()
        self.content_locks = {}
        self.reactivated = set()
        self.db = sqlite3.connect(filename, timeout=30, check_same_thread=False)
        try:
            self.db.execute('PRAGMA journal_mode=WAL')
            self.db.execute('PRAGMA synchronous=FULL')
            with self.db:
                initialize(self.db)
                binding = self.db.execute('SELECT bucket,prefix FROM media_store WHERE singleton=1').fetchone()
                expected = (self.store['bucket'], self.store['prefix'])
                if binding is not None and binding != expected:
                    raise ValueError('Cold media ledger belongs to a different bucket or prefix')
                self.db.execute('INSERT OR IGNORE INTO media_store VALUES (1,?,?)', expected)
            values = inventory(client, self.store['bucket'], self.store['prefix'])
            self.listed = {key[len(self.store['prefix']):]: value for key, value in values.items()}
        except BaseException:
            self.db.close()
            raise

    def close(self):
        self.db.close()

    @contextmanager
    def content_lock(self, sha256):
        with self.lock:
            lock = self.content_locks.setdefault(sha256, threading.Lock())
        with lock:
            yield

    def head(self, key):
        try:
            return self.client.head_object(**media.request(self.store, key))
        except Exception as error:
            code = str(getattr(error, 'response', {}).get('Error', {}).get('Code'))
            if code in {'404', 'NoSuchKey', 'NotFound'}:
                return None
            raise

    def previous(self, key):
        with self.lock:
            row = self.db.execute('SELECT descriptor,identity FROM media_objects WHERE key=?', (key,)).fetchone()
        if row is None:
            return None, None
        return media.validate_object(json.loads(row[0])), json.loads(row[1]) if row[1] else None

    def remember(self, key, record, head):
        media.verify_head(record, head)
        current = identity(head, storage_classes=media.COLD)
        with self.lock, self.db:
            self.db.execute('INSERT OR REPLACE INTO media_objects VALUES (?,?,?,?)',
                            (key, record.get('sha256'), json.dumps(record), json.dumps(current)))
            # A verified new upload is part of this run's inventory as well.
            self.listed[key] = dict(head, Size=head['ContentLength'], Key=self.store['prefix'] + key)

    def get(self, key, *, local=None, allow_legacy_etag=False):
        media.relative_path(key)
        previous, receipt = self.previous(key)
        with self.lock:
            listed = self.listed.get(key)
        if listed is None:
            return None
        current = identity(listed, listed=True, storage_classes=media.COLD)
        head = None
        if previous is not None and current is not None and list(current) == receipt:
            record = previous
        else:
            head = self.head(key)
            if head is None:
                with self.lock:
                    self.listed.pop(key, None)
                return None
            if previous is not None:
                media.verify_head(previous, head)  # Never adopt overwritten known bytes.
                record = previous
            elif allow_legacy_etag and not key.startswith(media.PREFIX):
                record = media.existing_object_from_head(head, local=local)
            else:
                record = media.object_from_head(head)
        if local is not None and media.matches_local(record, local) and 'sha256' not in record:
            record = dict(record, sha256=local.sha256.hexdigest())
            # The earlier receipt already proves the checksum; only the local
            # SHA is new, so this does not need another cloud request.
            if head is None:
                with self.lock, self.db:
                    self.db.execute('UPDATE media_objects SET sha256=?,descriptor=? WHERE key=?',
                                    (record['sha256'], json.dumps(record), key))
        if head is not None:
            self.remember(key, record, head)
        return record

    def same_content_keys(self, sha256):
        with self.lock:
            return [row[0] for row in self.db.execute('SELECT key FROM media_objects WHERE sha256=? ORDER BY key', (sha256,))]

    def verify_upload(self, key, local):
        head = self.head(key)
        if head is None:
            raise ValueError('Uploaded cold object is unavailable for checksum verification')
        previous, _ = self.previous(key)
        if previous is not None:
            media.verify_head(previous, head)
        record = media.object_from_head(head, local=local)
        if record['storage_class'] != 'DEEP_ARCHIVE':
            raise ValueError('New media uploads must use Deep Archive')
        self.remember(key, record, head)
        return record

    def objects(self, keys):
        result = {}
        for key in sorted(set(keys)):
            record = self.get(key)
            if record is None:
                raise ValueError(f'Required cold media object is missing: {key!r}')
            result[key] = record
        return result

    def verify_catalog(self, catalog):
        media.validate_inventory(catalog)
        if catalog['media_store'] != self.store:
            raise ValueError('Retained catalog uses a different cold store')
        for key, current in self.objects(catalog['objects']).items():
            expected = catalog['objects'][key]
            if (any(current[field] != expected[field] for field in ('size', 'storage_class', 'checksum'))
                    or ('sha256' in current and 'sha256' in expected and current['sha256'] != expected['sha256'])):
                raise ValueError('Retained catalog media identity has changed')
