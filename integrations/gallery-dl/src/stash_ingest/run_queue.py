"""Coalesced offline requests; admission never asserts scrape completion."""

from dataclasses import dataclass
import uuid

from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier
from .events import sha256
from .outbox import Capacity, Conflict, LeaseLost
from . import windows
from . import tickets


def specification(value):
    fields = {"collection_uuid", "collection_revision", "operation", "policy_sha256", "cooldown_seconds", "window"}
    if not isinstance(value, dict) or set(value) != fields:
        raise InvalidData("Source requests accept only the typed collection, policy and window fields")
    identifier(value["collection_uuid"])
    if (type(value["collection_revision"]) is not int or not 1 <= value["collection_revision"] <= 2147483647
            or value["operation"] not in ("download", "enrich") or not sha256(value["policy_sha256"])
            or type(value["cooldown_seconds"]) is not int or not 0 <= value["cooldown_seconds"] <= 86400):
        raise InvalidData("Invalid source request definition")
    window = windows.normalize(value['window'])
    if window.get('basis') == 'traversal' and value['operation'] != 'download':
        raise InvalidData('Configured traversal currently requires a download operation')
    return {**value, "window": window}


@dataclass(frozen=True)
class Submission:
    request_uuid: str
    sha256: str
    body: bytes
    owner: str
    fence: int
    attempts: int


