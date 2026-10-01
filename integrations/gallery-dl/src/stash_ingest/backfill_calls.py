"""Durable n8n calls: check retained history before admitting source work."""

from dataclasses import dataclass
import uuid

from . import backfills
from .client import Unavailable
from .completion import inspect_call
from .encoding import InvalidData, decode, digest, encode, identifier
from .events import sha256
from .outbox import Capacity, Conflict, LeaseLost
from .source_calls import SourceCalls


def migrate(db):
    db.execute("""CREATE TABLE backfill_calls(
        seq INTEGER PRIMARY KEY, uuid TEXT NOT NULL UNIQUE, request_sha256 TEXT NOT NULL,
        definition BLOB NOT NULL, definition_sha256 TEXT NOT NULL,
        state TEXT NOT NULL CHECK(state IN ('pending','active','completed','skipped','review')),
        guard BLOB, source_uuid TEXT UNIQUE REFERENCES source_calls(uuid),
        completion BLOB, completion_sha256 TEXT, receipt BLOB,
        owner TEXT, fence INTEGER NOT NULL DEFAULT 0, lease_until REAL,
        attempts INTEGER NOT NULL DEFAULT 0, available_at REAL NOT NULL,
        error_code TEXT, created_at REAL NOT NULL,
        CHECK((owner IS NULL)=(lease_until IS NULL)),
        CHECK((completion IS NULL)=(completion_sha256 IS NULL)),
        CHECK(state IN ('pending','review') OR guard IS NOT NULL),
        CHECK(state NOT IN ('completed','skipped') OR (receipt IS NOT NULL AND owner IS NULL)),
        CHECK(source_uuid IS NULL OR guard IS NOT NULL),
        CHECK(completion IS NULL OR source_uuid IS NOT NULL)
    )""")
    db.execute("CREATE INDEX ready_backfill_calls ON backfill_calls(state,available_at,seq)")
    db.execute("""CREATE TRIGGER backfill_call_immutable BEFORE UPDATE OF uuid,request_sha256,definition,definition_sha256 ON backfill_calls
        BEGIN SELECT RAISE(ABORT,'backfill snapshots are immutable'); END""")
    for column in ("guard", "source_uuid", "completion", "completion_sha256", "receipt"):
        db.execute(f"""CREATE TRIGGER backfill_call_{column}_immutable BEFORE UPDATE OF {column} ON backfill_calls
            WHEN OLD.{column} IS NOT NULL AND NEW.{column} IS NOT OLD.{column}
            BEGIN SELECT RAISE(ABORT,'backfill evidence is immutable'); END""")
    db.execute("""CREATE TRIGGER backfill_call_finished BEFORE UPDATE ON backfill_calls WHEN OLD.state IN ('completed','skipped')
        BEGIN SELECT RAISE(ABORT,'finished backfill calls are immutable'); END""")


@dataclass(frozen=True)
class BackfillDelivery:
    uuid: str
    definition: dict
    owner: str
    fence: int
    attempts: int


