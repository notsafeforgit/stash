"""Freeze caller inputs before lookup; bind each URL and its ticket atomically."""

from dataclasses import dataclass
import uuid

from .client import Unavailable
from .collections import lookup_collections, target_url
from .encoding import InvalidData, decode, digest, encode, identifier
from .events import sha256
from .outbox import Capacity, Conflict, LeaseLost
from .run_queue import RunQueue
from . import windows


def migrate(db):
    db.execute("""CREATE TABLE source_calls(
        seq INTEGER PRIMARY KEY, uuid TEXT NOT NULL UNIQUE,
        request_sha256 TEXT NOT NULL, definition BLOB NOT NULL, definition_sha256 TEXT NOT NULL,
        operation TEXT NOT NULL CHECK(operation IN ('download','enrich')), until_stamp TEXT NOT NULL,
        target_count INTEGER NOT NULL CHECK(target_count BETWEEN 1 AND 10000), created_at REAL NOT NULL
    )""")
    db.execute("""CREATE TABLE source_call_targets(
        seq INTEGER PRIMARY KEY, call_uuid TEXT NOT NULL REFERENCES source_calls(uuid),
        position INTEGER NOT NULL CHECK(position>0), target_url TEXT NOT NULL,
        state TEXT NOT NULL CHECK(state IN ('pending','resolving','queued','review')),
        collection_uuid TEXT, collection_revision INTEGER,
        ticket_uuid TEXT UNIQUE REFERENCES run_intent_tickets(uuid),
        intent_uuid TEXT REFERENCES run_intents(uuid),
        owner TEXT, fence INTEGER NOT NULL DEFAULT 0, lease_until REAL,
        attempts INTEGER NOT NULL DEFAULT 0, available_at REAL NOT NULL, error_code TEXT,
        UNIQUE(call_uuid,position), UNIQUE(call_uuid,target_url),
        CHECK((state='resolving' AND owner IS NOT NULL AND lease_until IS NOT NULL)
            OR (state!='resolving' AND owner IS NULL AND lease_until IS NULL)),
        CHECK((state='queued' AND collection_uuid IS NOT NULL AND collection_revision IS NOT NULL AND collection_revision>0
            AND ticket_uuid IS NOT NULL AND intent_uuid IS NOT NULL)
            OR (state!='queued' AND collection_uuid IS NULL AND collection_revision IS NULL
            AND ticket_uuid IS NULL AND intent_uuid IS NULL))
    )""")
    db.execute("CREATE INDEX ready_source_calls ON source_call_targets(state,available_at,seq)")
    db.execute("CREATE INDEX expired_source_calls ON source_call_targets(lease_until) WHERE state='resolving'")
    db.execute("""CREATE TRIGGER source_call_immutable BEFORE UPDATE ON source_calls
        BEGIN SELECT RAISE(ABORT,'caller snapshots are immutable'); END""")
    db.execute("""CREATE TRIGGER source_call_target_immutable BEFORE UPDATE OF call_uuid,position,target_url ON source_call_targets
        BEGIN SELECT RAISE(ABORT,'caller targets are immutable'); END""")
    db.execute("""CREATE TRIGGER source_call_binding_immutable BEFORE UPDATE ON source_call_targets WHEN OLD.state='queued'
        BEGIN SELECT RAISE(ABORT,'caller bindings are immutable'); END""")


def definition(value):
    if not isinstance(value, dict) or set(value) != {"root_uuid", "operation", "policy_sha256", "cooldown_seconds", "window"}:
        raise InvalidData("Caller definitions require a root, operation, policy, cooldown and window")
    if value["root_uuid"] is not None or value["operation"] == "download":
        identifier(value["root_uuid"])
    if (value["operation"] not in ("download", "enrich") or not sha256(value["policy_sha256"])
            or type(value["cooldown_seconds"]) is not int or not 0 <= value["cooldown_seconds"] <= 86400):
        raise InvalidData("Invalid caller source definition")
    return {**value, "window": windows.normalize(value["window"])}


