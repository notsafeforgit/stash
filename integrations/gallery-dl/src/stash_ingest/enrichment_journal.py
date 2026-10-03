"""Local enrichment delivery journal; native receipts own archive history."""

from contextlib import contextmanager
from dataclasses import dataclass
import fcntl
import os
import stat
import uuid

from .encoding import InvalidData, decode, digest, encode, identifier
from .enrichment_client import EnrichmentClient, RETRYABLE, checkpoint_bytes
from .metadata_bundle import Bundle, ERRORS, MAX_BYTES
from .outbox import Capacity, Conflict, LeaseLost

STATE_LIMIT = 64 << 10


def migrate(db):
    db.execute(f"""CREATE TABLE enrichment_executions(
        job_uuid TEXT PRIMARY KEY, definition BLOB NOT NULL, definition_sha256 TEXT NOT NULL,
        revision INTEGER NOT NULL CHECK(revision>0),
        phase TEXT NOT NULL CHECK(phase IN ('active','review','completed','failed')),
        state BLOB NOT NULL, state_sha256 TEXT NOT NULL,
        body BLOB, body_sha256 TEXT, reserved_bytes INTEGER NOT NULL DEFAULT 0,
        created_at REAL NOT NULL, updated_at REAL NOT NULL,
        CHECK((body IS NULL)=(body_sha256 IS NULL)),
        CHECK(reserved_bytes BETWEEN 0 AND {MAX_BYTES}),
        CHECK(reserved_bytes>=coalesce(length(body),0)),
        CHECK(phase NOT IN ('completed','failed') OR (body IS NULL AND reserved_bytes=0))
    )""")
    db.execute("CREATE INDEX enrichment_local_phase ON enrichment_executions(phase,updated_at,job_uuid)")
    db.execute("""CREATE TRIGGER enrichment_definition_immutable
        BEFORE UPDATE OF job_uuid,definition,definition_sha256 ON enrichment_executions
        BEGIN SELECT RAISE(ABORT,'enrichment definitions are immutable'); END""")
    db.execute("""CREATE TRIGGER enrichment_local_completed BEFORE UPDATE ON enrichment_executions
        WHEN OLD.phase='completed'
        BEGIN SELECT RAISE(ABORT,'completed enrichment acknowledgements are immutable'); END""")


@dataclass(frozen=True)
class Execution:
    job_uuid: str
    revision: int
    phase: str
    definition: dict
    state: dict
    body: bytes | None
    reserved_bytes: int


