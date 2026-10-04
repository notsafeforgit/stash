"""Durable, bounded delivery state, separate from the downloader's archive.

No HTTP or media I/O takes place in a transaction. Concurrent drainers use short
fenced leases; interrupted delivery replays the same event bytes. A validated
server receipt and payload removal are committed in the same FULL transaction.
"""

from contextlib import contextmanager
from dataclasses import dataclass
import os
from pathlib import Path
import sqlite3
import time

from . import events
from .encoding import InvalidData, MAX_BATCH_BYTES, decode, digest, encode, identifier
from .endpoint import origin

APPLICATION_ID = 0x5354494F  # STIO, not a Stash or gallery-dl archive database.
SCHEMA = 12


class Conflict(InvalidData):
    pass


class Capacity(RuntimeError):
    pass


class LeaseLost(RuntimeError):
    pass


@dataclass(frozen=True)
class Delivery:
    event_uuid: str
    sha256: str
    body: bytes
    owner: str
    fence: int
    attempts: int


class Outbox:
    def __init__(self, path, endpoint, producer, *, max_events=10000,
                 max_bytes=512 << 20, clock=time.time):
        self.endpoint = origin(endpoint)
        self.producer = identifier(producer)
        if type(max_events) is not int or max_events < 1 or type(max_bytes) is not int or max_bytes < 1:
            raise InvalidData("Outbox capacity must be positive")
        self.max_events, self.max_bytes, self.clock = max_events, max_bytes, clock
        path = Path(path).absolute()
        self.path = path
        # The directory is provisioned by the runner. Do not silently create a
        # replacement on an absent mount or follow a substituted database link.
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        os.close(fd)
        self.db = sqlite3.connect(path, timeout=15, isolation_level=None)
        self.db.row_factory = sqlite3.Row
        try:
            # Another first opener can publish the schema between these reads.
            # Inspect one read snapshot; mixing the empty version with the new
            # application/tables would incorrectly reject a valid new outbox.
            self.db.execute("BEGIN")
            try:
                version = self.db.execute("PRAGMA user_version").fetchone()[0]
                application = self.db.execute("PRAGMA application_id").fetchone()[0]
                tables = self.db.execute("SELECT name FROM sqlite_schema WHERE type='table'").fetchall()
            finally:
                self.db.execute("ROLLBACK")
            if not ((version in (1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, SCHEMA) and application == APPLICATION_ID)
                    or (version == 0 and application == 0 and not tables)):
                raise InvalidData("Unsupported or foreign outbox database")
            self.db.execute("PRAGMA foreign_keys=ON")
            self._enable_wal()
            self.db.execute("PRAGMA synchronous=FULL")
            with self.transaction():
                # Recheck under the write lock: another runner may have opened
                # and initialized the same empty queue concurrently.
                if self.db.execute("PRAGMA user_version").fetchone()[0] == 0:
                    self._initialize()
                self._migrate()
                binding = self.db.execute("SELECT endpoint, producer FROM binding WHERE id=1").fetchone()
                if tuple(binding) != (self.endpoint, self.producer):
                    raise Conflict("Outbox belongs to a different Stash origin or producer")
            directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
        except BaseException:
            self.db.close()
            raise

    def _enable_wal(self):
        # Concurrent first opens can return SQLITE_BUSY immediately while the
        # persistent journal mode changes, despite the connection busy timeout.
        # Wait for the same database; never replace or recreate its queue.
        deadline = time.monotonic() + 15
        while True:
            try:
                mode = self.db.execute("PRAGMA journal_mode=WAL").fetchone()[0]
                if mode != "wal":
                    raise InvalidData("Outbox requires a local filesystem with SQLite WAL support")
                return
            except sqlite3.OperationalError as error:
                if ((getattr(error, "sqlite_errorcode", 0) & 255) not in (sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED)
                        or time.monotonic() >= deadline):
                    raise
                time.sleep(0.025)

    def _initialize(self):
        self.db.execute("CREATE TABLE binding(id INTEGER PRIMARY KEY CHECK(id=1), endpoint TEXT NOT NULL, producer TEXT NOT NULL)")
        self.db.execute("INSERT INTO binding VALUES(1,?,?)", (self.endpoint, self.producer))
        self.db.execute("""CREATE TABLE events(
            seq INTEGER PRIMARY KEY, event_uuid TEXT NOT NULL UNIQUE,
            sha256 TEXT NOT NULL, kind TEXT NOT NULL, collection_uuid TEXT NOT NULL,
            collection_revision INTEGER NOT NULL, root_uuid TEXT, run_uuid TEXT NOT NULL,
            parent_uuid TEXT REFERENCES events(event_uuid), body BLOB, receipt BLOB,
            state TEXT NOT NULL CHECK(state IN ('pending','sending','acknowledged','review')),
            owner TEXT, fence INTEGER NOT NULL DEFAULT 0, lease_until REAL,
            attempts INTEGER NOT NULL DEFAULT 0, available_at REAL NOT NULL,
            created_at REAL NOT NULL, acknowledged_at REAL, error_code TEXT,
            CHECK((state='sending')=(owner IS NOT NULL AND lease_until IS NOT NULL)),
            CHECK((state='acknowledged')=(receipt IS NOT NULL AND body IS NULL)),
            CHECK(state='acknowledged' OR (receipt IS NULL AND body IS NOT NULL))
        )""")
        self.db.execute("CREATE INDEX ready_events ON events(state,available_at,seq)")
        self.db.execute("CREATE INDEX parent_events ON events(parent_uuid)")
        self.db.execute("CREATE INDEX expired_events ON events(lease_until) WHERE state='sending'")
        self.db.execute("CREATE INDEX queued_sizes ON events(length(body)) WHERE body IS NOT NULL")
        self.db.execute(f"PRAGMA application_id={APPLICATION_ID}")
        self.db.execute("PRAGMA user_version=1")

    def _migrate(self):
        version = self.db.execute("PRAGMA user_version").fetchone()[0]
        if version == SCHEMA:
            return
        if version not in (1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11):
            raise InvalidData("Unsupported outbox migration")
        if version == 1:
            self._migrate_runs()
        if version < 3:
            self._migrate_dispatch()
        if version < 4:
            from .tickets import migrate
            migrate(self.db)
        if version < 5:
            from .source_calls import migrate
            migrate(self.db)
        if version < 6:
            from .backfill_calls import migrate
            migrate(self.db)
        if version < 7:
            from .n8n_receipts import migrate
            migrate(self.db)
        if version < 8:
            from .enrichment_journal import migrate
            migrate(self.db)
        if version < 9:
            from .enrichment_dispatch import migrate
            migrate(self.db)
        if version < 10:
            from .collection_dispatch import migrate
            migrate(self.db)
        if version < 11:
            from .discovery_journal import migrate
            migrate(self.db)
        from .discovery_dispatch import migrate
        migrate(self.db)
        self.db.execute(f"PRAGMA user_version={SCHEMA}")

    def _migrate_dispatch(self):
        self.db.execute("""CREATE TABLE dispatch_cursors(
            root_uuid TEXT NOT NULL, policy_sha256 TEXT NOT NULL,
            after_sequence INTEGER NOT NULL DEFAULT 0 CHECK(after_sequence>=0),
            revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
            failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
            available_at REAL NOT NULL DEFAULT 0, error_code TEXT,
            PRIMARY KEY(root_uuid,policy_sha256)
        )""")
        self.db.execute("PRAGMA user_version=3")

    def _migrate_runs(self):
        self.db.execute("""CREATE TABLE run_intents(
            uuid TEXT PRIMARY KEY, config_sha256 TEXT NOT NULL UNIQUE, template BLOB NOT NULL,
            operation TEXT NOT NULL CHECK(operation IN ('download','enrich')),
            windows BLOB NOT NULL, window_count INTEGER NOT NULL CHECK(window_count BETWEEN 0 AND 64),
            latest_until TEXT NOT NULL, pending_since REAL, created_at REAL NOT NULL, updated_at REAL NOT NULL,
            CHECK((window_count=0 AND pending_since IS NULL) OR (window_count>0 AND pending_since IS NOT NULL))
        )""")
        self.db.execute("""CREATE TABLE run_requests(
            seq INTEGER PRIMARY KEY, request_uuid TEXT NOT NULL UNIQUE,
            intent_uuid TEXT NOT NULL REFERENCES run_intents(uuid),
            sha256 TEXT NOT NULL, window BLOB NOT NULL, until_stamp TEXT NOT NULL,
            body BLOB, receipt BLOB, run_uuid TEXT,
            state TEXT NOT NULL CHECK(state IN ('pending','sending','admitted','review')),
            owner TEXT, fence INTEGER NOT NULL DEFAULT 0, lease_until REAL,
            attempts INTEGER NOT NULL DEFAULT 0, available_at REAL NOT NULL,
            created_at REAL NOT NULL, admitted_at REAL, error_code TEXT,
            CHECK((state='sending' AND owner IS NOT NULL AND lease_until IS NOT NULL)
                OR (state!='sending' AND owner IS NULL AND lease_until IS NULL)),
            CHECK((state='admitted' AND receipt IS NOT NULL AND body IS NULL AND run_uuid IS NOT NULL)
                OR (state!='admitted' AND receipt IS NULL AND body IS NOT NULL AND run_uuid IS NULL))
        )""")
        self.db.execute("CREATE UNIQUE INDEX one_run_submission ON run_requests(intent_uuid) WHERE state!='admitted'")
        self.db.execute("CREATE INDEX ready_run_requests ON run_requests(state,available_at,until_stamp)")
        self.db.execute("CREATE INDEX expired_run_requests ON run_requests(lease_until) WHERE state='sending'")
        self.db.execute("CREATE INDEX run_request_history ON run_requests(intent_uuid,seq)")
        self.db.execute("CREATE INDEX pending_run_intents ON run_intents(operation,latest_until) WHERE window_count>0")
        self.db.execute("""CREATE TABLE run_intent_tickets(
            uuid TEXT PRIMARY KEY, intent_uuid TEXT NOT NULL REFERENCES run_intents(uuid),
            sha256 TEXT NOT NULL, window BLOB NOT NULL, first_sequence INTEGER NOT NULL CHECK(first_sequence>0),
            created_at REAL NOT NULL
        )""")
        self.db.execute("PRAGMA user_version=2")

    @contextmanager
    def transaction(self):
        self.db.execute("BEGIN IMMEDIATE")
        try:
            yield
            self.db.execute("COMMIT")
        except BaseException:
            self.db.execute("ROLLBACK")
            raise

    def close(self):
        self.db.close()

    def metadata_reserved_bytes(self):
        # Call inside the same transaction as capacity admission. Reservations
        # cover bytes that an in-flight extractor can still return after a crash.
        return self.db.execute("""SELECT
            (SELECT coalesce(sum(reserved_bytes),0) FROM enrichment_executions) +
            (SELECT coalesce(sum(reserved_bytes),0) FROM discovery_executions)""").fetchone()[0]

    def enqueue(self, body):
        event = events.validate(body)
        if event["producer_uuid"] != self.producer:
            raise Conflict("Event belongs to a different producer")
        event_id, sha = event["event_uuid"], digest(body)
        source = event.get("source") if event["kind"] == "file.completed" else None
        parent_id = source["capture_event_uuid"] if source else None
        with self.transaction():
            previous = self.db.execute("SELECT sha256 FROM events WHERE event_uuid=?", (event_id,)).fetchone()
            if previous:
                if previous[0] != sha:
                    raise Conflict("Event UUID already identifies different bytes")
                return event_id
            if parent_id:
                parent = self.db.execute("SELECT * FROM events WHERE event_uuid=?", (parent_id,)).fetchone()
                if (not parent or parent["kind"] != "source.capture"
                        or any(parent[k] != event[k] for k in ("collection_uuid", "collection_revision", "root_uuid"))):
                    raise Conflict("File source must already be queued for the same collection and root")
            count, size = self.db.execute("SELECT count(*),coalesce(sum(length(body)),0) FROM events WHERE body IS NOT NULL").fetchone()
            size += self.metadata_reserved_bytes()
            if count >= self.max_events or size + len(body) > self.max_bytes:
                raise Capacity("Outbox capacity exhausted; pause new downloads and drain or review the queue")
            now = self.clock()
            self.db.execute("""INSERT INTO events(event_uuid,sha256,kind,collection_uuid,
                collection_revision,root_uuid,run_uuid,parent_uuid,body,state,available_at,created_at)
                VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?)""",
                (event_id, sha, event["kind"], event["collection_uuid"], event["collection_revision"],
                 event["root_uuid"], event["run_uuid"], parent_id, body, now, now))
        return event_id

    def claim(self, owner, *, seconds=60, limit=8, max_bytes=MAX_BATCH_BYTES, kinds=None):
        identifier(owner)
        if (type(seconds) is not int or not 5 <= seconds <= 900
                or type(limit) is not int or not 1 <= limit <= 8 or max_bytes < 1024):
            raise InvalidData("Invalid delivery lease or batch limit")
        kinds = tuple(kinds if kinds is not None else ("source.capture", "file.completed"))
        if not kinds or not set(kinds) <= {"source.capture", "file.completed"}:
            raise InvalidData("Invalid delivery kinds")
        max_bytes = min(max_bytes, MAX_BATCH_BYTES)
        result, size = [], 32
        with self.transaction():
            now = self.clock()
            self.db.execute("""UPDATE events SET state='pending',owner=NULL,lease_until=NULL,
                error_code='delivery_interrupted' WHERE state='sending' AND lease_until<=?""", (now,))
            placeholders = ",".join("?" for _ in kinds)
            rows = self.db.execute(f"""SELECT e.* FROM events e WHERE state='pending'
                AND available_at<=? AND kind IN ({placeholders})
                AND (parent_uuid IS NULL OR EXISTS(SELECT 1 FROM events p
                    WHERE p.event_uuid=e.parent_uuid AND p.state='acknowledged'))
                ORDER BY available_at,seq LIMIT ?""", (now, *kinds, limit)).fetchall()
            for row in rows:
                if size + len(row["body"]) + 128 > max_bytes:
                    continue
                size += len(row["body"]) + 128
                fence = row["fence"] + 1
                self.db.execute("""UPDATE events SET state='sending',owner=?,fence=?,
                    lease_until=?,attempts=attempts+1 WHERE event_uuid=?""",
                    (owner, fence, now + seconds, row["event_uuid"]))
                result.append(Delivery(row["event_uuid"], row["sha256"], row["body"],
                                       owner, fence, row["attempts"] + 1))
        return result

    def _owned(self, delivery):
        row = self.db.execute("SELECT * FROM events WHERE event_uuid=?", (delivery.event_uuid,)).fetchone()
        if (not row or row["state"] != "sending" or row["owner"] != delivery.owner
                or row["fence"] != delivery.fence or row["lease_until"] <= self.clock()
                or row["sha256"] != delivery.sha256):
            raise LeaseLost("Delivery lease expired or was replaced; the event remains queued")
        return row

    def acknowledge(self, delivery, receipt):
        raw = encode(receipt, 65536)
        with self.transaction():
            row = self._owned(delivery)
            if (not isinstance(receipt, dict) or receipt.get("producer_uuid") != self.producer
                    or any(receipt.get(k) != row[k] for k in ("event_uuid", "sha256", "kind",
                        "collection_uuid", "collection_revision", "root_uuid", "run_uuid"))):
                raise Conflict("Server receipt does not acknowledge this event")
            identifier(receipt.get("credential_uuid"))
            if not isinstance(receipt.get("result"), dict) or not receipt.get("committed_at"):
                raise InvalidData("Incomplete server receipt")
            if row["kind"] == "source.capture":
                identifier(receipt.get("capture_uuid"))
                identifier(receipt.get("post_uuid"))
            else:
                identifier(receipt.get("job_uuid"))
            self.db.execute("""UPDATE events SET state='acknowledged',receipt=?,body=NULL,
                owner=NULL,lease_until=NULL,acknowledged_at=?,error_code=NULL WHERE event_uuid=?""",
                (raw, self.clock(), delivery.event_uuid))

    def fail(self, delivery, code, *, review=False, retry_after=0):
        # Persist only adapter-defined codes, never server bodies, URLs or tokens.
        if (not isinstance(code, str) or not code or len(code) > 64
                or not all(c in "abcdefghijklmnopqrstuvwxyz0123456789_" for c in code)):
            raise InvalidData("Invalid delivery error code")
        if type(retry_after) is not int or retry_after < 0:
            raise InvalidData("Invalid retry delay")
        with self.transaction():
            self._owned(delivery)
            delay = min(86400, max(5 * 2**min(delivery.attempts - 1, 15), retry_after))
            self.db.execute("""UPDATE events SET state=?,owner=NULL,lease_until=NULL,
                available_at=?,error_code=? WHERE event_uuid=?""",
                ("review" if review else "pending", self.clock() + delay, code, delivery.event_uuid))

    def retry(self, event_uuid):
        identifier(event_uuid)
        with self.transaction():
            changed = self.db.execute("""UPDATE events SET state='pending',available_at=?,error_code=NULL
                WHERE event_uuid=? AND state='review'""", (self.clock(), event_uuid)).rowcount
            if changed != 1:
                raise Conflict("Only an event requiring review can be explicitly retried")

    def receipt(self, event_uuid):
        identifier(event_uuid)
        row = self.db.execute("SELECT receipt FROM events WHERE event_uuid=?", (event_uuid,)).fetchone()
        return decode(row[0], 65536) if row and row[0] is not None else None

    def status(self):
        rows = self.db.execute("""SELECT state,count(*) AS count,coalesce(sum(length(body)),0) AS bytes,
            min(created_at) AS oldest, min(available_at) AS available FROM events GROUP BY state""").fetchall()
        states = {state: 0 for state in ("pending", "sending", "acknowledged", "review")}
        outstanding = [r for r in rows if r["state"] != "acknowledged"]
        states.update({r["state"]: r["count"] for r in rows})
        return {"counts": states, "queued_bytes": sum(r["bytes"] for r in outstanding),
                "oldest_queued_seconds": max(0, self.clock() - min(r["oldest"] for r in outstanding)) if outstanding else None,
                "next_attempt_at": min((r["available"] for r in rows if r["state"] == "pending"), default=None)}
