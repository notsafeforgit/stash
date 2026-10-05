"""Check producer job journals against immutable native acknowledgements.

This is receipt verification, not worker execution. Historical leases and retry
deadlines are never renewed, reset or interpreted as current ownership.
"""

from datetime import datetime, timedelta, timezone
import hashlib
import re

from .receipts import checked_json, identifier, objects
from .storage import InvalidArchive, json_bytes

MAX_BODY = 32 << 20
MAX_STATE = 64 << 10
FAMILIES = (
    ("enrichment", 8, "post.enrich", "enrichment_job_attempts"),
    ("discovery", 11, "account.list_page", "discovery_job_attempts"),
    ("discovery_detail", 14, "post.verify_candidate", "discovery_detail_attempts"),
)
STAMP = re.compile(r"(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})?\Z")


def same(left, right, message):
    if json_bytes(left) != json_bytes(right):
        raise InvalidArchive(message)


def one(db, query, parameters, message, *, optional=False):
    rows = list(objects(db, query, parameters))
    if len(rows) != 1:
        if optional and not rows:
            return None
        raise InvalidArchive(message)
    return rows[0]


def instant(value, *, native=False):
    """Compare Go RFC3339Nano and SQLite timestamps without losing nanoseconds."""
    match = STAMP.fullmatch(value) if isinstance(value, str) else None
    if match is None or (not native and match[3] is None):
        raise InvalidArchive("Invalid retained job acknowledgement time")
    try:
        base = datetime.fromisoformat(match[1] + (match[3] or "+00:00")).astimezone(timezone.utc)
        return base, int((match[2] or "").ljust(9, "0"))
    except (ValueError, OverflowError) as error:
        raise InvalidArchive("Invalid retained job acknowledgement time") from error


def match_time(saved, stored):
    if instant(saved) != instant(stored, native=True):
        raise InvalidArchive("Retained job acknowledgement time differs from native history")


def match_projection(saved, native, keys, message):
    if not isinstance(saved, dict):
        raise InvalidArchive(message)
    same({k: saved.get(k) for k in keys}, {k: native[k] for k in keys}, message)
    if "created_at" in native:
        match_time(saved.get("created_at"), native.get("created_at"))


def bounded_bytes(value, digest, maximum):
    if not isinstance(value, bytes) or not 0 < len(value) <= maximum or hashlib.sha256(value).hexdigest() != digest:
        raise InvalidArchive("Producer job journal lost its exact retained bytes")
    return value


def attempt(db, table, job, lease, producer=None):
    if (not isinstance(lease, dict) or type(lease.get("fence")) is not int or lease["fence"] < 1):
        raise InvalidArchive("Invalid retained job attempt")
    identifier(lease.get("owner_uuid"))
    row = one(db, f"""SELECT a.*,p.producer_uuid FROM archive_job_attempts a
        JOIN {table} p ON p.job_uuid=a.job_uuid AND p.fence=a.fence
        WHERE a.job_uuid=? AND a.fence=?""", (job, lease["fence"]), "Acknowledged job attempt is missing from native history")
    if row["owner_uuid"] != lease["owner_uuid"] or producer is not None and row["producer_uuid"] != producer:
        raise InvalidArchive("Retained job attempt belongs to another owner or producer")
    return row


def failure(db, table, job, saved, producer):
    row = attempt(db, table, job, saved, producer)
    row["result"] = checked_json(row["result"], 16384)
    match_projection(saved, row, ("job_uuid", "fence", "owner_uuid", "producer_uuid", "outcome", "error_code", "result"),
                     "Failure acknowledgement differs from native attempt history")
    if row["outcome"] not in ("retry", "failed"):
        raise InvalidArchive("Failure acknowledgement has no completed native attempt")
    epoch = datetime(1970, 1, 1, tzinfo=timezone.utc)
    for key in ("started_at", "ended_at"):
        stored = row[key + "_ms"]
        if type(stored) is not int:
            raise InvalidArchive("Native attempt lost its completion time")
        expected = epoch + timedelta(milliseconds=stored)
        if instant(saved.get(key)) != (expected.replace(microsecond=0), expected.microsecond * 1000):
            raise InvalidArchive("Failure acknowledgement time differs from native attempt history")


def checkpoint(db, family, job, receipt):
    if not isinstance(receipt, dict):
        raise InvalidArchive("Missing metadata checkpoint acknowledgement")
    row = one(db, f"""SELECT job_uuid,revision,digest AS sha256,fence,record_count,pending_count,
        unresolved_count,created_at FROM {family}_checkpoint_receipts WHERE job_uuid=? AND revision=?""",
        (job, receipt.get("revision")), "Acknowledged metadata checkpoint is missing from native history")
    match_projection(receipt, row, ("job_uuid", "revision", "sha256", "fence", "record_count", "pending_count", "unresolved_count"),
                     "Metadata checkpoint acknowledgement differs from native history")
    return row