class RunQueue:
    def __init__(self, outbox, *, max_groups=10000, max_windows=windows.MAX_WINDOWS, max_tickets=100000):
        if (type(max_groups) is not int or not 1 <= max_groups <= 10000
                or type(max_windows) is not int or not 1 <= max_windows <= windows.MAX_WINDOWS
                or type(max_tickets) is not int or not 1 <= max_tickets <= 1000000):
            raise InvalidData("Invalid offline request capacity")
        self.box, self.db = outbox, outbox.db
        self.max_groups, self.max_windows = max_groups, max_windows
        self.max_tickets = max_tickets

    def _save_windows(self, intent, pending):
        if len(pending) > self.max_windows:
            raise Capacity("Too many disjoint source windows; existing requests remain queued")
        self.db.execute("""UPDATE run_intents SET
                        pending_since=CASE WHEN ?=0 THEN NULL WHEN window_count=0 THEN ? ELSE pending_since END,
                        windows=?,window_count=?,latest_until=?,updated_at=? WHERE uuid=?""",
                        (len(pending), self.box.clock(), encode(pending, 16384), len(pending), max((w['until'] for w in pending), default=''),
                         self.box.clock(), intent))

    def enqueue(self, value, *, ticket_uuid=None):
        with self.box.transaction():
            return self.enqueue_in_transaction(value, ticket_uuid=ticket_uuid)

    def enqueue_in_transaction(self, value, *, ticket_uuid=None):
        if not self.db.in_transaction:
            raise RuntimeError("Source admission requires the caller transaction")
        spec = specification(value)
        if ticket_uuid is not None:
            identifier(ticket_uuid)
        ticket_digest = digest(encode(spec, 8192))
        template = {k: v for k, v in spec.items() if k != "window"}
        body = encode(template, 8192)
        basis = spec['window'].get('basis')
        key = digest(encode([template, basis], 8192)) if basis else digest(body)
        if ticket_uuid is not None:
            previous = self.db.execute("SELECT * FROM run_intent_tickets WHERE uuid=?", (ticket_uuid,)).fetchone()
            if previous is not None:
                if previous["sha256"] != ticket_digest:
                    raise Conflict("Caller ticket already identifies different source work")
                return previous["intent_uuid"]
            if self.db.execute("SELECT count(*) FROM run_intent_tickets").fetchone()[0] >= self.max_tickets:
                raise Capacity("Caller ticket capacity exhausted; existing receipts are retained")
        group = self.db.execute("SELECT * FROM run_intents WHERE config_sha256=?", (key,)).fetchone()
        if group:
            if group["template"] != body:
                raise Conflict("Source request configuration digest conflicts")
            intent, pending = group["uuid"], decode(group["windows"], 16384)
        else:
            if self.db.execute("SELECT count(*) FROM run_intents").fetchone()[0] >= self.max_groups:
                raise Capacity("Offline source request configuration capacity exhausted")
            intent, pending, now = str(uuid.uuid4()), [], self.box.clock()
            self.db.execute("""INSERT INTO run_intents(uuid,config_sha256,template,operation,windows,
                window_count,latest_until,created_at,updated_at) VALUES(?,?,?,?,?,0,'',?,?)""",
                (intent, key, body, spec["operation"], b"[]", now, now))
        frozen = self.db.execute("SELECT * FROM run_requests WHERE intent_uuid=? AND state!='admitted'", (intent,)).fetchone()
        pending = windows.union(pending, [spec["window"]])
        if frozen:
            pending = windows.subtract(pending, [decode(frozen["window"], 8192)])
        self._save_windows(intent, pending)
        if ticket_uuid is not None:
            first = frozen["seq"] if frozen else self.db.execute("SELECT coalesce(max(seq),0)+1 FROM run_requests").fetchone()[0]
            self.db.execute("""INSERT INTO run_intent_tickets(uuid,intent_uuid,sha256,window,first_sequence,created_at,unassigned,unassigned_count)
                VALUES(?,?,?,?,?,?,?,1)""", (ticket_uuid, intent, ticket_digest, encode(spec["window"]), first, self.box.clock(),
                                          encode([spec["window"]], 16384)))
            if frozen:
                tickets.attach(self.db, self.db.execute("SELECT * FROM run_intent_tickets WHERE uuid=?", (ticket_uuid,)).fetchone(), frozen)
        return intent

    def claim(self, owner, *, seconds=120):
        identifier(owner)
        if type(seconds) is not int or not 30 <= seconds <= 900:
            raise InvalidData("Invalid source submission lease")
        with self.box.transaction():
            now = self.box.clock()
            self.db.execute("""UPDATE run_requests SET state='pending',owner=NULL,lease_until=NULL,
                error_code='submission_interrupted' WHERE state='sending' AND lease_until<=?""", (now,))
            candidate = self.db.execute("""SELECT 'request' AS kind,r.request_uuid AS value,
                    CASE g.operation WHEN 'download' THEN 0 ELSE 1 END AS priority,r.until_stamp AS until_stamp
                FROM run_requests r JOIN run_intents g ON g.uuid=r.intent_uuid
                WHERE r.state='pending' AND r.available_at<=?
                UNION ALL
                SELECT 'intent',g.uuid,CASE g.operation WHEN 'download' THEN 0 ELSE 1 END,g.latest_until
                FROM run_intents g WHERE g.window_count>0 AND NOT EXISTS(
                    SELECT 1 FROM run_requests r WHERE r.intent_uuid=g.uuid AND r.state!='admitted')
                ORDER BY priority,until_stamp DESC,value LIMIT 1""", (now,)).fetchone()
            if candidate is None:
                return None
            if candidate["kind"] == "intent":
                group = self.db.execute("SELECT * FROM run_intents WHERE uuid=?", (candidate["value"],)).fetchone()
                pending = decode(group["windows"], 16384)
                window = pending.pop()
                request_id = str(uuid.uuid4())
                body = encode({**decode(group["template"], 8192), "request_uuid": request_id, "window": window}, 8192)
                self.db.execute("""INSERT INTO run_requests(request_uuid,intent_uuid,sha256,window,until_stamp,
                    body,state,available_at,created_at) VALUES(?,?,?,?,?,?,'pending',?,?)""",
                    (request_id, group["uuid"], digest(body), encode(window), window["until"], body, now, group["pending_since"]))
                self._save_windows(group["uuid"], pending)
                tickets.assign_request(self.db, request_id)
            else:
                request_id = candidate["value"]
            row = self.db.execute("SELECT * FROM run_requests WHERE request_uuid=?", (request_id,)).fetchone()
            fence = row["fence"] + 1
            self.db.execute("""UPDATE run_requests SET state='sending',owner=?,fence=?,lease_until=?,
                attempts=attempts+1 WHERE request_uuid=?""", (owner, fence, now + seconds, request_id))
            return Submission(request_id, row["sha256"], row["body"], owner, fence, row["attempts"] + 1)

    def _owned(self, delivery):
        row = self.db.execute("SELECT * FROM run_requests WHERE request_uuid=?", (delivery.request_uuid,)).fetchone()
        if (row is None or row["state"] != "sending" or row["owner"] != delivery.owner
                or row["fence"] != delivery.fence or row["lease_until"] <= self.box.clock()
                or row["sha256"] != delivery.sha256):
            raise LeaseLost("Source submission ownership expired; its immutable request remains queued")
        return row

    def admit(self, delivery, receipt):
        raw = encode(receipt, 65536)
        with self.box.transaction():
            row = self._owned(delivery)
            request = decode(row["body"], 8192)
            if (not isinstance(receipt, dict) or receipt.get("request_uuid") != delivery.request_uuid
                    or any(receipt.get(k) != request[k] for k in (
                        "collection_uuid", "collection_revision", "operation", "policy_sha256", "cooldown_seconds"))):
                raise Conflict("Run admission does not acknowledge this source request")
            run_id = identifier(receipt.get("uuid"))
            if request["operation"] == "download":
                identifier(receipt.get("root_uuid"))
            if receipt.get("state") not in ("queued", "running", "succeeded", "deferred", "cancelled"):
                raise InvalidData("Run admission has no valid server state")
            self.db.execute("""UPDATE run_requests SET state='admitted',body=NULL,receipt=?,run_uuid=?,
                owner=NULL,lease_until=NULL,admitted_at=?,error_code=NULL WHERE request_uuid=?""",
                (raw, run_id, self.box.clock(), delivery.request_uuid))

    def fail(self, delivery, code, *, review=False, retry_after=0):
        if (not isinstance(code, str) or not code or len(code) > 64
                or not all(c in "abcdefghijklmnopqrstuvwxyz0123456789_" for c in code)
                or type(retry_after) is not int or retry_after < 0):
            raise InvalidData("Invalid source submission failure")
        with self.box.transaction():
            self._owned(delivery)
            delay = min(86400, max(5 * 2 ** min(delivery.attempts - 1, 15), retry_after))
            self.db.execute("""UPDATE run_requests SET state=?,owner=NULL,lease_until=NULL,
                available_at=?,error_code=? WHERE request_uuid=?""",
                ("review" if review else "pending", self.box.clock() + delay, code, delivery.request_uuid))

    def retry(self, request_uuid):
        identifier(request_uuid)
        with self.box.transaction():
            count = self.db.execute("""UPDATE run_requests SET state='pending',available_at=?,error_code=NULL
                WHERE request_uuid=? AND state='review'""", (self.box.clock(), request_uuid)).rowcount
            if count != 1:
                raise Conflict("Only a reviewed source submission can be retried")

    def status(self):
        groups = self.db.execute("""SELECT count(*) AS configurations,coalesce(sum(window_count),0) AS pending_windows,
            min(pending_since) AS oldest FROM run_intents""").fetchone()
        rows = self.db.execute("""SELECT state,count(*) AS count,min(created_at) AS oldest,
            min(available_at) AS available FROM run_requests GROUP BY state""").fetchall()
        counts = {state: 0 for state in ("pending", "sending", "admitted", "review")}
        counts.update({row["state"]: row["count"] for row in rows})
        ages = [r["oldest"] for r in rows if r["state"] != "admitted"]
        if groups["oldest"] is not None:
            ages.append(groups["oldest"])
        return {"counts": counts, "configurations": groups["configurations"],
                "caller_tickets": self.db.execute("SELECT count(*) FROM run_intent_tickets").fetchone()[0],
                "pending_windows": groups["pending_windows"],
                "oldest_queued_seconds": max(0, self.box.clock() - min(ages)) if ages else None,
                "next_attempt_at": min((r["available"] for r in rows if r["state"] == "pending"), default=None)}

    def history(self, intent_uuid, *, after=0, limit=50):
        identifier(intent_uuid)
        if type(after) is not int or after < 0 or type(limit) is not int or not 1 <= limit <= 50:
            raise InvalidData("Invalid source submission page")
        rows = self.db.execute("""SELECT seq,request_uuid,state,run_uuid,window,error_code,attempts,available_at
            FROM run_requests WHERE intent_uuid=? AND seq>? ORDER BY seq LIMIT ?""", (intent_uuid, after, limit)).fetchall()
        return [{**dict(row), "window": decode(row["window"], 8192)} for row in rows]

    def ticket(self, ticket_uuid):
        identifier(ticket_uuid)
        row = self.db.execute("SELECT uuid,intent_uuid,window,first_sequence FROM run_intent_tickets WHERE uuid=?", (ticket_uuid,)).fetchone()
        if row is None:
            return None
        return {**dict(row), "window": decode(row["window"], 8192)}


