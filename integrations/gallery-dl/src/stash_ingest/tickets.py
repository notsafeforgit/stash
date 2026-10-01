"""Stable caller-ticket assignments to immutable source submissions."""

from .encoding import InvalidData, decode, encode
from . import windows


def intersection(left, right):
    return windows.subtract(left, windows.subtract(left, right))


def assigned_windows(raw):
    value = decode(raw, 16384)
    if not isinstance(value, list) or len(value) > windows.MAX_WINDOWS:
        raise InvalidData("Invalid caller-ticket coverage")
    return windows.union(value)


def attach(db, ticket, request):
    """The caller owns the queue transaction; never link later replacement work."""
    if ticket["intent_uuid"] != request["intent_uuid"] or request["seq"] < ticket["first_sequence"]:
        raise InvalidData("Source request is outside this caller ticket")
    pending = assigned_windows(ticket["unassigned"])
    covered = intersection(pending, [decode(request["window"], 8192)])
    if not covered:
        return
    remaining = windows.subtract(pending, covered)
    db.execute("INSERT INTO run_ticket_requests(ticket_uuid,request_uuid,windows) VALUES(?,?,?)",
               (ticket["uuid"], request["request_uuid"], encode(covered, 16384)))
    db.execute("UPDATE run_intent_tickets SET unassigned=?,unassigned_count=? WHERE uuid=?",
               (encode(remaining, 16384), len(remaining), ticket["uuid"]))


def assign_request(db, request_uuid):
    request = db.execute("SELECT seq,request_uuid,intent_uuid,window FROM run_requests WHERE request_uuid=?",
                         (request_uuid,)).fetchone()
    # Fetch in bounded pages: a shared request may satisfy many concurrent
    # caller tickets without loading their entire retained history at once.
    after = ""
    while True:
        tickets = db.execute("""SELECT uuid,intent_uuid,first_sequence,unassigned FROM run_intent_tickets
            WHERE intent_uuid=? AND unassigned_count>0 AND first_sequence<=? AND uuid>?
            ORDER BY uuid LIMIT 100""", (request["intent_uuid"], request["seq"], after)).fetchall()
        for ticket in tickets:
            attach(db, ticket, request)
        if len(tickets) < 100:
            return
        after = tickets[-1]["uuid"]


def migrate(db):
    """Reconstruct the first covering submissions, preserving old ticket intent."""
    db.execute("ALTER TABLE run_intent_tickets ADD COLUMN unassigned BLOB NOT NULL DEFAULT X'5B5D'")
    db.execute("ALTER TABLE run_intent_tickets ADD COLUMN unassigned_count INTEGER NOT NULL DEFAULT 0 CHECK(unassigned_count>=0)")
    db.execute("CREATE INDEX unassigned_run_tickets ON run_intent_tickets(intent_uuid,uuid) WHERE unassigned_count>0")
    db.execute("""CREATE TABLE run_ticket_requests(
        ticket_uuid TEXT NOT NULL REFERENCES run_intent_tickets(uuid),
        request_uuid TEXT NOT NULL REFERENCES run_requests(request_uuid), windows BLOB NOT NULL,
        PRIMARY KEY(ticket_uuid,request_uuid)
    )""")
    after = ""
    while True:
        tickets = db.execute("SELECT uuid,intent_uuid,window,first_sequence FROM run_intent_tickets WHERE uuid>? ORDER BY uuid LIMIT 100", (after,)).fetchall()
        for ticket in tickets:
            wanted = windows.normalize(decode(ticket["window"], 8192))
            db.execute("UPDATE run_intent_tickets SET unassigned=?,unassigned_count=1 WHERE uuid=?",
                       (encode([wanted], 16384), ticket["uuid"]))
            cursor = ticket["first_sequence"] - 1
            while True:
                requests = db.execute("""SELECT seq,request_uuid,intent_uuid,window FROM run_requests
                    WHERE intent_uuid=? AND seq>? AND until_stamp>?
                    AND (json_extract(window,'$.since') IS NULL OR json_extract(window,'$.since')<?)
                    ORDER BY seq LIMIT 100""", (ticket["intent_uuid"], cursor, wanted["since"] or "", wanted["until"])).fetchall()
                for request in requests:
                    current = db.execute("SELECT * FROM run_intent_tickets WHERE uuid=?", (ticket["uuid"],)).fetchone()
                    if not current["unassigned_count"]:
                        break
                    attach(db, current, request)
                remaining = db.execute("SELECT unassigned_count FROM run_intent_tickets WHERE uuid=?", (ticket["uuid"],)).fetchone()[0]
                if not remaining or len(requests) < 100:
                    break
                cursor = requests[-1]["seq"]
        if len(tickets) < 100:
            return
        after = tickets[-1]["uuid"]