def metadata_completion(db, family, job, receipt):
    if family == "enrichment":
        row = one(db, """SELECT p.*,r.digest AS checkpoint_sha256,r.record_count,r.unresolved_count,e.capture_count
            FROM enrichment_publications p JOIN enrichment_checkpoint_receipts r
            ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
            JOIN enrichment_completions e ON e.uuid=p.completion_uuid WHERE p.job_uuid=?""",
            (job,), "Acknowledged enrichment publication is missing from native history")
        keys = ("job_uuid", "checkpoint_revision", "checkpoint_sha256", "fence", "completion_uuid",
                "record_count", "capture_count", "unresolved_count")
    else:
        row = one(db, """SELECT job_uuid,checkpoint_revision,fence,created_at,
            CASE WHEN length(CAST(evidence AS BLOB))<=16384 THEN evidence END AS evidence
            FROM discovery_detail_results WHERE job_uuid=?""", (job,),
            "Acknowledged detail comparison is missing from native history")
        row["evidence"] = checked_json(row["evidence"], 16384)
        keys = ("job_uuid", "checkpoint_revision", "fence", "evidence")
    match_projection(receipt, row, keys, "Metadata completion acknowledgement differs from native history")


def page(db, job, receipt):
    row = one(db, """SELECT listing_uuid,ordinal,job_uuid,fence,producer_uuid,digest AS sha256,
        record_count,complete,created_at,byte_count,
        CASE WHEN length(CAST(body AS BLOB))<=33554432 THEN CAST(body AS BLOB) END AS body
        FROM discovery_pages WHERE job_uuid=?""", (job,), "Acknowledged listing page is missing from native history")
    row["complete"] = bool(row["complete"])
    if receipt is not None:
        match_projection(receipt, row, ("listing_uuid", "ordinal", "job_uuid", "fence", "producer_uuid", "sha256", "record_count", "complete"),
                         "Listing acknowledgement differs from native history")
    raw = bounded_bytes(row["body"], row["sha256"], MAX_BODY)
    if len(raw) != row["byte_count"]:
        raise InvalidArchive("Native listing page lost its acknowledged bytes")
    return row


def definition(db, family, row, value):
    expected = {"job_uuid", "arguments", "listing", "cursor"} if family == "discovery" else {"job_uuid", "arguments", "url"}
    if family == "enrichment":
        expected.add("policy")
    if set(value) != expected or value.get("job_uuid") != row["job_uuid"] or not isinstance(value.get("arguments"), dict):
        raise InvalidArchive("Invalid producer job definition")
    if family == "enrichment":
        source = one(db, """SELECT u.url,t.policy FROM enrichment_targets t
            JOIN source_post_urls u ON u.uuid=t.url_uuid WHERE t.uuid=?""",
            (value["arguments"].get("target_uuid"),), "Metadata job source is missing from native history")
        same({k: value[k] for k in ("url", "policy")}, source, "Metadata job source differs from native history")
    elif family == "discovery_detail":
        if value["url"] != value["arguments"].get("url"):
            raise InvalidArchive("Detail job source differs from its immutable request")
    else:
        listing = value["listing"]
        if not isinstance(listing, dict):
            raise InvalidArchive("Missing immutable listing definition")
        source = one(db, """SELECT digest,created_at,
            CASE WHEN length(CAST(definition AS BLOB))<=32768 THEN definition END AS definition
            FROM discovery_listings WHERE uuid=?""", (value["arguments"].get("listing_uuid"),),
            "Listing definition is missing from native history")
        native = checked_json(source["definition"], 32768)
        same({k:v for k,v in listing.items() if k not in ("sha256", "created_at")}, native,
             "Listing definition differs from native history")
        if listing.get("sha256") != source["digest"] or value["arguments"].get("definition_sha256") != source["digest"]:
            raise InvalidArchive("Listing definition lost its original digest")
        match_time(listing.get("created_at"), source["created_at"])
        ordinal = value["arguments"].get("page_ordinal")
        if type(ordinal) is not int or ordinal < 1:
            raise InvalidArchive("Invalid retained listing ordinal")
        cursor = native.get("initial_cursor")
        if ordinal > 1:
            prior = one(db, """SELECT json_extract(body,'$.next_cursor') AS cursor
                FROM discovery_pages WHERE listing_uuid=? AND ordinal=?""", (listing.get("uuid"), ordinal - 1),
                "Listing continuation is missing its acknowledged predecessor")
            # Cursors are objects or null in the native listing protocol.
            cursor = checked_json(prior["cursor"], MAX_STATE) if prior["cursor"] is not None else None
        same(value["cursor"], cursor, "Listing continuation differs from its acknowledged predecessor")


