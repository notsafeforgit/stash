"""Cross-snapshot proof for ingestion and source-run admission receipts.

This is one part of backup coordination. It does not certify download archives,
media, filesystem recovery or configuration.
The coordinator must bind this proof to the exact component hashes it publishes.
Connections must already hold read transactions on the snapshots being checked.
"""

from contextlib import closing, ExitStack
from datetime import datetime, timezone
import hashlib
from pathlib import Path
import tempfile
import uuid
from urllib.parse import urlsplit

from .bundle import FORMAT, _target, connect_readonly, import_archive, iter_artifacts
from .storage import HEX, RESERVE_BYTES, InvalidArchive, decode_json, json_bytes

MAX_EVENT = 5 << 20
MAX_RECEIPT = 65536
COMMON = ("event_uuid", "collection_uuid", "collection_revision", "root_uuid", "run_uuid", "kind")
OPTIONAL = ("post_uuid", "capture_uuid", "job_uuid")


def identifier(value):
    try:
        parsed = uuid.UUID(value)
        if not parsed.int or str(parsed) != value:
            raise ValueError()
    except (ValueError, TypeError, AttributeError) as error:
        raise InvalidArchive("Expected a canonical nonzero UUID in receipt boundary") from error
    return value


def origin(value):
    try:
        if not isinstance(value, str) or any(ord(c) <= 32 for c in value):
            raise ValueError()
        parsed = urlsplit(value)
        if (parsed.scheme not in {"http", "https"} or not parsed.hostname
                or parsed.username is not None or parsed.password is not None
                or parsed.path not in {"", "/"} or "?" in value or "#" in value
                or "\\" in value or "%" in parsed.netloc):
            raise ValueError()
        host = parsed.hostname.encode("idna").decode("ascii").lower()
        if ":" in host:
            host = "[" + host + "]"
        port = parsed.port
        if port is not None and not 1 <= port <= 65535:
            raise ValueError()
        if port == {"http": 80, "https": 443}[parsed.scheme]:
            port = None
        return parsed.scheme + "://" + host + (":" + str(port) if port else "")
    except (ValueError, UnicodeError) as error:
        raise InvalidArchive("Invalid producer origin in receipt boundary") from error


def timestamp(value, *, sqlite=False):
    try:
        parsed = datetime.fromisoformat(value)
        if parsed.tzinfo is None:
            if not sqlite:
                raise ValueError()
            parsed = parsed.replace(tzinfo=timezone.utc)  # SQLite CURRENT_TIMESTAMP.
        return parsed.astimezone(timezone.utc)
    except (ValueError, TypeError) as error:
        raise InvalidArchive("Invalid receipt commit time") from error


def objects(connection, query, parameters=()):
    cursor = connection.execute(query, parameters)
    names = [column[0] for column in cursor.description]
    for row in cursor:
        yield dict(zip(names, row))


def checked_json(body, maximum):
    if not isinstance(body, (bytes, str)) or not 0 < len(body) <= maximum:
        raise InvalidArchive("Missing or oversized retained event/receipt")
    value = decode_json(body)
    if not isinstance(value, dict):
        raise InvalidArchive("Retained event/receipt must be an object")
    return value


def matching_receipt(row, producer, server, retained=None):
    expected = {name: row[name] for name in COMMON}
    expected.update(producer_uuid=producer, sha256=row["sha256"])
    native = {name: server[name] for name in COMMON}
    native.update(producer_uuid=server["producer_uuid"], sha256=server["digest"])
    if json_bytes(native) != json_bytes(expected):
        raise InvalidArchive("Native receipt does not match the producer event")
    if retained is None:
        return
    if json_bytes({key: retained.get(key) for key in expected}) != json_bytes(expected):
        raise InvalidArchive("Retained acknowledgement does not match its event")
    if retained.get("credential_uuid") != server["credential_uuid"]:
        raise InvalidArchive("Retained acknowledgement identifies another credential")
    if any(retained.get(key) != server[key] for key in OPTIONAL):
        raise InvalidArchive("Retained acknowledgement identifies another native result")
    result = checked_json(server["result"], 16384)
    if not isinstance(retained.get("result"), dict) or json_bytes(retained["result"]) != json_bytes(result):
        raise InvalidArchive("Retained acknowledgement result differs from the native receipt")
    if timestamp(retained.get("committed_at")) != timestamp(server["committed_at"], sqlite=True):
        raise InvalidArchive("Retained acknowledgement commit time differs from the native receipt")