def snapshot(value):
    if not isinstance(value, dict) or "targets" not in value:
        raise InvalidData("A caller snapshot requires source targets")
    targets = value["targets"]
    if not isinstance(targets, list) or not 1 <= len(targets) <= 10000:
        raise InvalidData("A caller snapshot requires 1–10000 targets")
    targets = [target_url(target) for target in targets]
    if len(set(targets)) != len(targets) or sum(len(target.encode()) for target in targets) > 8 << 20:
        raise InvalidData("Caller targets must be unique and bounded")
    spec = definition({key: item for key, item in value.items() if key != "targets"})
    return spec, targets


@dataclass(frozen=True)
class Resolution:
    call_uuid: str
    definition: dict
    owner: str
    targets: tuple


class SourceCalls:
    def __init__(self, box, *, max_calls=10000, max_targets=100000):
        if (type(max_calls) is not int or not 1 <= max_calls <= 1000000
                or type(max_targets) is not int or not 1 <= max_targets <= 1000000):
            raise InvalidData("Invalid caller queue capacity")
        self.box, self.db = box, box.db
        self.max_calls, self.max_targets = max_calls, max_targets

    def _find(self, call_uuid):
        identifier(call_uuid)
        return self.db.execute("SELECT * FROM source_calls WHERE uuid=?", (call_uuid,)).fetchone()

    def _definition(self, row):
        if digest(row["definition"]) != row["definition_sha256"]:
            raise InvalidData("Caller definition differs from its recorded digest")
        value = definition(decode(row["definition"], 8192))
        if (row["operation"], row["until_stamp"]) != (value["operation"], value["window"]["until"]):
            raise InvalidData("Caller scheduling fields differ from its frozen definition")
        return value

    def record(self, call_uuid, request_sha256, prepare):
        """Prepare files/config/time outside SQL, only for a new caller UUID.

        The request digest identifies the caller's options, not the mutable
        contents of its list/profile or a newly calculated relative cutoff.
        Concurrent first calls keep the snapshot that commits first.
        """
        identifier(call_uuid)
        if not sha256(request_sha256):
            raise InvalidData("A caller requires a stable request digest")
        previous = self._find(call_uuid)
        if previous is not None:
            if previous["request_sha256"] != request_sha256:
                raise Conflict("Caller UUID already identifies different options")
            return self.summary(call_uuid)
        spec, targets = snapshot(prepare())
        with self.box.transaction():
            self.record_in_transaction(call_uuid, request_sha256, {**spec, "targets": targets})
        return self.summary(call_uuid)

    def record_in_transaction(self, call_uuid, request_sha256, value):
        """Commit a prepared snapshot with its caller's prerequisite decision."""
        if not self.db.in_transaction:
            raise RuntimeError("Source snapshot requires the caller transaction")
        identifier(call_uuid)
        if not sha256(request_sha256):
            raise InvalidData("A caller requires a stable request digest")
        spec, targets = snapshot(value)
        previous = self._find(call_uuid)
        if previous is not None:
            if previous["request_sha256"] != request_sha256:
                raise Conflict("Caller UUID already identifies different options")
            return
        if (self.db.execute("SELECT count(*) FROM source_calls").fetchone()[0] >= self.max_calls
                or self.db.execute("SELECT count(*) FROM source_call_targets").fetchone()[0] + len(targets) > self.max_targets):
            raise Capacity("Caller capacity exhausted; existing snapshots are retained")
        now, body = self.box.clock(), encode(spec, 8192)
        self.db.execute("""INSERT INTO source_calls(uuid,request_sha256,definition,definition_sha256,
            operation,until_stamp,target_count,created_at) VALUES(?,?,?,?,?,?,?,?)""",
            (call_uuid, request_sha256, body, digest(body), spec["operation"], spec["window"]["until"], len(targets), now))
        self.db.executemany("""INSERT INTO source_call_targets(call_uuid,position,target_url,state,available_at)
            VALUES(?,?,?,'pending',?)""", [(call_uuid, index, target, now) for index, target in enumerate(targets, 1)])

    def summary(self, call_uuid=None):
        row = self._find(call_uuid) if call_uuid is not None else None
        if call_uuid is not None and row is None:
            raise InvalidData("Caller snapshot was not found")
        counts = {state: 0 for state in ("pending", "resolving", "queued", "review")}
        where, args = (" WHERE call_uuid=?", (call_uuid,)) if row is not None else ("", ())
        counts.update({r["state"]: r["total"] for r in self.db.execute(
            "SELECT state,count(*) AS total FROM source_call_targets" + where + " GROUP BY state", args)})
        if row is None:
            return {"calls": self.db.execute("SELECT count(*) FROM source_calls").fetchone()[0], "counts": counts}
        if sum(counts.values()) != row["target_count"]:
            raise InvalidData("Caller snapshot has missing source targets")
        state = next((s for s in ("review", "resolving", "pending") if counts[s]), "queued")
        return {"call_uuid": call_uuid, "request_sha256": row["request_sha256"], "definition": self._definition(row),
                "target_count": row["target_count"], "counts": counts, "state": state}

    def page(self, call_uuid, *, after=0, limit=50):
        identifier(call_uuid)
        if type(after) is not int or after < 0 or type(limit) is not int or not 1 <= limit <= 50:
            raise InvalidData("Invalid caller target page")
        rows = self.db.execute("""SELECT position,target_url,state,collection_uuid,collection_revision,ticket_uuid,
            intent_uuid,error_code FROM source_call_targets WHERE call_uuid=? AND position>? ORDER BY position LIMIT ?""",
            (call_uuid, after, limit)).fetchall()
        return [dict(row) for row in rows]

    def claim(self, owner, *, seconds=120):
        identifier(owner)
        if type(seconds) is not int or not 90 <= seconds <= 900:
            raise InvalidData("Invalid caller resolution lease")
        with self.box.transaction():
            now = self.box.clock()
            self.db.execute("""UPDATE source_call_targets SET state='pending',owner=NULL,lease_until=NULL,
                error_code='lookup_interrupted' WHERE state='resolving' AND lease_until<=?""", (now,))
            # Filter the active target index before ordering; completed caller
            # history cannot make lookup scheduling scan every retained row.
            first = self.db.execute("""SELECT t.call_uuid FROM source_call_targets t INDEXED BY ready_source_calls
                JOIN source_calls c ON c.uuid=t.call_uuid WHERE t.state='pending' AND t.available_at<=?
                ORDER BY c.operation,c.until_stamp DESC,c.seq,t.position LIMIT 1""", (now,)).fetchone()
            if first is None:
                return None
            row = self._find(first[0])
            spec = self._definition(row)
            targets = self.db.execute("""SELECT seq,position,target_url,fence,attempts FROM source_call_targets
                WHERE call_uuid=? AND state='pending' AND available_at<=? ORDER BY position LIMIT 50""", (first[0], now)).fetchall()
            result = []
            for target in targets:
                item = dict(target)
                item["fence"] += 1
                item["attempts"] += 1
                self.db.execute("""UPDATE source_call_targets SET state='resolving',owner=?,fence=?,attempts=?,lease_until=?
                    WHERE seq=?""", (owner, item["fence"], item["attempts"], now + seconds, item["seq"]))
                result.append(item)
            return Resolution(first[0], spec, owner, tuple(result))

    def _owned(self, delivery, item):
        row = self.db.execute("SELECT * FROM source_call_targets WHERE seq=?", (item["seq"],)).fetchone()
        if (row is None or row["state"] != "resolving" or row["call_uuid"] != delivery.call_uuid
                or row["owner"] != delivery.owner or row["fence"] != item["fence"] or row["lease_until"] <= self.box.clock()
                or row["position"] != item["position"] or row["target_url"] != item["target_url"]):
            raise LeaseLost("Caller resolution ownership expired; its original targets remain queued")

    def bind(self, delivery, item, match):
        if match["target_url"] != item["target_url"]:
            raise InvalidData("Collection lookup changed the caller's target")
        with self.box.transaction():
            self._owned(delivery, item)
            if match["state"] != "resolved":
                if match["state"] not in ("unresolved", "ambiguous", "disabled", "retired"):
                    raise InvalidData("Unknown collection lookup state")
                self.db.execute("""UPDATE source_call_targets SET state='review',owner=NULL,lease_until=NULL,error_code=?
                    WHERE seq=?""", ("collection_" + match["state"], item["seq"]))
                return "review"
            if match["has_more"] or len(match["candidates"]) != 1 or match["candidates"][0]["state"] != "active":
                raise InvalidData("Collection binding requires one active match")
            candidate = match["candidates"][0]
            ticket = str(uuid.uuid5(uuid.UUID(delivery.call_uuid), "source/" + str(item["position"])))
            spec = {key: value for key, value in delivery.definition.items() if key != "root_uuid"}
            spec.update(collection_uuid=candidate["collection_uuid"], collection_revision=candidate["collection_revision"])
            intent = RunQueue(self.box).enqueue_in_transaction(spec, ticket_uuid=ticket)
            self.db.execute("""UPDATE source_call_targets SET state='queued',owner=NULL,lease_until=NULL,
                collection_uuid=?,collection_revision=?,ticket_uuid=?,intent_uuid=?,error_code=NULL WHERE seq=?""",
                (candidate["collection_uuid"], candidate["collection_revision"], ticket, intent, item["seq"]))
            return "queued"

    def fail(self, delivery, code, *, review=False, retry_after=0):
        if (not isinstance(code, str) or not code or len(code) > 64
                or not all(c in "abcdefghijklmnopqrstuvwxyz0123456789_" for c in code)
                or type(retry_after) is not int or not 0 <= retry_after <= 86400):
            raise InvalidData("Invalid caller lookup failure")
        with self.box.transaction():
            changed = 0
            for item in delivery.targets:
                delay = min(86400, max(5 * 2 ** min(item["attempts"] - 1, 10), retry_after))
                changed += self.db.execute("""UPDATE source_call_targets SET state=?,owner=NULL,lease_until=NULL,
                    available_at=?,error_code=? WHERE seq=? AND call_uuid=? AND state='resolving' AND owner=?
                    AND fence=? AND lease_until>?""", ("review" if review else "pending", self.box.clock() + delay, code,
                    item["seq"], delivery.call_uuid, delivery.owner, item["fence"], self.box.clock())).rowcount
            return changed

    def retry(self, call_uuid):
        if self._find(call_uuid) is None:
            raise InvalidData("Caller snapshot was not found")
        with self.box.transaction():
            return self.db.execute("""UPDATE source_call_targets SET state='pending',available_at=?,error_code=NULL
                WHERE call_uuid=? AND state='review'""", (self.box.clock(), call_uuid)).rowcount


