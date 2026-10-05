"""Family-specific producer journal acknowledgement and pending-body checks."""

from .job_receipts import (MAX_BODY, bounded_bytes, checkpoint, match_projection,
                           metadata_completion, one, page, same)
from .receipts import checked_json
from .storage import HEX, InvalidArchive


def keys(state, required, optional=()):
    if not set(required) <= state.keys() or state.keys() - set(required) - set(optional) or not isinstance(state.get("error_code"), str):
        raise InvalidArchive("Unsupported producer job journal state")


def superseded(db, job, pending, accepted):
    if accepted["fence"] <= pending["lease"]["fence"]:
        return False
    prior = one(db, "SELECT outcome FROM archive_job_attempts WHERE job_uuid=? AND fence=?",
                (job, pending["lease"]["fence"]), "Pending predecessor attempt is missing")
    if prior["outcome"] not in ("expired", "retry"):
        raise InvalidArchive("Pending job cannot be superseded before its attempt ended")
    return True


def metadata_storage(db, family, job, receipt):
    head = one(db, f"""SELECT h.revision,h.byte_size,r.digest,r.record_count,r.pending_count,r.unresolved_count,
        CASE WHEN length(CAST(h.body AS BLOB))<=33554432 THEN CAST(h.body AS BLOB) END AS body
        FROM {family}_checkpoints h JOIN {family}_checkpoint_receipts r
        ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=?""", (job,),
        "Invalid native metadata checkpoint", optional=True)
    if head is None:
        if family != "enrichment":
            raise InvalidArchive("Acknowledged detail checkpoint has lost its retained body")
        release = one(db, """SELECT r.version,r.proof_sha256,p.checkpoint_revision
            FROM enrichment_checkpoint_releases r JOIN enrichment_publications p ON p.job_uuid=r.job_uuid
            WHERE r.job_uuid=?""", (job,), "Acknowledged metadata checkpoint has no retained body or release receipt")
        if (release["version"] not in (1, 2) or release["checkpoint_revision"] < receipt["revision"]
                or not isinstance(release["proof_sha256"], str) or not HEX.fullmatch(release["proof_sha256"])):
            raise InvalidArchive("Metadata checkpoint has an unsupported release receipt")
        # Native schema validation checks the release's capture/provenance proof.
        # The archive preserves its complete database; this check covers receipt
        # correspondence and does not reconstruct a deliberately released body.
        return
    raw = bounded_bytes(head["body"], head["digest"], MAX_BODY)
    if len(raw) != head["byte_size"] or head["revision"] < receipt["revision"]:
        raise InvalidArchive("Native metadata head predates its acknowledged checkpoint")
    body = checked_json(raw, MAX_BODY)
    for key, count in (("records", "record_count"), ("pending", "pending_count"), ("unresolved", "unresolved_count")):
        if not isinstance(body.get(key), list) or len(body[key]) != head[count]:
            raise InvalidArchive("Native metadata body differs from its checkpoint receipt")


def verify_metadata(db, family, row, value, state, native, counts):
    completion = "publication" if family == "enrichment" else "comparison"
    intent = "publish" if family == "enrichment" else "complete"
    keys(state, ("claim", "lease", "pending", "checkpoint", completion, "error_code"), ("failure", "native_outcome"))
    job, pending = row["job_uuid"], state["pending"]
    ack = None
    if state["checkpoint"] is not None:
        ack = checkpoint(db, family, job, state["checkpoint"])
        counts["checkpoints"] += 1
    if (row["body_length"] is not None) != (pending is not None and pending.get("kind") == "checkpoint"):
        raise InvalidArchive("Metadata delivery lost its pending checkpoint body")
    if pending is not None:
        kind = pending.get("kind")
        if kind == "checkpoint":
            if (set(pending) != {"kind", "lease", "sha256", "expected_revision"}
                    or type(pending["expected_revision"]) is not int or not 0 <= pending["expected_revision"] < 128):
                raise InvalidArchive("Invalid pending checkpoint intent")
            body = checked_json(row["body"], MAX_BODY)
            if body.get("url") != value["url"] or body.get("extractor_version") != value["arguments"].get("extractor_version"):
                raise InvalidArchive("Pending checkpoint identifies another source or runtime")
            for key in ("records", "pending", "unresolved"):
                if not isinstance(body.get(key), list):
                    raise InvalidArchive("Pending checkpoint lost its retained records")
            accepted = one(db, f"""SELECT job_uuid,revision,digest AS sha256,fence,record_count,pending_count,unresolved_count,created_at
                FROM {family}_checkpoint_receipts WHERE job_uuid=? AND digest=?""", (job, row["body_sha256"]),
                "Repeated native metadata receipt", optional=True)
            if accepted is not None:
                newer = superseded(db, job, pending, accepted)
                if not newer and (accepted["revision"] > pending["expected_revision"] + 1
                        or accepted["revision"] == pending["expected_revision"] + 1 and accepted["fence"] != pending["lease"]["fence"]
                        or any(accepted[count] != len(body[key]) for key, count in (
                            ("records", "record_count"), ("pending", "pending_count"), ("unresolved", "unresolved_count")))):
                    raise InvalidArchive("Pending checkpoint conflicts with its native acceptance")
                ack = accepted
                counts["superseded_pending_bodies" if newer else "accepted_pending_bodies"] += 1
        elif kind == intent:
            if set(pending) != {"kind", "lease", "receipt"}:
                raise InvalidArchive("Invalid pending metadata completion intent")
            requested = checkpoint(db, family, job, pending["receipt"])
            if requested["pending_count"] or ack is None:
                raise InvalidArchive("Metadata completion lost its acknowledged complete checkpoint")
            same(pending["receipt"], state["checkpoint"], "Metadata completion identifies another checkpoint")
        elif kind != "failure" or set(pending) != {"kind", "lease", "error_code"} or not isinstance(pending["error_code"], str):
            raise InvalidArchive("Unsupported pending metadata delivery")
    if ack is not None:
        metadata_storage(db, family, job, ack)
    if state[completion] is not None:
        if row["phase"] != "completed" or native["state"] != "succeeded":
            raise InvalidArchive("Metadata completion has no successful native job")
        metadata_completion(db, family, job, state[completion])
        counts["completions"] += 1
    elif row["phase"] == "completed":
        raise InvalidArchive("Completed metadata job lost its native acknowledgement")
    if row["phase"] == "failed":
        observed = state.get("native_outcome")
        failed = state.get("failure")
        if observed not in ("failed", "cancelled") and (failed is None or failed.get("outcome") != "failed"):
            raise InvalidArchive("Failed metadata job lost its terminal acknowledgement")
        if observed is not None and observed != native["state"]:
            raise InvalidArchive("Retained metadata terminal state differs from native history")