def verify_ingestion_receipts(library, outboxes, expected_origin):
    """Fail if any discarded payload lacks the same immutable native receipt.

    An unacknowledged event may already exist in the native snapshot after a lost
    response. Its exact bytes must still be available for idempotent replay. A
    conflicting receipt is rejected even when the producer retained the body.
    No queue, lease, event state or application row is changed by this check.
    """
    expected_origin = origin(expected_origin)
    outboxes = list(outboxes)
    if any(not db.in_transaction or db.execute("PRAGMA query_only").fetchone()[0] != 1
           for db in [library] + outboxes):
        raise InvalidArchive("Receipt verification requires held read-only snapshot transactions")
    if library.execute("SELECT lineage FROM native_schema WHERE singleton=1").fetchall() != [(FORMAT,)]:
        raise InvalidArchive("Foreign native snapshot")
    reports, producers = [], set()
    for queue in outboxes:
        if (queue.execute("PRAGMA application_id").fetchone()[0] != 0x5354494F
                or queue.execute("PRAGMA user_version").fetchone()[0] not in range(1, 16)):
            raise InvalidArchive("Unsupported producer snapshot for receipt verification")
        binding = queue.execute("SELECT endpoint,producer FROM binding").fetchall()
        if len(binding) != 1 or origin(binding[0][0]) != expected_origin:
            raise InvalidArchive("Producer snapshot belongs to another origin")
        producer = identifier(binding[0][1])
        if producer in producers:
            raise InvalidArchive("Duplicate producer snapshot in receipt boundary")
        producers.add(producer)
        if library.execute("SELECT uuid FROM ingest_producers WHERE uuid=?", (producer,)).fetchall() != [(producer,)]:
            raise InvalidArchive("Producer identity is missing from the native snapshot")
        counts = {name: 0 for name in ("acknowledged", "pending", "sending", "review", "accepted_unacknowledged")}
        digest, maximum_sequence = hashlib.sha256(), 0
        query = """SELECT seq,event_uuid,sha256,kind,collection_uuid,collection_revision,root_uuid,run_uuid,parent_uuid,state,
            length(body) AS body_length,length(receipt) AS receipt_length,
            CASE WHEN length(body)<=? THEN body END AS body,
            CASE WHEN length(receipt)<=? THEN receipt END AS receipt
            FROM events ORDER BY seq"""
        for row in objects(queue, query, (MAX_EVENT, MAX_RECEIPT)):
            if type(row["seq"]) is not int or row["seq"] <= maximum_sequence:
                raise InvalidArchive("Invalid producer event sequence")
            maximum_sequence = row["seq"]
            if ((row["body_length"] is not None and row["body_length"] > MAX_EVENT)
                    or (row["receipt_length"] is not None and row["receipt_length"] > MAX_RECEIPT)):
                raise InvalidArchive("Oversized retained event or receipt")
            for key in ("event_uuid", "collection_uuid", "run_uuid"):
                identifier(row[key])
            if row["root_uuid"] is not None:
                identifier(row["root_uuid"])
            if (not isinstance(row["sha256"], str) or not HEX.fullmatch(row["sha256"])
                    or type(row["collection_revision"]) is not int or row["collection_revision"] < 1
                    or row["kind"] not in ("source.capture", "file.completed")
                    or row["state"] not in ("acknowledged", "pending", "sending", "review")):
                raise InvalidArchive("Invalid producer event identity or state")
            receipts = list(objects(library, "SELECT * FROM ingest_receipts WHERE producer_uuid=? AND event_uuid=?",
                                    (producer, row["event_uuid"])))
            server = receipts[0] if len(receipts) == 1 else None
            if len(receipts) > 1:
                raise InvalidArchive("Duplicate native event receipt")
            if row["state"] == "acknowledged":
                if server is None:
                    raise InvalidArchive("Acknowledged producer event is missing from the native snapshot")
                if row["body"] is not None:
                    raise InvalidArchive("Acknowledged event still has an unexpected payload")
                retained = checked_json(row["receipt"], MAX_RECEIPT)
                matching_receipt(row, producer, server, retained)
                evidence = {"receipt": retained}
            else:
                if row["receipt"] is not None:
                    raise InvalidArchive("Unacknowledged event has an unexpected receipt")
                event = checked_json(row["body"], MAX_EVENT)
                if (not isinstance(row["body"], bytes) or hashlib.sha256(row["body"]).hexdigest() != row["sha256"]
                        or event.get("producer_uuid") != producer
                        or json_bytes({key: event.get(key) for key in COMMON}) != json_bytes({key: row[key] for key in COMMON})):
                    raise InvalidArchive("Pending event lost its exact payload or identity")
                source = event.get("source")
                if row["kind"] == "file.completed" and source is not None and not isinstance(source, dict):
                    raise InvalidArchive("Invalid pending file source association")
                parent = source.get("capture_event_uuid") if row["kind"] == "file.completed" and source is not None else None
                if parent != row["parent_uuid"]:
                    raise InvalidArchive("Pending file lost its capture-event association")
                if server is not None:
                    matching_receipt(row, producer, server)
                    counts["accepted_unacknowledged"] += 1
                evidence = {"payload_sha256": row["sha256"]}
            counts[row["state"]] += 1
            digest.update(json_bytes(dict(evidence, seq=row["seq"], event_uuid=row["event_uuid"], state=row["state"])))
        from .run_receipts import verify_run_admissions
        from .job_receipts import verify_job_journals
        admissions = verify_run_admissions(library, queue, producer, queue.execute("PRAGMA user_version").fetchone()[0])
        journals = verify_job_journals(library, queue, producer, queue.execute("PRAGMA user_version").fetchone()[0])
        reports.append({"producer_uuid": producer, "events": sum(counts[k] for k in ("acknowledged", "pending", "sending", "review")),
                        "maximum_sequence": maximum_sequence, "counts": counts, "boundary_sha256": digest.hexdigest(),
                        "source_admissions": admissions, "job_journals": journals})
    if any(row[0] not in producers for row in library.execute("SELECT uuid FROM ingest_producers")):
        raise InvalidArchive("A registered producer has no matching outbox snapshot")
    return {"format": FORMAT + ".ingestion-receipt-boundary", "version": 1,
            "coverage": "capture-file-run-and-job-receipts", "origin": expected_origin,
            "registered_producers_complete": True,
            "producers": sorted(reports, key=lambda report: report["producer_uuid"])}