def verify_job_journals(db, queue, producer, version):
    reports = {}
    for family, introduced, kind, attempts in FAMILIES:
        table = family + "_executions"
        exists = queue.execute("SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?", (table,)).fetchone() is not None
        if exists != (version >= introduced):
            raise InvalidArchive("Producer schema version would omit or invent a retained job journal")
        counts = {k:0 for k in ("active", "review", "completed", "failed", "pending_bodies", "checkpoints", "completions", "failures",
                               "accepted_pending_bodies", "superseded_pending_bodies")}
        digest = hashlib.sha256()
        if exists:
            query = f"""SELECT job_uuid,revision,phase,definition_sha256,state_sha256,body_sha256,reserved_bytes,
                length(body) AS body_length,
                CASE WHEN length(definition)<=65536 THEN definition END AS definition,
                CASE WHEN length(state)<=65536 THEN state END AS state,
                CASE WHEN length(body)<=33554432 THEN body END AS body FROM {table} ORDER BY job_uuid"""
            for row in objects(queue, query):
                identifier(row["job_uuid"])
                if (type(row["revision"]) is not int or row["revision"] < 1 or row["phase"] not in ("active", "review", "completed", "failed")
                        or type(row["reserved_bytes"]) is not int or not 0 <= row["reserved_bytes"] <= MAX_BODY):
                    raise InvalidArchive("Invalid producer job revision, phase or reservation")
                value = checked_json(bounded_bytes(row["definition"], row["definition_sha256"], MAX_STATE), MAX_STATE)
                state = checked_json(bounded_bytes(row["state"], row["state_sha256"], MAX_STATE), MAX_STATE)
                native = one(db, """SELECT uuid,kind,state,fence,revision,
                    CASE WHEN length(CAST(arguments AS BLOB))<=262144 THEN arguments END AS arguments
                    FROM archive_jobs WHERE uuid=?""", (row["job_uuid"],), "Recorded producer job is missing from native history")
                if native["kind"] != kind:
                    raise InvalidArchive("Producer job belongs to another native job family")
                same(value.get("arguments"), checked_json(native["arguments"], 262144), "Producer job lost its immutable native arguments")
                definition(db, family, row, value)
                verify_state(db, family, attempts, producer, row, value, state, native, counts)
                counts[row["phase"]] += 1
                digest.update(json_bytes({k:row[k] for k in ("job_uuid", "revision", "phase", "definition_sha256", "state_sha256", "body_sha256", "reserved_bytes")}))
        reports[family] = {"counts": counts, "boundary_sha256": digest.hexdigest()}
    return reports


def verify_state(db, family, attempts, producer, row, value, state, native, counts):
    from .job_states import verify_discovery, verify_metadata
    job = row["job_uuid"]
    pending, lease, claim = state.get("pending"), state.get("lease"), state.get("claim")
    if claim is not None:
        if not isinstance(claim, dict):
            raise InvalidArchive("Invalid retained job claim intent")
        identifier(claim.get("owner_uuid"))
        description = claim.get("description") if family == "discovery" else None
        if family == "discovery" and not isinstance(description, dict):
            raise InvalidArchive("Invalid retained listing claim description")
        claimed = description.get("job") if family == "discovery" else claim.get("job")
        if not isinstance(claimed, dict) or claimed.get("uuid") != job or claimed.get("kind") != native["kind"]:
            raise InvalidArchive("Retained claim identifies another job")
        same(claimed.get("arguments"), value["arguments"], "Retained claim identifies another job definition")
    if lease is not None:
        if (not isinstance(lease, dict) or lease.get("uuid") != job or lease.get("kind") != native["kind"]
                or lease.get("state") != "running" or claim is None):
            raise InvalidArchive("Retained lease identifies another job")
        same(lease.get("arguments"), value["arguments"], "Retained lease identifies another job definition")
        if lease.get("owner_uuid") != claim["owner_uuid"]:
            raise InvalidArchive("Retained lease differs from its claim intent")
        attempt(db, attempts, job, lease, producer)
    if pending is not None:
        if not isinstance(pending, dict):
            raise InvalidArchive("Invalid retained delivery intent")
        attempt(db, attempts, job, pending.get("lease"), producer)
    if row["body_length"] is not None:
        bounded_bytes(row["body"], row["body_sha256"], MAX_BODY)
        if pending is None or pending.get("sha256") != row["body_sha256"] or row["reserved_bytes"] < row["body_length"]:
            raise InvalidArchive("Pending job body lost its intent or reservation")
        counts["pending_bodies"] += 1
    elif row["body_sha256"] is not None:
        raise InvalidArchive("Pending job body is missing")
    if row["phase"] in ("completed", "failed") and (row["body_length"] is not None or row["reserved_bytes"] or pending is not None):
        raise InvalidArchive("Terminal producer job still has unacknowledged work")
    if state.get("failure") is not None:
        failure(db, attempts, job, state["failure"], producer)
        counts["failures"] += 1
    if family == "discovery":
        verify_discovery(db, producer, row, value, state, native, counts)
    else:
        verify_metadata(db, family, row, value, state, native, counts)
