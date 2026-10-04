"""Durable one-page delivery; native receipts own listing progress and history."""

from contextlib import contextmanager
from dataclasses import dataclass
import fcntl
import os
import stat
import uuid

from .client import Unavailable
from .discovery_client import DiscoveryClient, page_bytes
from .discovery_fetch import ERRORS, MAX_RECORDS, validate_page
from .encoding import InvalidData, decode, digest, encode, identifier
from .metadata_bundle import MAX_BYTES
from .outbox import Capacity, Conflict, LeaseLost

STATE_LIMIT = 64 << 10
LOCAL_ERRORS = frozenset({"", "native_delivery_unavailable", "source_ownership_unavailable",
    "unacknowledged_source_evidence", "page_requires_review", "native_job_failed", "native_job_cancelled"})


def migrate(db):
    db.execute(f"""CREATE TABLE discovery_executions(
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
    db.execute("CREATE INDEX discovery_local_phase ON discovery_executions(phase,updated_at,job_uuid)")
    db.execute("""CREATE TRIGGER discovery_definition_immutable
        BEFORE UPDATE OF job_uuid,definition,definition_sha256 ON discovery_executions
        BEGIN SELECT RAISE(ABORT,'discovery definitions are immutable'); END""")
    db.execute("""CREATE TRIGGER discovery_local_finished BEFORE UPDATE ON discovery_executions
        WHEN OLD.phase IN ('completed','failed')
        BEGIN SELECT RAISE(ABORT,'finished discovery acknowledgements are immutable'); END""")


@dataclass(frozen=True)
class Execution:
    job_uuid: str
    revision: int
    phase: str
    definition: dict
    state: dict
    body: bytes | None
    reserved_bytes: int


class DiscoveryJournal:
    def __init__(self, box, *, max_pending=10000, retained_finished=1000):
        if (type(max_pending) is not int or not 1 <= max_pending <= 1000000
                or type(retained_finished) is not int or not 1 <= retained_finished <= 10000):
            raise InvalidData("Invalid discovery journal capacity")
        self.box, self.db = box, box.db
        self.max_pending, self.retained_finished, self._locked = max_pending, retained_finished, False

    @contextmanager
    def execution(self):
        # Kernel ownership ends on process death. Keep this separate from both
        # the database's short write transactions and the enrichment worker lock.
        if self._locked:
            raise Conflict("Discovery execution is already locked")
        path = self.box.path.with_name(self.box.path.name + ".discovery.lock")
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        try:
            opened = os.fstat(fd)
            if not stat.S_ISREG(opened.st_mode) or opened.st_nlink != 1:
                raise InvalidData("Discovery process lock must be a regular local file")
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                yield False
                return
            current = path.stat(follow_symlinks=False)
            if (opened.st_dev, opened.st_ino) != (current.st_dev, current.st_ino):
                raise Conflict("Discovery process lock was replaced")
            self._locked = True
            yield True
        finally:
            self._locked = False
            os.close(fd)

    def _require_lock(self):
        if not self._locked:
            raise LeaseLost("Discovery journal changes require local execution ownership")

    @staticmethod
    def definition(description):
        work, listing = DiscoveryClient._description(description)
        return {"job_uuid": description["job"]["uuid"], "arguments": work,
                "listing": listing, "cursor": description["cursor"]}

    def find(self, job):
        identifier(job)
        row = self.db.execute("SELECT * FROM discovery_executions WHERE job_uuid=?", (job,)).fetchone()
        if row is None:
            return None
        if (digest(row["definition"]) != row["definition_sha256"] or digest(row["state"]) != row["state_sha256"]
                or (row["body"] is not None and digest(row["body"]) != row["body_sha256"])):
            raise InvalidData("Discovery journal differs from its recorded digests")
        value = Execution(job, row["revision"], row["phase"], decode(row["definition"], STATE_LIMIT),
                          decode(row["state"], STATE_LIMIT), row["body"], row["reserved_bytes"])
        self._validate(value)
        return value

    def _validate(self, value):
        state = value.state
        if (not isinstance(value.definition, dict) or set(value.definition) != {"job_uuid", "arguments", "listing", "cursor"}
                or value.definition.get("job_uuid") != value.job_uuid
                or value.phase not in ("active", "review", "completed", "failed")
                or type(value.reserved_bytes) is not int or not len(value.body or b"") <= value.reserved_bytes <= MAX_BYTES
                or (value.phase in ("completed", "failed") and (value.body is not None or value.reserved_bytes))
                or not isinstance(state, dict) or set(state) != {"claim", "lease", "pending", "receipt", "failure", "terminal", "error_code"}
                or not isinstance(state["error_code"], str) or state["error_code"] not in LOCAL_ERRORS | ERRORS):
            raise InvalidData("Invalid discovery journal state")
        claim, lease, pending, terminal = state["claim"], state["lease"], state["pending"], state["terminal"]
        try:
            if terminal is not None:
                if self.definition(terminal) != value.definition:
                    raise InvalidData("Native terminal evidence belongs to another page")
                native_state = terminal["job"]["state"]
                expected_phase = "completed" if native_state == "succeeded" else "failed"
                if (native_state not in ("succeeded", "failed", "cancelled") or value.phase != expected_phase
                        or pending is not None or claim is not None or lease is not None):
                    raise InvalidData("Native terminal evidence cannot discard a pending delivery")
            if claim is not None:
                if not isinstance(claim, dict) or set(claim) != {"description", "owner_uuid"}:
                    raise InvalidData("Invalid discovery claim intent")
                identifier(claim["owner_uuid"])
                if self.definition(claim["description"]) != value.definition:
                    raise InvalidData("Discovery claim differs from the original page")
            if lease is not None:
                DiscoveryClient._job(lease, value.job_uuid)
                if (claim is None or lease["state"] != "running" or lease["owner_uuid"] != claim["owner_uuid"]
                        or lease["arguments"] != value.definition["arguments"]):
                    raise InvalidData("Discovery lease differs from its saved claim")
            if pending is not None:
                if not isinstance(pending, dict) or pending.get("kind") not in ("page", "failure"):
                    raise InvalidData("Invalid discovery delivery intent")
                DiscoveryClient.lease(pending.get("lease"))
                if pending["kind"] == "page":
                    if (set(pending) != {"kind", "lease", "sha256", "record_count", "complete"}
                            or value.body is None or pending["sha256"] != digest(value.body)
                            or type(pending["record_count"]) is not int or not 0 <= pending["record_count"] <= MAX_RECORDS
                            or type(pending["complete"]) is not bool):
                        raise InvalidData("Discovery page changed after delivery was prepared")
                elif (set(pending) != {"kind", "lease", "error_code"}
                      or not isinstance(pending["error_code"], str) or pending["error_code"] not in ERRORS):
                    raise InvalidData("Invalid discovery failure intent")
            if (value.body is not None) != (pending is not None and pending["kind"] == "page"):
                raise InvalidData("Discovery journal lost its pending page association")
            if value.phase == "completed" and terminal is None:
                if lease is None or state["receipt"] is None or pending is not None:
                    raise InvalidData("Discovery completion requires its own page receipt")
                DiscoveryClient._receipt(state["receipt"], lease)
                if state["receipt"]["producer_uuid"] != self.box.producer or state["receipt"]["fence"] != lease["fence"]:
                    raise InvalidData("Discovery completion belongs to another attempt")
            elif state["receipt"] is not None:
                raise InvalidData("A retained page receipt must finish its delivery")
            if state["failure"] is not None:
                failure = state["failure"]
                if (not isinstance(failure, dict) or not isinstance(failure.get("error_code"), str)
                        or failure["error_code"] not in ERRORS):
                    raise InvalidData("Invalid discovery failure acknowledgement")
                DiscoveryClient._failure(failure, value.job_uuid, DiscoveryClient.lease(failure), self.box.producer, failure["error_code"])
            if value.phase == "failed" and terminal is None and (state["failure"] is None or state["failure"]["outcome"] != "failed"
                    or pending is not None or claim is not None or lease is not None):
                raise InvalidData("Terminal discovery failure requires its acknowledgement")
        except Unavailable:
            raise InvalidData("Discovery journal has inconsistent native evidence") from None

    def prepare(self, description):
        self._require_lock()
        definition = self.definition(description)
        raw = encode(definition, STATE_LIMIT)
        job = definition["job_uuid"]
        with self.box.transaction():
            prior = self.find(job)
            if prior is not None:
                if encode(prior.definition, STATE_LIMIT) != raw:
                    raise Conflict("Discovery job already identifies another listing or cursor")
                return prior
            if description["job"]["state"] not in ("queued", "running"):
                raise Conflict("An ended native page cannot start a fresh local execution")
            if self.db.execute("SELECT count(*) FROM discovery_executions WHERE phase IN ('active','review')").fetchone()[0] >= self.max_pending:
                raise Capacity("Discovery journal is full; pending evidence was preserved")
            self.db.execute("""DELETE FROM discovery_executions WHERE job_uuid IN (
                SELECT job_uuid FROM discovery_executions WHERE phase IN ('completed','failed')
                ORDER BY updated_at DESC,job_uuid LIMIT -1 OFFSET ?)""", (self.retained_finished,))
            initial = encode({"claim": None, "lease": None, "pending": None, "receipt": None, "failure": None, "terminal": None, "error_code": ""})
            now = self.box.clock()
            self.db.execute("""INSERT INTO discovery_executions(job_uuid,definition,definition_sha256,revision,phase,state,state_sha256,created_at,updated_at)
                VALUES(?,?,?,1,'active',?,?,?,?)""", (job, raw, digest(raw), initial, digest(initial), now, now))
        return self.find(job)

    def _change(self, value, *, state=None, phase=None, body=False, reserved=None):
        self._require_lock()
        raw = encode(value.state if state is None else state, STATE_LIMIT)
        data = value.body if body is False else body
        size = value.reserved_bytes if reserved is None else reserved
        selected = phase or value.phase
        if size > value.reserved_bytes:
            raise Conflict("Discovery capacity must be reserved before use")
        candidate = Execution(value.job_uuid, value.revision + 1, selected, value.definition, decode(raw, STATE_LIMIT), data, size)
        self._validate(candidate)
        with self.box.transaction():
            current = self.find(value.job_uuid)
            if (current is None or current.revision != value.revision or current.definition != value.definition
                    or current.phase in ("completed", "failed")):
                raise Conflict("Discovery execution changed")
            self.db.execute("""UPDATE discovery_executions SET revision=revision+1,phase=?,state=?,state_sha256=?,body=?,body_sha256=?,
                reserved_bytes=?,updated_at=? WHERE job_uuid=?""", (selected, raw, digest(raw), data,
                digest(data) if data is not None else None, size, self.box.clock(), value.job_uuid))
        return self.find(value.job_uuid)

    def reserve(self, value):
        self._require_lock()
        if value.body is not None or value.state["pending"] is not None or value.phase != "active":
            raise Conflict("Deliver saved discovery evidence before fetching again")
        with self.box.transaction():
            current = self.find(value.job_uuid)
            if current is None or current.revision != value.revision:
                raise Conflict("Discovery execution changed")
            events = self.db.execute("SELECT coalesce(sum(length(body)),0) FROM events WHERE body IS NOT NULL").fetchone()[0]
            if events + self.box.metadata_reserved_bytes() - current.reserved_bytes + MAX_BYTES > self.box.max_bytes:
                raise Capacity("Reserve a full discovery page before fetching source data")
            self.db.execute("UPDATE discovery_executions SET reserved_bytes=?,revision=revision+1,updated_at=? WHERE job_uuid=?",
                            (MAX_BYTES, self.box.clock(), value.job_uuid))
        return self.find(value.job_uuid)

    def release_reservation(self, value):
        if value.body is not None:
            raise Conflict("Unacknowledged discovery bytes must stay reserved")
        return self._change(value, reserved=0)

    def claim(self, value, description):
        self._require_lock()
        if self.definition(description) != value.definition or value.phase != "active":
            raise Conflict("Discovery claim differs from its original active page")
        if value.state["pending"] is not None and value.state["pending"]["kind"] != "page":
            raise Conflict("A saved failure must be acknowledged before claiming again")
        job, prior = description["job"], value.state["claim"]
        if prior is not None and (prior["description"] == description or (job["state"] == "running"
                and job.get("owner_uuid") == prior["owner_uuid"])):
            current = self.find(value.job_uuid)
            if current is None or current.revision != value.revision:
                raise Conflict("Discovery execution changed")
            return value
        if job["state"] != "queued":
            raise Conflict("Only queued pages or the original claim owner can be claimed")
        return self._change(value, state={**value.state, "claim": {"description": description, "owner_uuid": str(uuid.uuid4())}, "lease": None})

    def claimed(self, value, job):
        claim = value.state["claim"]
        DiscoveryClient._job(job, value.job_uuid)
        if claim is None:
            raise Conflict("Discovery claim acknowledgement has no saved intent")
        expected = claim["description"]["job"]
        fence = expected["fence"] if expected["state"] == "running" else expected["fence"] + 1
        if (job["state"] != "running" or job.get("owner_uuid") != claim["owner_uuid"] or job["fence"] != fence
                or job["arguments"] != value.definition["arguments"] or job["revision"] < expected["revision"]):
            raise Conflict("Discovery claim acknowledgement changed ownership")
        return self._change(value, state={**value.state, "lease": job, "error_code": ""})

    @staticmethod
    def _owned(value, lease):
        owned = DiscoveryClient.lease(lease)
        if value.state["lease"] is None or DiscoveryClient.lease(value.state["lease"]) != owned:
            raise Conflict("Discovery delivery differs from its saved owned attempt")
        return owned

    def page(self, value, lease, body):
        if value.state["pending"] is not None or value.reserved_bytes != MAX_BYTES or value.phase != "active":
            raise Conflict("Discovery extraction did not reserve its page")
        owned = self._owned(value, lease)
        listing = value.definition["listing"]
        body = validate_page(body, listing["profile_url"], listing["extractor_version"], value.definition["cursor"])
        raw = page_bytes(body)
        pending = {"kind": "page", "lease": owned, "sha256": digest(raw),
                   "record_count": len(body["records"]), "complete": body["complete"]}
        return self._change(value, state={**value.state, "pending": pending}, body=raw, reserved=len(raw))

    def failure(self, value, lease, code):
        if value.state["pending"] is not None or value.body is not None or value.phase != "active":
            raise Conflict("Discovery evidence still requires acknowledgement")
        if not isinstance(code, str) or code not in ERRORS:
            raise InvalidData("Invalid discovery failure intent")
        pending = {"kind": "failure", "lease": self._owned(value, lease), "error_code": code}
        return self._change(value, state={**value.state, "pending": pending}, reserved=0)

    def rebind_page(self, value, lease):
        # The executor must first try the original delivery. A definitive lease
        # conflict and a newly claimed attempt permit same-producer byte reuse.
        pending = value.state["pending"]
        if value.phase != "active" or pending is None or pending["kind"] != "page":
            raise Conflict("Only an unacknowledged page can change attempt ownership")
        return self._change(value, state={**value.state, "pending": {**pending, "lease": self._owned(value, lease)}})

    def acknowledged(self, value, receipt):
        pending = value.state["pending"]
        if pending is None:
            raise Conflict("There is no pending discovery delivery")
        state = {**value.state, "pending": None, "error_code": ""}
        try:
            if pending["kind"] == "page":
                DiscoveryClient._receipt(receipt, value.state["lease"])
                if (receipt["producer_uuid"] != self.box.producer or receipt["fence"] != pending["lease"]["fence"]
                        or any(receipt[key] != pending[key] for key in ("sha256", "record_count", "complete"))):
                    raise Conflict("Discovery acknowledgement identifies another page or attempt")
                state["receipt"], phase = receipt, "completed"
            else:
                DiscoveryClient._failure(receipt, value.job_uuid, pending["lease"], self.box.producer, pending["error_code"])
                state.update(failure=receipt, claim=None, lease=None, error_code=pending["error_code"])
                phase = "failed" if receipt["outcome"] == "failed" else "active"
        except Unavailable:
            raise Conflict("Discovery acknowledgement differs from its delivery intent") from None
        return self._change(value, state=state, phase=phase, body=None, reserved=0)

    def note(self, value, code, *, review=False):
        if not isinstance(code, str) or code not in LOCAL_ERRORS:
            raise InvalidData("Invalid local discovery outcome")
        return self._change(value, state={**value.state, "error_code": code}, phase="review" if review else value.phase)

    def observe_terminal(self, value, description):
        if value.body is not None or value.state["pending"] is not None:
            raise Conflict("Native job status cannot discard an unacknowledged discovery delivery")
        if self.definition(description) != value.definition or description["job"]["state"] not in ("succeeded", "failed", "cancelled"):
            raise Conflict("Discovery terminal status differs from its original page")
        native_state = description["job"]["state"]
        phase = "completed" if native_state == "succeeded" else "failed"
        return self._change(value, phase=phase, reserved=0, state={**value.state, "claim": None, "lease": None,
            "terminal": description, "error_code": "" if phase == "completed" else "native_job_" + native_state})

    def summary(self, job=None):
        if job is not None:
            value = self.find(job)
            if value is None:
                return None
            pending = value.state["pending"]
            return {"job_uuid": job, "phase": value.phase, "revision": value.revision,
                    "pending": pending["kind"] if pending else None,
                    "staged_bytes": len(value.body or b""), "reserved_bytes": value.reserved_bytes,
                    "error_code": value.state["error_code"],
                    "receipt": value.state["receipt"] or (value.state["terminal"] or {}).get("receipt")}
        counts = {key: 0 for key in ("active", "review", "completed", "failed")}
        counts.update({row["phase"]: row["total"] for row in self.db.execute(
            "SELECT phase,count(*) AS total FROM discovery_executions GROUP BY phase")})
        used = self.db.execute("SELECT coalesce(sum(length(body)),0),coalesce(sum(reserved_bytes),0) FROM discovery_executions").fetchone()
        return {"counts": counts, "staged_bytes": used[0], "reserved_bytes": used[1]}