def verify_discovery(db, producer, row, value, state, native, counts):
    keys(state, ("claim", "lease", "pending", "receipt", "failure", "terminal", "error_code"))
    job, pending, terminal = row["job_uuid"], state["pending"], state["terminal"]
    if (row["body_length"] is not None) != (pending is not None and pending.get("kind") == "page"):
        raise InvalidArchive("Listing delivery lost its pending page body")
    if pending is not None:
        if pending.get("kind") == "page":
            if (set(pending) != {"kind", "lease", "sha256", "record_count", "complete"}
                    or type(pending["record_count"]) is not int or type(pending["complete"]) is not bool):
                raise InvalidArchive("Invalid pending listing delivery")
            body = checked_json(row["body"], MAX_BODY)
            if (not isinstance(body.get("records"), list) or len(body["records"]) != pending["record_count"]
                    or type(body.get("complete")) is not bool or body["complete"] != pending["complete"]
                    or body.get("url") != value["listing"].get("profile_url")
                    or body.get("extractor_version") != value["listing"].get("extractor_version")):
                raise InvalidArchive("Pending listing body differs from its delivery intent")
            same(body.get("cursor"), value["cursor"], "Pending listing body identifies another continuation")
            accepted = one(db, """SELECT job_uuid,listing_uuid,ordinal,fence,producer_uuid,digest AS sha256,
                record_count,complete,created_at FROM discovery_pages WHERE job_uuid=?""", (job,),
                "Duplicate native listing acceptance", optional=True)
            if accepted is not None:
                accepted["complete"] = bool(accepted["complete"])
                expected = {"job_uuid":job, "listing_uuid":value["listing"]["uuid"], "ordinal":value["arguments"]["page_ordinal"],
                            "producer_uuid":producer, "fence":pending["lease"]["fence"],
                            **{k:pending[k] for k in ("sha256", "record_count", "complete")}}
                newer = superseded(db, job, pending, accepted)
                matched = ("job_uuid", "listing_uuid", "ordinal") if newer else expected.keys()
                same({k:expected[k] for k in matched}, {k:accepted[k] for k in matched}, "Pending listing conflicts with its native acceptance")
                page(db, job, None)
                counts["superseded_pending_bodies" if newer else "accepted_pending_bodies"] += 1
        elif pending.get("kind") != "failure" or set(pending) != {"kind", "lease", "error_code"} or not isinstance(pending["error_code"], str):
            raise InvalidArchive("Unsupported pending listing delivery")
    receipt = state["receipt"]
    if terminal is not None:
        if (not isinstance(terminal, dict) or set(terminal) != {"job", "listing", "cursor", "receipt"}
                or pending is not None or state["lease"] is not None or state["claim"] is not None):
            raise InvalidArchive("Invalid observed listing terminal state")
        saved = terminal["job"]
        if not isinstance(saved, dict):
            raise InvalidArchive("Invalid observed listing job")
        match_projection(saved, native, ("uuid", "kind", "state", "fence", "revision"), "Observed listing terminal state differs from native history")
        same(saved.get("arguments"), value["arguments"], "Observed listing terminal arguments changed")
        same(terminal["listing"], value["listing"], "Observed listing terminal definition changed")
        same(terminal["cursor"], value["cursor"], "Observed listing terminal cursor changed")
        if native["state"] not in ("succeeded", "failed", "cancelled"):
            raise InvalidArchive("Observed listing is not terminal in the native snapshot")
        if receipt is not None:
            raise InvalidArchive("Observed listing terminal state has duplicate local completion")
        receipt = terminal["receipt"]
    if receipt is not None:
        if row["phase"] != "completed" or native["state"] != "succeeded":
            raise InvalidArchive("Listing completion has no successful native job")
        if (not isinstance(receipt, dict) or receipt.get("listing_uuid") != value["listing"].get("uuid")
                or receipt.get("ordinal") != value["arguments"].get("page_ordinal")
                or terminal is None and receipt.get("producer_uuid") != producer):
            raise InvalidArchive("Listing acknowledgement identifies another page or producer")
        page(db, job, receipt)
        counts["completions"] += 1
    elif row["phase"] == "completed":
        raise InvalidArchive("Completed listing lost its native acknowledgement")
    if row["phase"] == "failed":
        if terminal is not None:
            if native["state"] not in ("failed", "cancelled"):
                raise InvalidArchive("Failed listing has no native terminal history")
        elif state["failure"] is None or state["failure"].get("outcome") != "failed":
            raise InvalidArchive("Failed listing lost its native attempt acknowledgement")