class BackfillCalls:
    def __init__(self, box, *, max_calls=10000):
        if type(max_calls) is not int or not 1 <= max_calls <= 1000000:
            raise InvalidData("Invalid backfill caller capacity")
        self.box, self.db, self.max_calls = box, box.db, max_calls

    def _find(self, call_uuid):
        identifier(call_uuid)
        return self.db.execute("SELECT * FROM backfill_calls WHERE uuid=?", (call_uuid,)).fetchone()

    def _definition(self, row):
        if digest(row["definition"]) != row["definition_sha256"]:
            raise InvalidData("Backfill definition differs from its recorded digest")
        return backfills.definition(decode(row["definition"], 8192))

    def record(self, call_uuid, request_sha256, prepare):
        identifier(call_uuid)
        if not sha256(request_sha256):
            raise InvalidData("Backfill caller requires a stable request digest")
        prior = self._find(call_uuid)
        if prior is not None:
            if prior["request_sha256"] != request_sha256:
                raise Conflict("Backfill caller already identifies different options")
            return self.result(call_uuid)
        body = encode(backfills.definition(prepare()), 8192)
        with self.box.transaction():
            prior = self._find(call_uuid)
            if prior is not None:
                if prior["request_sha256"] != request_sha256:
                    raise Conflict("Backfill caller already identifies different options")
            else:
                if self.db.execute("SELECT count(*) FROM backfill_calls").fetchone()[0] >= self.max_calls:
                    raise Capacity("Backfill caller capacity exhausted; retained decisions were not removed")
                now = self.box.clock()
                self.db.execute("""INSERT INTO backfill_calls(uuid,request_sha256,definition,definition_sha256,state,available_at,created_at)
                    VALUES(?,?,?,?,'pending',?,?)""", (call_uuid, request_sha256, body, digest(body), now, now))
        return self.result(call_uuid)

    def result(self, call_uuid):
        row = self._find(call_uuid)
        if row is None:
            raise InvalidData("Backfill caller was not found")
        spec = self._definition(row)
        receipt = decode(row["receipt"], 32768) if row["receipt"] else None
        status = backfills.validate_status(receipt["status"], spec) if receipt else None
        failed, pending = row["state"] == "review", row["state"] in ("pending", "active")
        return {"token": uuid.UUID(call_uuid).hex, "call_uuid": call_uuid, "source_call_uuid": row["source_uuid"],
                "state": row["state"], "backfill_pending": pending, "command_failed": failed,
                "exit_code": 1 if failed else 2 if pending else 0, "network_blocked": False,
                "stdout_tail": "Backfill is pending." if pending else "Backfill requires review." if failed else
                               "Backfill deliberately skipped." if row["state"] == "skipped" else "Backfill completed or previously accepted.",
                "stderr_tail": row["error_code"] or "", "backfill_cached": status is not None and row["source_uuid"] is None,
                "account_backfill_complete": status["account_complete"] if status else False,
                "decisions": status["decisions"] if status else [], "next_attempt_at": row["available_at"] if pending else None,
                "intake_completion": "inspect_native_receipts"}

    def summary(self):
        counts = {state: 0 for state in ("pending", "active", "completed", "skipped", "review")}
        counts.update({row["state"]: row["total"] for row in self.db.execute(
            "SELECT state,count(*) AS total FROM backfill_calls GROUP BY state")})
        return {"calls": sum(counts.values()), "counts": counts}

    def claim(self, owner, call_uuid=None, *, seconds=120):
        identifier(owner)
        if call_uuid is not None:
            identifier(call_uuid)
        if type(seconds) is not int or not 90 <= seconds <= 900:
            raise InvalidData("Invalid backfill caller lease")
        with self.box.transaction():
            now = self.box.clock()
            where, args = (" AND uuid=?", (call_uuid,)) if call_uuid else ("", ())
            row = self.db.execute("""SELECT * FROM backfill_calls WHERE state IN ('pending','active')
                AND available_at<=? AND (lease_until IS NULL OR lease_until<=?)""" + where + " ORDER BY seq LIMIT 1",
                (now, now, *args)).fetchone()
            if row is None:
                return None
            spec = self._definition(row)
            self.db.execute("UPDATE backfill_calls SET owner=?,fence=fence+1,attempts=attempts+1,lease_until=? WHERE uuid=?",
                            (owner, now + seconds, row["uuid"]))
            return BackfillDelivery(row["uuid"], spec, owner, row["fence"] + 1, row["attempts"] + 1)

    def _owned(self, delivery):
        row = self._find(delivery.uuid)
        if (row is None or row["state"] not in ("pending", "active") or row["owner"] != delivery.owner
                or row["fence"] != delivery.fence or row["lease_until"] <= self.box.clock()):
            raise LeaseLost("Backfill caller ownership expired; its original work remains retained")
        return row

    def guard(self, delivery, status):
        status = backfills.validate_status(status, delivery.definition)
        body = encode(status, 16384)
        with self.box.transaction():
            row = self._owned(delivery)
            if row["guard"] is not None:
                raise Conflict("Backfill caller already has its historical decision")
            child, receipt, state = None, None, status["state"]
            if state == "needed":
                child, state = str(uuid.uuid5(uuid.UUID(delivery.uuid), "sources")), "active"
                snapshot = backfills.source_snapshot(delivery.definition)
                SourceCalls(self.box).record_in_transaction(child, digest(encode(snapshot, 8192)), snapshot)
            else:
                receipt = encode({"status": status}, 32768)
            self.db.execute("""UPDATE backfill_calls SET guard=?,source_uuid=?,receipt=?,state=?,owner=NULL,lease_until=NULL,
                available_at=?,error_code=NULL WHERE uuid=?""", (body, child, receipt, state, self.box.clock(), delivery.uuid))

    def proof(self, delivery):
        row = self._owned(delivery)
        if row["completion"] is not None:
            if digest(row["completion"]) != row["completion_sha256"]:
                raise InvalidData("Backfill completion proof differs from its recorded digest")
            return row["completion"]
        return None

    def save_proof(self, delivery, proof):
        body = encode(proof, 1 << 20)
        with self.box.transaction():
            row = self._owned(delivery)
            if row["completion"] is not None and row["completion"] != body:
                raise Conflict("Backfill completion proof cannot change after submission")
            self.db.execute("UPDATE backfill_calls SET completion=?,completion_sha256=? WHERE uuid=?",
                            (body, digest(body), delivery.uuid))
        return body

    def finish(self, delivery, decision, status):
        if status["state"] != "completed":
            raise InvalidData("Backfill completion is missing from native history")
        body = encode({"decision": decision, "status": status}, 32768)
        with self.box.transaction():
            row = self._owned(delivery)
            if row["completion"] is None:
                raise InvalidData("Backfill caller has no submitted completion proof")
            self.db.execute("""UPDATE backfill_calls SET receipt=?,state='completed',owner=NULL,lease_until=NULL,error_code=NULL
                WHERE uuid=?""", (body, delivery.uuid))

    def release(self, delivery, code=None, *, review=False, seconds=30):
        if (code is not None and (not isinstance(code, str) or not 1 <= len(code) <= 64
                                 or not all(c in "abcdefghijklmnopqrstuvwxyz0123456789_" for c in code))):
            raise InvalidData("Invalid backfill failure code")
        with self.box.transaction():
            row = self._owned(delivery)
            delay = min(86400, max(seconds, 5 * 2 ** min(delivery.attempts - 1, 10) if code else 0))
            self.db.execute("""UPDATE backfill_calls SET state=?,owner=NULL,lease_until=NULL,available_at=?,error_code=? WHERE uuid=?""",
                            ("review" if review else row["state"], self.box.clock() + delay, code, delivery.uuid))

    def retry(self, call_uuid):
        identifier(call_uuid)
        with self.box.transaction():
            changed = self.db.execute("""UPDATE backfill_calls SET state=CASE WHEN source_uuid IS NULL THEN 'pending' ELSE 'active' END,
                available_at=?,error_code=NULL WHERE uuid=? AND state='review'""", (self.box.clock(), call_uuid)).rowcount
            if not changed:
                raise Conflict("Only a reviewed backfill caller can be retried")