def resolve_once(calls, client, *, owner=None):
    if (calls.box.endpoint, calls.box.producer) != (client.endpoint, client.producer):
        raise Conflict("Caller queue and client identify different Stash producers")
    delivery = calls.claim(owner or str(uuid.uuid4()))
    if delivery is None:
        return {"state": "idle"}
    try:
        result = lookup_collections(client, [item["target_url"] for item in delivery.targets], delivery.definition["root_uuid"])
        counts = {"queued": 0, "review": 0}
        for item, match in zip(delivery.targets, result["targets"], strict=True):
            counts[calls.bind(delivery, item, match)] += 1
        return {"state": "review" if counts["review"] else "queued", "call_uuid": delivery.call_uuid, "counts": counts}
    except LeaseLost:
        return {"state": "lease_lost", "call_uuid": delivery.call_uuid}
    except Capacity:
        calls.fail(delivery, "caller_submission_capacity")
        raise
    except (Unavailable, InvalidData) as error:
        code, review, delay = "invalid_collection_binding", True, 0
        if isinstance(error, Unavailable):
            code, review, delay = error.code, error.status in {400, 403, 404, 409, 413, 422}, error.retry_after
        changed = calls.fail(delivery, code, review=review, retry_after=delay)
        return {"state": ("review" if review else "pending") if changed else "lease_lost",
                "call_uuid": delivery.call_uuid, "error_code": code}