def submit_once(queue, client, *, owner=None):
    if (queue.box.endpoint, queue.box.producer) != (client.endpoint, client.producer):
        raise Conflict("Source queue and client identify different Stash producers")
    delivery = queue.claim(owner or str(uuid.uuid4()))
    if delivery is None:
        return {"state": "idle"}
    try:
        if digest(delivery.body) != delivery.sha256:
            queue.fail(delivery, "request_integrity_failed", review=True)
            return {"state": "review", "request_uuid": delivery.request_uuid, "error_code": "request_integrity_failed"}
        capabilities = client.capabilities()
        if (capabilities.get("source_runs") is not True or capabilities.get("source_run_protocol") != 1
                or capabilities.get("source_run_submission_receipts") is not True):
            raise Unavailable("incompatible_source_runs")
        if (decode(delivery.body)['window'].get('basis') == 'traversal'
                and capabilities.get('source_run_traversal_protocol') != 1):
            raise Unavailable('native_source_traversal_unavailable')
        receipt = client._request("POST", "/runs", delivery.body)
        queue.admit(delivery, receipt)
        return {"state": "admitted", "request_uuid": delivery.request_uuid, "run_uuid": receipt["uuid"]}
    except LeaseLost:
        return {"state": "lease_lost", "request_uuid": delivery.request_uuid}
    except (Unavailable, InvalidData) as exc:
        if isinstance(exc, Unavailable):
            code, review, delay = exc.code, exc.status in {400, 403, 404, 409, 413, 422}, exc.retry_after
        else:
            code, review, delay = "run_receipt_mismatch", True, 0
        try:
            queue.fail(delivery, code, review=review, retry_after=delay)
            return {"state": "review" if review else "pending", "request_uuid": delivery.request_uuid, "error_code": code}
        except LeaseLost:
            return {"state": "lease_lost", "request_uuid": delivery.request_uuid}
