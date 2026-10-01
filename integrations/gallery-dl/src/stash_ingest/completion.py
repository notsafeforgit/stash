"""Check a caller's exact source windows, independently of media intake."""

from collections import Counter, OrderedDict
import uuid

from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier
from .outbox import Conflict
from .run_queue import specification
from .tickets import assigned_windows, intersection
from . import windows


def ticket_snapshot(box, ticket_uuid):
    identifier(ticket_uuid)
    with box.transaction():
        ticket = box.db.execute("""SELECT t.*,g.template FROM run_intent_tickets t JOIN run_intents g ON g.uuid=t.intent_uuid
            WHERE t.uuid=?""", (ticket_uuid,)).fetchone()
        if ticket is None:
            raise InvalidData("Caller ticket was not found")
        requested = specification({**decode(ticket["template"], 8192), "window": decode(ticket["window"], 8192)})
        if digest(encode(requested, 8192)) != ticket["sha256"]:
            raise InvalidData("Caller ticket contents differ from their recorded digest")
        pending = assigned_windows(ticket["unassigned"])
        if len(pending) != ticket["unassigned_count"]:
            raise InvalidData("Caller ticket has inconsistent pending coverage")
        rows = box.db.execute("""SELECT r.request_uuid,r.intent_uuid,r.window,r.state,r.run_uuid,r.receipt,
            r.error_code,link.windows FROM run_ticket_requests link JOIN run_requests r USING(request_uuid)
            WHERE link.ticket_uuid=? ORDER BY r.seq LIMIT 65""", (ticket_uuid,)).fetchall()
        if len(rows) > windows.MAX_WINDOWS:
            raise InvalidData("Caller ticket exceeds its source submission limit")
        covered, assignments = pending, []
        for row in rows:
            assigned = assigned_windows(row["windows"])
            request_window = windows.normalize(decode(row["window"], 8192))
            if (row["intent_uuid"] != ticket["intent_uuid"] or not assigned
                    or intersection(covered, assigned) or windows.subtract(assigned, [request_window])):
                raise InvalidData("Caller ticket has inconsistent submission assignments")
            covered = windows.union(covered, assigned)
            assignments.append({**dict(row), "assigned": assigned, "request_window": request_window})
        if covered != [requested["window"]]:
            raise InvalidData("Caller ticket assignments do not cover its original window")
    return requested, pending, assignments


def validate_run(value, assignment, requested, expected=None):
    receipt = decode(assignment["receipt"], 65536)
    fields = ("collection_uuid", "collection_revision", "operation", "policy_sha256", "cooldown_seconds")
    if (not isinstance(receipt, dict) or receipt.get("request_uuid") != assignment["request_uuid"]
            or receipt.get("uuid") != assignment["run_uuid"] or not isinstance(value, dict)
            or value.get("uuid") != assignment["run_uuid"]):
        raise InvalidData("Source status does not identify the admitted run")
    identifier(value["uuid"])
    for field in fields:
        if any(type(record.get(field)) is not type(requested[field]) or record[field] != requested[field]
               for record in (receipt, value)):
            raise InvalidData("Source status does not match this caller's definition")
    root = receipt.get("root_uuid")
    if expected is not None and root != expected["root_uuid"]:
        raise InvalidData("Caller admission changed its original root")
    if root is not None or requested["operation"] == "download":
        identifier(root)
    if value.get("root_uuid") != root:
        raise InvalidData("Source status changed the admitted media root")
    if value.get("state") not in ("queued", "running", "succeeded", "deferred", "cancelled"):
        raise InvalidData("Unknown source execution state")
    completed = value.get("completed")
    if not isinstance(completed, list) or len(completed) > windows.MAX_WINDOWS:
        raise InvalidData("Source status lacks bounded completed windows")
    return windows.union(completed)


class _RunStatuses:
    """One inspection shares bounded current reads, never a persisted success cache."""

    def __init__(self, client):
        self.client, self.capabilities, self.cache = client, None, OrderedDict()

    def get(self, run_uuid):
        if self.capabilities is None:
            try:
                found = self.client.capabilities()
                if found.get("source_runs") is not True or found.get("source_run_protocol") != 1:
                    raise Unavailable("incompatible_source_runs")
                self.capabilities = True
            except Unavailable as error:
                self.capabilities = error
        if isinstance(self.capabilities, Unavailable):
            raise self.capabilities
        if run_uuid not in self.cache:
            try:
                self.cache[run_uuid] = self.client._request("GET", "/runs/" + identifier(run_uuid))
            except Unavailable as error:
                self.cache[run_uuid] = error
            if len(self.cache) > 256:
                self.cache.popitem(last=False)
        current = self.cache[run_uuid]
        self.cache.move_to_end(run_uuid)
        if isinstance(current, Unavailable):
            raise current
        return current