class EnrichmentJournal:
    def __init__(self, box, *, max_pending=10000, retained_finished=1000):
        if (type(max_pending) is not int or not 1 <= max_pending <= 1000000
                or type(retained_finished) is not int or not 1 <= retained_finished <= 10000):
            raise InvalidData("Invalid enrichment journal capacity")
        self.box, self.db = box, box.db
        self.max_pending, self.retained_finished = max_pending, retained_finished
        self._locked = False

    @contextmanager
    def execution(self):
        # A kernel process lock has no stale local lease to guess after a crash.
        # It is separate from SQLite's byte-range database locks. Other outbox
        # delivery and downloads continue to use short ordinary transactions.
        if self._locked:
            raise Conflict("Enrichment execution is already locked")
        path = self.box.path.with_name(self.box.path.name + ".enrichment.lock")
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        try:
            opened = os.fstat(fd)
            if not stat.S_ISREG(opened.st_mode) or opened.st_nlink != 1:
                raise InvalidData("Enrichment process lock must be a regular local file")
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                yield False
                return
            current = path.stat(follow_symlinks=False)
            if (opened.st_dev, opened.st_ino) != (current.st_dev, current.st_ino):
                raise Conflict("Enrichment process lock was replaced")
            self._locked = True
            yield True
        finally:
            self._locked = False
            os.close(fd)

    def _require_lock(self):
        if not self._locked:
            raise LeaseLost("Enrichment journal changes require local execution ownership")

    @staticmethod
    def definition(execution):
        job, target = execution["job"], execution["target"]
        work = EnrichmentClient._job(job)
        if (not isinstance(target, dict) or work["target_uuid"] != target.get("uuid")
                or any(work[k] != target.get(k) for k in ("post_uuid", "collection_uuid", "collection_revision"))
                or target.get("policy") != "gallery-dl-metadata-v1"):
            raise InvalidData("Enrichment target differs from its native job")
        return {"job_uuid": job["uuid"], "arguments": work, "url": target["url"], "policy": target["policy"]}

    def find(self, job):
        identifier(job)
        row = self.db.execute("SELECT * FROM enrichment_executions WHERE job_uuid=?", (job,)).fetchone()
        if row is None:
            return None
        if (digest(row["definition"]) != row["definition_sha256"] or digest(row["state"]) != row["state_sha256"]
                or (row["body"] is not None and digest(row["body"]) != row["body_sha256"])):
            raise InvalidData("Enrichment journal differs from its recorded digests")
        value = Execution(job, row["revision"], row["phase"], decode(row["definition"], STATE_LIMIT),
                          decode(row["state"], STATE_LIMIT), row["body"], row["reserved_bytes"])
        if not isinstance(value.definition, dict) or not isinstance(value.state, dict):
            raise InvalidData("Invalid enrichment journal state")
        pending = value.state.get("pending")
        if bool(value.body is not None) != bool(pending and pending.get("kind") == "checkpoint"):
            raise InvalidData("Enrichment journal lost its pending checkpoint association")
        if value.body is not None and pending.get("sha256") != row["body_sha256"]:
            raise InvalidData("Enrichment checkpoint changed after delivery was prepared")
        return value

    def prepare(self, execution):
        self._require_lock()
        definition = self.definition(execution)
        body = encode(definition, STATE_LIMIT)
        job = definition["job_uuid"]
        with self.box.transaction():
            prior = self.find(job)
            if prior is not None:
                if encode(prior.definition, STATE_LIMIT) != body:
                    raise Conflict("Enrichment job already identifies a different source or policy")
                return prior
            if self.db.execute("SELECT count(*) FROM enrichment_executions WHERE phase IN ('active','review')").fetchone()[0] >= self.max_pending:
                raise Capacity("Enrichment journal is full; pending evidence was preserved")
            # Finished receipts are a bounded local convenience. Native jobs and
            # publications retain the authoritative completion history.
            self.db.execute("""DELETE FROM enrichment_executions WHERE job_uuid IN (
                SELECT job_uuid FROM enrichment_executions WHERE phase IN ('completed','failed')
                ORDER BY updated_at DESC,job_uuid LIMIT -1 OFFSET ?)""", (self.retained_finished,))
            initial = encode({"claim": None, "lease": None, "pending": None, "checkpoint": None,
                              "publication": None, "error_code": ""})
            now = self.box.clock()
            self.db.execute("""INSERT INTO enrichment_executions(job_uuid,definition,definition_sha256,revision,phase,state,state_sha256,created_at,updated_at)
                VALUES(?,?,?,1,'active',?,?,?,?)""", (job, body, digest(body), initial, digest(initial), now, now))
        return self.find(job)

    def change(self, value, *, state=None, phase=None, body=False, reserved=None):
        self._require_lock()
        state = encode(value.state if state is None else state, STATE_LIMIT)
        data = value.body if body is False else body
        size = value.reserved_bytes if reserved is None else reserved
        selected = phase or value.phase
        if size > value.reserved_bytes:
            raise Conflict("Enrichment capacity must be reserved before use")
        if selected == "completed":
            EnrichmentClient._publication(decode(state, STATE_LIMIT).get("publication"), value.job_uuid)
        with self.box.transaction():
            current = self.find(value.job_uuid)
            if current is None or current.revision != value.revision or current.phase == "completed":
                raise Conflict("Enrichment execution changed")
            self.db.execute("""UPDATE enrichment_executions SET revision=revision+1,phase=?,state=?,state_sha256=?,body=?,body_sha256=?,
                reserved_bytes=?,updated_at=? WHERE job_uuid=?""", (selected, state, digest(state), data,
                digest(data) if data is not None else None, size, self.box.clock(), value.job_uuid))
        return self.find(value.job_uuid)

    def reserve(self, value):
        self._require_lock()
        if value.body is not None or value.state["pending"] is not None or value.phase != "active":
            raise Conflict("Deliver saved enrichment evidence before fetching again")
        with self.box.transaction():
            current = self.find(value.job_uuid)
            if current is None or current.revision != value.revision:
                raise Conflict("Enrichment execution changed")
            events = self.db.execute("SELECT coalesce(sum(length(body)),0) FROM events WHERE body IS NOT NULL").fetchone()[0]
            used = self.db.execute("SELECT coalesce(sum(reserved_bytes),0) FROM enrichment_executions").fetchone()[0]
            if events + used - current.reserved_bytes + MAX_BYTES > self.box.max_bytes:
                raise Capacity("Reserve a full enrichment checkpoint before fetching source data")
            self.db.execute("UPDATE enrichment_executions SET reserved_bytes=?,revision=revision+1,updated_at=? WHERE job_uuid=?",
                            (MAX_BYTES, self.box.clock(), value.job_uuid))
        return self.find(value.job_uuid)

    def claim(self, value, job):
        EnrichmentClient._job(job, value.job_uuid)
        if job["arguments"] != value.definition["arguments"]:
            raise Conflict("Enrichment job changed its immutable definition")
        prior = value.state["claim"]
        if (prior is not None and (prior["job"] == job or (job["state"] == "running"
                and job.get("owner_uuid") == prior["owner_uuid"]))):
            return value
        return self.change(value, state={**value.state, "claim": {"job": job, "owner_uuid": str(uuid.uuid4())}, "lease": None})

    def claimed(self, value, job):
        claim = value.state["claim"]
        if claim is None or job["owner_uuid"] != claim["owner_uuid"] or job["arguments"] != value.definition["arguments"]:
            raise Conflict("Enrichment claim acknowledgement changed ownership")
        return self.change(value, state={**value.state, "lease": job, "error_code": ""})

    def checkpoint(self, value, lease, expected, body):
        if (value.state["pending"] is not None or value.reserved_bytes != MAX_BYTES
                or type(expected) is not int or expected < 0):
            raise Conflict("Enrichment extraction did not reserve its checkpoint")
        body = Bundle(value.definition["url"], value.definition["arguments"]["extractor_version"], body).checkpoint()
        raw = checkpoint_bytes(body)
        pending = {"kind": "checkpoint", "lease": EnrichmentClient.lease(lease), "expected_revision": expected, "sha256": digest(raw)}
        return self.change(value, state={**value.state, "pending": pending}, body=raw, reserved=len(raw))

    def intent(self, value, lease, kind, payload):
        if kind not in ("publish", "failure") or value.state["pending"] is not None or value.body is not None:
            raise Conflict("Enrichment evidence still requires acknowledgement")
        if kind == "publish":
            if set(payload) != {"receipt"}:
                raise InvalidData("Invalid enrichment publication intent")
            EnrichmentClient._receipt(payload["receipt"], value.job_uuid)
            if payload["receipt"]["pending_count"]:
                raise Conflict("Pending children cannot be published")
        elif (set(payload) != {"error_code"} or not isinstance(payload["error_code"], str)
              or payload["error_code"] not in ERRORS | {"post_identity_conflict"}):
            raise InvalidData("Invalid enrichment failure intent")
        pending = {"kind": kind, "lease": EnrichmentClient.lease(lease), **payload}
        return self.change(value, state={**value.state, "pending": pending}, reserved=0)

    def acknowledged(self, value, receipt):
        pending = value.state["pending"]
        if pending is None:
            raise Conflict("There is no pending enrichment delivery")
        state = {**value.state, "pending": None, "error_code": ""}
        phase = value.phase
        if pending["kind"] == "checkpoint":
            EnrichmentClient._receipt(receipt, value.job_uuid)
            body = decode(value.body, MAX_BYTES, preserve_numbers=True)
            if (receipt["sha256"] != pending["sha256"] or receipt["fence"] > pending["lease"]["fence"]
                    or receipt["revision"] > pending["expected_revision"] + 1
                    or (receipt["revision"] == pending["expected_revision"] + 1 and receipt["fence"] != pending["lease"]["fence"])
                    or any(receipt[field] != len(body[key]) for field, key in (("record_count", "records"),
                           ("pending_count", "pending"), ("unresolved_count", "unresolved")))):
                raise Conflict("Enrichment acknowledgement identifies another checkpoint")
            state["checkpoint"] = receipt
            if body["pending"]:
                reasons = [item["reason"] for item in body["pending"]]
                code = "rate_limited" if "rate_limited" in reasons else reasons[0]
                state["pending"] = {"kind": "failure", "lease": pending["lease"],
                                    "error_code": code if code in ERRORS else "invalid_checkpoint"}
            else:
                state["pending"] = {"kind": "publish", "lease": pending["lease"], "receipt": receipt}
        elif pending["kind"] == "publish":
            EnrichmentClient._publication(receipt, value.job_uuid)
            if receipt["checkpoint_sha256"] != pending["receipt"]["sha256"]:
                raise Conflict("Enrichment publication identifies another checkpoint")
            state["publication"], phase = receipt, "completed"
        else:
            expected = "retry" if pending["error_code"] in RETRYABLE and pending["lease"]["fence"] < 8 else "failed"
            if (receipt.get("job_uuid") != value.job_uuid or receipt.get("producer_uuid") != self.box.producer
                    or any(receipt.get(k) != pending["lease"][k] for k in ("owner_uuid", "fence"))
                    or receipt.get("error_code") != pending["error_code"] or receipt.get("outcome") != expected
                    or not receipt.get("ended_at")):
                raise Conflict("Enrichment failure acknowledgement identifies another attempt")
            state["failure"] = receipt
            state["claim"], state["lease"] = None, None
            phase = "failed" if receipt["outcome"] == "failed" else "active"
        return self.change(value, state=state, phase=phase, body=None, reserved=0)

    def summary(self, job=None):
        if job is not None:
            value = self.find(job)
            if value is None:
                return None
            pending = value.state["pending"]
            return {"job_uuid": job, "phase": value.phase, "revision": value.revision,
                    "pending": pending["kind"] if pending else None,
                    "staged_bytes": len(value.body or b""), "reserved_bytes": value.reserved_bytes,
                    "error_code": value.state["error_code"], "publication": value.state["publication"]}
        counts = {key: 0 for key in ("active", "review", "completed", "failed")}
        counts.update({row["phase"]: row["total"] for row in self.db.execute(
            "SELECT phase,count(*) AS total FROM enrichment_executions GROUP BY phase")})
        used = self.db.execute("SELECT coalesce(sum(length(body)),0),coalesce(sum(reserved_bytes),0) FROM enrichment_executions").fetchone()
        return {"counts": counts, "staged_bytes": used[0], "reserved_bytes": used[1]}
