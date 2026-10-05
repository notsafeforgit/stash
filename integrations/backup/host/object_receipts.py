"""Reuse full-checksum upload evidence with a fresh, paginated S3 inventory.

LIST is a presence/change check, never a content checksum. Only a previous
successful full-checksum HEAD can create a receipt. Restore and explicit audit
still inspect the remote checksum directly. Losing this disposable local index
costs a new verification pass, not missing backup contents.
"""

import base64
from datetime import datetime, timezone
import os
from pathlib import Path
import sqlite3
import threading

from stash_archive.storage import HEX, InvalidArchive, regular, sync_directory

APPLICATION_ID = 0x534F5243


def identity(value, *, listed=False, storage_classes=("STANDARD",)):
    size = value.get("Size" if listed else "ContentLength")
    etag, modified = value.get("ETag"), value.get("LastModified")
    storage = value.get("StorageClass", "STANDARD")
    if (type(size) is not int or size < 0 or not isinstance(etag, str) or not 1 <= len(etag) <= 1024
            or any(ord(c) < 32 or ord(c) > 126 for c in etag)
            or not isinstance(modified, datetime) or modified.tzinfo is None
            or not isinstance(storage, str) or storage not in storage_classes):
        return None
    return size, etag, modified.astimezone(timezone.utc).isoformat(), storage


def inventory(client, bucket, prefix):
    """One complete listing; malformed/denied/truncated responses cannot pass."""
    result, tokens, token = {}, set(), None
    while True:
        args = {"Bucket": bucket, "Prefix": prefix, "MaxKeys": 1000}
        if token is not None:
            args["ContinuationToken"] = token
        page = client.list_objects_v2(**args)
        if not isinstance(page, dict) or type(page.get("IsTruncated")) is not bool:
            raise InvalidArchive("S3 inventory has no valid completion flag")
        contents = page.get("Contents", [])
        if not isinstance(contents, list) or len(contents) > 1000:
            raise InvalidArchive("Invalid S3 inventory page")
        for item in contents:
            key = item.get("Key") if isinstance(item, dict) else None
            if not isinstance(key, str) or not key.startswith(prefix) or key in result:
                raise InvalidArchive("S3 inventory contains duplicate or out-of-scope keys")
            result[key] = item
        if not page["IsTruncated"]:
            return result
        token = page.get("NextContinuationToken")
        if not isinstance(token, str) or not token or token in tokens or not contents:
            raise InvalidArchive("S3 inventory pagination is incomplete or repeated")
        tokens.add(token)


class ObjectReceipts:
    def __init__(self, filename, bucket):
        self.path, self.bucket = Path(filename), bucket
        parent = self.path.parent.lstat()
        if self.path.parent.is_symlink() or not self.path.parent.is_dir() or parent.st_uid != os.geteuid() or parent.st_mode & 0o077:
            raise InvalidArchive("Object receipts require a private owned directory")
        fd = os.open(self.path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        os.close(fd)
        for path in (self.path, *(Path(str(self.path) + suffix) for suffix in ("-wal", "-shm", "-journal"))):
            if path.exists() or path.is_symlink():
                info = regular(path)
                if info.st_uid != os.geteuid() or info.st_mode & 0o077:
                    raise InvalidArchive("Object receipt files must be private and owned")
        self.db = sqlite3.connect(self.path, check_same_thread=False)
        self.lock, self.pending = threading.Lock(), 0
        try:
            tables = self.db.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
            if not tables:
                if self.db.execute("PRAGMA application_id").fetchone()[0] not in (0, APPLICATION_ID):
                    raise InvalidArchive("Foreign object receipt database")
                self.db.executescript(f"""
                    PRAGMA application_id={APPLICATION_ID};
                    PRAGMA user_version=2;
                    CREATE TABLE receipts(bucket TEXT NOT NULL, key TEXT NOT NULL, sha256 TEXT NOT NULL,
                        size INTEGER NOT NULL, etag TEXT NOT NULL, modified TEXT NOT NULL, storage TEXT NOT NULL,
                        tag_state TEXT NOT NULL DEFAULT 'unknown',
                        PRIMARY KEY(bucket,key)) WITHOUT ROWID;
                """)
                sync_directory(self.path.parent)
            if (self.db.execute("PRAGMA application_id").fetchone()[0] != APPLICATION_ID
                    or self.db.execute("PRAGMA user_version").fetchone()[0] not in (1, 2)
                    or self.db.execute("PRAGMA quick_check").fetchall() != [("ok",)]):
                raise InvalidArchive("Invalid object receipt database")
            self.db.execute("PRAGMA journal_mode=WAL")
            self.db.execute("PRAGMA synchronous=FULL")
            if self.db.execute("PRAGMA user_version").fetchone()[0] == 1:
                with self.db:
                    self.db.execute("ALTER TABLE receipts ADD COLUMN tag_state TEXT NOT NULL DEFAULT 'unknown'")
                    self.db.execute("PRAGMA user_version=2")
        except BaseException:
            self.db.close()
            raise

    def matches(self, key, sha256, size, listed):
        current = identity(listed, listed=True) if listed is not None else None
        if current is None or current[0] != size:
            return False
        with self.lock:
            previous = self.db.execute("SELECT sha256,size,etag,modified,storage FROM receipts WHERE bucket=? AND key=?",
                                       (self.bucket, key)).fetchone()
        return previous == (sha256, *current)

    def tags_match(self, key, sha256, size, listed, state):
        if not self.matches(key, sha256, size, listed):
            return False
        with self.lock:
            row = self.db.execute("SELECT tag_state FROM receipts WHERE bucket=? AND key=?", (self.bucket, key)).fetchone()
        return row == (state,)

    def invalidate_tags(self, key):
        # S3 tag writes do not change the LIST identity. Commit this BEFORE
        # sending a retirement request, including requests whose reply is lost.
        with self.lock:
            self.db.execute("UPDATE receipts SET tag_state='unknown' WHERE bucket=? AND key=?", (self.bucket, key))
            self.db.commit()
            self.pending = 0

    def remember(self, key, sha256, head, *, tag_state=None):
        if tag_state not in (None, "live", "retired"):
            raise InvalidArchive("Invalid object tag receipt state")
        if (not isinstance(sha256, str) or not HEX.fullmatch(sha256)
                or head.get("ChecksumType") != "FULL_OBJECT"
                or head.get("ChecksumSHA256") != base64.b64encode(bytes.fromhex(sha256)).decode("ascii")
                or head.get("StorageClass", "STANDARD") != "STANDARD"):
            raise InvalidArchive("Only a verified full-object SHA-256 can create an upload receipt")
        current = identity(head)
        if current is None:
            return  # Full checksum was checked, but no reusable LIST identity.
        with self.lock:
            old = self.db.execute("SELECT sha256,size,etag,modified,storage,tag_state FROM receipts WHERE bucket=? AND key=?",
                                  (self.bucket, key)).fetchone()
            state = tag_state or (old[-1] if old is not None and old[:-1] == (sha256, *current) else "unknown")
            self.db.execute("INSERT OR REPLACE INTO receipts VALUES (?,?,?,?,?,?,?,?)", (self.bucket, key, sha256, *current, state))
            self.pending += 1
            if self.pending >= 256:
                self.db.commit()
                self.pending = 0

    def close(self):
        with self.lock:
            try:
                self.db.commit()
            finally:
                self.db.close()
