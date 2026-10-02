"""Read a frozen legacy automation database without running its schema or jobs."""

import hashlib
import os
from pathlib import Path
import sqlite3
import stat

from .catalog_source import APPLICATION_ID, quoted
from .encoding import InvalidData


READER_VERSION = "automation-sqlite-v1"
TABLES = {
    "maintenance": "key value".split(),
    "translation_jobs": "job_key original_text target_language priority source_hint status result_json attempts next_attempt last_error created_at updated_at".split(),
    "translation_targets": "job_key catalog_id post_key field applied".split(),
    "enrichment_jobs": "catalog_id post_key version platform account_key url status priority attempts next_attempt last_error staged_json created_at updated_at".split(),
    "enrichment_cooldowns": "scope until_time reason".split(),
    "enrichment_seed_progress": "catalog_id last_post_key complete counts_json".split(),
    "enrichment_source_progress": "platform last_attempt".split(),
    "discovery_accounts": "job_key platform account_key profile_url status cursor_json staged_json pages attempts next_attempt last_error created_at updated_at".split(),
    "discovery_targets": "catalog_id post_key job_key evidence_json status".split(),
    "discovery_candidates": "catalog_id post_key url basis payload_json".split(),
}
KEYS = {
    "maintenance": ["key"], "translation_jobs": ["job_key"],
    "translation_targets": "job_key catalog_id post_key field".split(),
    "enrichment_jobs": "catalog_id post_key version".split(),
    "enrichment_cooldowns": ["scope"], "enrichment_seed_progress": ["catalog_id"],
    "enrichment_source_progress": ["platform"], "discovery_accounts": ["job_key"],
    "discovery_targets": ["catalog_id", "post_key"],
    "discovery_candidates": ["catalog_id", "post_key", "url"],
}


def open_regular(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise InvalidData("Snapshot input must be a regular file")
        return os.fdopen(fd, "rb")
    except BaseException:
        os.close(fd)
        raise


def file_identity(value):
    return value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns


class AutomationSource:
    """A frozen single-file input, fenced against replacement or concurrent writes."""

    def __init__(self, path):
        self.path = Path(path).absolute()
        self.file = self.db = None
        self.tables = {}

    def __enter__(self):
        try:
            self.file = open_regular(self.path)
            self.identity = file_identity(os.fstat(self.file.fileno()))
            self.assert_unchanged()
            # immutable avoids creating SHM/WAL files beside the reviewed copy.
            # Nonempty journals and any changed file identity are rejected.
            self.db = sqlite3.connect(self.path.as_uri() + "?mode=ro&immutable=1", uri=True, timeout=5)
            self.db.row_factory = sqlite3.Row
            self.db.execute("PRAGMA query_only=ON")
            self.db.execute("PRAGMA trusted_schema=OFF")
            self.db.execute("BEGIN")
            self._schema()
            self.source_sha256 = hashlib.file_digest(self.file, "sha256").hexdigest()
            self.assert_unchanged()
            return self
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def __exit__(self, *_):
        if self.db is not None:
            self.db.close()
            self.db = None
        if self.file is not None:
            self.file.close()
            self.file = None

    def assert_unchanged(self):
        current = self.path.stat(follow_symlinks=False)
        if (not stat.S_ISREG(current.st_mode) or file_identity(current) != self.identity
                or file_identity(os.fstat(self.file.fileno())) != self.identity):
            raise InvalidData("Frozen automation input changed during preparation")
        for suffix in ("-wal", "-journal"):
            journal = Path(str(self.path) + suffix)
            try:
                state = journal.stat(follow_symlinks=False)
            except FileNotFoundError:
                continue
            if not stat.S_ISREG(state.st_mode) or state.st_size:
                raise InvalidData("Automation input has an active journal; use a frozen SQLite backup")

    def _schema(self):
        self.application_id = self.db.execute("PRAGMA application_id").fetchone()[0]
        self.version = self.db.execute("PRAGMA user_version").fetchone()[0]
        if self.application_id != APPLICATION_ID or self.version not in (1, 2, 3):
            raise InvalidData("Unsupported automation database lineage/version")
        self.schema = [dict(row) for row in self.db.execute(
            "SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY type,name")]
        for item in self.schema:
            name = item["name"]
            if (item["type"] not in ("table", "index", "trigger") or item["tbl_name"] not in TABLES
                    or not isinstance(item["sql"], str) or not 0 < len(item["sql"].encode()) <= 1 << 20):
                raise InvalidData("Unknown automation schema object")
            if item["type"] != "table":
                continue
            columns = [dict(row) for row in self.db.execute("PRAGMA table_xinfo(" + quoted(name) + ")")]
            if (name != item["tbl_name"] or {c["name"] for c in columns} != set(TABLES[name])
                    or len(columns) != len(TABLES[name]) or any(c["hidden"] for c in columns)
                    or [c["name"] for c in sorted(columns, key=lambda c: c["pk"]) if c["pk"]] != KEYS[name]):
                raise InvalidData("Unsupported automation table columns or primary key")
            self.tables[name] = columns
        if set(self.tables) != set(TABLES):
            raise InvalidData("Automation input is missing a required record family")

    def check_integrity(self):
        if [row[0] for row in self.db.execute("PRAGMA quick_check")] != ["ok"]:
            raise InvalidData("Automation SQLite integrity check failed")
        # Broken legacy references are retained for explicit domain review.
        return {"quick_check": "ok", "foreign_key_violations": sum(1 for _ in self.db.execute("PRAGMA foreign_key_check"))}

    def rows(self, table):
        if table not in self.tables:
            raise InvalidData("Unknown automation record family")
        order = ",".join(quoted(key) + " COLLATE BINARY" for key in KEYS[table])
        for row in self.db.execute("SELECT * FROM " + quoted(table) + " ORDER BY " + order):
            yield dict(row)