def verify_restored_receipts(source, restored, manifest, expected_origin):
    """Check an isolated, fully restored archive before its owner discards it."""
    entries = [entry for entry in iter_artifacts(source, manifest)
               if entry["role"] in ("library", "producer_outbox")]
    with ExitStack() as stack:
        library = stack.enter_context(closing(connect_readonly(Path(restored) / "library.sqlite")))
        library.execute("BEGIN")
        queues = []
        for entry in entries:
            if entry["role"] == "producer_outbox":
                queue = stack.enter_context(closing(connect_readonly(_target(Path(restored), entry))))
                queue.execute("BEGIN")
                queues.append(queue)
        report = verify_ingestion_receipts(library, queues, expected_origin)
    report.update(archive_uuid=manifest["uuid"], manifest_sha256=hashlib.sha256(json_bytes(manifest)).hexdigest(),
                  components=[{"role": e["role"], "name": e["name"], "sha256": e["sha256"]} for e in entries])
    return report


def verify_receipt_archive(source, expected_origin, *, temp_parent=None, reserve=RESERVE_BYTES):
    """Verify a complete transport, then bind receipt proof to its component hashes."""
    with tempfile.TemporaryDirectory(prefix="stash-archive-receipts-", dir=temp_parent) as temp:
        restored = Path(temp) / "restored"
        manifest = import_archive(source, restored, reserve=reserve)
        return verify_restored_receipts(source, restored, manifest, expected_origin)