def advance_once(calls, client, call_uuid=None, *, owner=None):
    if (calls.box.endpoint, calls.box.producer) != (client.endpoint, client.producer):
        raise Conflict("Backfill queue and client identify different Stash producers")
    delivery = calls.claim(owner or str(uuid.uuid4()), call_uuid)
    if delivery is None:
        return {"state": "idle"}
    try:
        backfills.check_protocol(client)
        row = calls._owned(delivery)
        if row["guard"] is None:
            calls.guard(delivery, backfills.status(client, delivery.definition))
        else:
            body = calls.proof(delivery)
            if body is None:
                result = inspect_call(SourceCalls(calls.box), client, row["source_uuid"])
                if result["state"] != "source_succeeded":
                    code = next((item["error_code"] for item in result["issues"] if item.get("error_code")), None)
                    review = result["state"] in ("review", "cancelled")
                    calls.release(delivery, code or ("source_" + result["state"] if review else None), review=review)
                    return calls.result(delivery.uuid)
                proof = backfills.completion_proof(calls.box, delivery.uuid, row["source_uuid"], delivery.definition)
                body = calls.save_proof(delivery, proof)
            decision = backfills.complete(client, body, delivery.definition)
            status = backfills.status(client, delivery.definition)
            calls.finish(delivery, decision, status)
    except LeaseLost:
        return {"state": "lease_lost", "call_uuid": delivery.uuid}
    except (Unavailable, InvalidData, Capacity) as error:
        code, review, delay = "invalid_backfill_response", True, 30
        if isinstance(error, Unavailable):
            code, review, delay = error.code, error.status in {400, 403, 404, 409, 413, 422}, error.retry_after
        elif isinstance(error, Capacity):
            code, review = "caller_submission_capacity", False
        try:
            calls.release(delivery, code, review=review, seconds=delay)
        except LeaseLost:
            return {"state": "lease_lost", "call_uuid": delivery.uuid}
    return calls.result(delivery.uuid)
