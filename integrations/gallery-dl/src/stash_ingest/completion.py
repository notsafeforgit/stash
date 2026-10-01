"""Check a caller's exact source windows, independently of media intake."""

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


def validate_run(value, assignment, requested):
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


def inspect_ticket(box, client, ticket_uuid):
    if (box.endpoint, box.producer) != (client.endpoint, client.producer):
        raise Conflict("Caller ticket and client identify different Stash producers")
    requested, unassigned, assignments = ticket_snapshot(box, ticket_uuid)
    remaining, parts, cache = list(unassigned), [], {}
    capabilities = None
    for assignment in assignments:
        part = {"request_uuid": assignment["request_uuid"], "run_uuid": assignment["run_uuid"],
                "state": assignment["state"], "remaining": assignment["assigned"]}
        if assignment["state"] != "admitted":
            if assignment["error_code"]:
                part["error_code"] = assignment["error_code"]
        else:
            try:
                if capabilities is None:
                    try:
                        found = client.capabilities()
                        if found.get("source_runs") is not True or found.get("source_run_protocol") != 1:
                            raise Unavailable("incompatible_source_runs")
                        capabilities = True
                    except Unavailable as error:
                        capabilities = error
                if isinstance(capabilities, Unavailable):
                    raise capabilities
                run_uuid = assignment["run_uuid"]
                if run_uuid not in cache:
                    try:
                        cache[run_uuid] = client._request("GET", "/runs/" + identifier(run_uuid))
                    except Unavailable as error:
                        cache[run_uuid] = error
                current = cache[run_uuid]
                if isinstance(current, Unavailable):
                    raise current
                completed = validate_run(current, assignment, requested)
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