def inspect_ticket(box, client, ticket_uuid, *, _statuses=None, _expected=None):
    if (box.endpoint, box.producer) != (client.endpoint, client.producer):
        raise Conflict("Caller ticket and client identify different Stash producers")
    requested, unassigned, assignments = ticket_snapshot(box, ticket_uuid)
    if _expected is not None and requested != {key: value for key, value in _expected.items() if key != "root_uuid"}:
        raise InvalidData("Caller ticket differs from its frozen source binding")
    statuses = _statuses or _RunStatuses(client)
    if statuses.client is not client:
        raise Conflict("Source status reader identifies a different Stash client")
    remaining, parts = list(unassigned), []
    for assignment in assignments:
        part = {"request_uuid": assignment["request_uuid"], "run_uuid": assignment["run_uuid"],
                "state": assignment["state"], "remaining": assignment["assigned"]}
        if assignment["state"] != "admitted":
            if assignment["error_code"]:
                part["error_code"] = assignment["error_code"]
        else:
            try:
                current = statuses.get(assignment["run_uuid"])
                completed = validate_run(current, assignment, requested, _expected)
                part["remaining"] = windows.subtract(assignment["assigned"], completed)
                if not part["remaining"]:
                    part["state"] = "source_succeeded"
                elif current["state"] == "succeeded":
                    part.update(state="review", error_code="source_completion_mismatch")
                else:
                    part["state"] = current["state"]
            except Unavailable as error:
                part.update(state="unavailable", error_code=error.code)
            except InvalidData:
                part.update(state="review", error_code="source_status_mismatch")
        remaining = windows.union(remaining, part["remaining"])
        parts.append(part)
    if not remaining:
        state = "source_succeeded"
    else:
        states = {part["state"] for part in parts if part["remaining"]}
        state = next((state for state in ("review", "cancelled", "deferred", "unavailable", "running", "queued", "sending", "pending")
                      if state in states), "recorded")
    return {"ticket_uuid": ticket_uuid, "state": state, "operation": requested["operation"],
            "requested": requested["window"], "unassigned": unassigned, "remaining": remaining,
            "submissions": parts, "intake_completion": "inspect_native_receipts"}


def inspect_call(calls, client, call_uuid):
    if (calls.box.endpoint, calls.box.producer) != (client.endpoint, client.producer):
        raise Conflict("Caller queue and client identify different Stash producers")
    summary = calls.summary(call_uuid)
    statuses, counts, issues = _RunStatuses(client), Counter(), []
    after = 0
    while page := calls.page(call_uuid, after=after):
        for item in page:
            if item["position"] != after + 1:
                raise InvalidData("Caller snapshot has missing source positions")
            after = item["position"]
            state, code = item["state"], item["error_code"]
            if state == "queued":
                try:
                    if item["ticket_uuid"] != str(uuid.uuid5(uuid.UUID(call_uuid), "source/" + str(after))):
                        raise InvalidData("Caller target has a different ticket identity")
                    expected = {**summary["definition"], "collection_uuid": item["collection_uuid"],
                                "collection_revision": item["collection_revision"]}
                    result = inspect_ticket(calls.box, client, item["ticket_uuid"], _statuses=statuses, _expected=expected)
                    state = result["state"]
                    code = next((part["error_code"] for part in result["submissions"] if part.get("error_code")), None)
                except InvalidData:
                    state, code = "review", "caller_binding_mismatch"
            counts[state] += 1
            if state != "source_succeeded" and len(issues) < 20:
                issues.append({"position": after, "target_url": item["target_url"], "state": state, "error_code": code})
    if after != summary["target_count"]:
        raise InvalidData("Caller snapshot has missing source targets")
    state = next((s for s in ("review", "cancelled", "deferred", "unavailable", "running", "resolving",
                             "queued", "sending", "pending", "recorded") if counts[s]), "source_succeeded")
    return {"call_uuid": call_uuid, "state": state, "requested": summary["definition"]["window"],
            "target_count": after, "counts": dict(counts), "issues": issues,
            "issues_truncated": after - counts["source_succeeded"] > len(issues),
            "intake_completion": "inspect_native_receipts"}
