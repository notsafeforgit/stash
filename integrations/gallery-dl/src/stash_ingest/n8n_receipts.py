"""Retained legacy n8n command results, never native source-completion proof."""

from collections import Counter
import re

from .encoding import InvalidData, decode, digest, encode, identifier
from .outbox import Capacity, Conflict

MAX_RECEIPT = 128 << 10
MAX_FILES = 10000
MAX_BYTES = 64 << 20
TOKEN = re.compile(r"[0-9a-f]{32}")
REQUIRED = {"command_failed", "exit_code", "network_blocked", "stdout_tail", "stderr_tail"}
OPTIONAL = {"backfill_cached", "account_backfill_complete", "completed_at", "backfill_skip_reason", "legacy_skip_recorded_at"}


def classify(value):
    # Unknown fields stay in the original bytes. They cannot acquire execution
    # semantics merely by being echoed into a native workflow result.
    if not isinstance(value, dict) or not REQUIRED <= value.keys() or value.keys() - REQUIRED - OPTIONAL:
        return "review", "unsupported_result_shape"
    if (any(type(value[k]) is not bool for k in ("command_failed", "network_blocked"))
            or type(value["exit_code"]) is not int or not -255 <= value["exit_code"] <= 255
            or any(not isinstance(value[k], str) or len(value[k]) > 8000 for k in ("stdout_tail", "stderr_tail"))
            or any(type(value[k]) is not bool for k in ("backfill_cached", "account_backfill_complete") if k in value)
            or any(value[k] is not None and (not isinstance(value[k], str) or len(value[k]) > 4096)
                   for k in ("completed_at", "backfill_skip_reason", "legacy_skip_recorded_at") if k in value)):
        return "review", "unsupported_result_fields"
    if value["command_failed"] != (value["exit_code"] != 0):
        return "review", "inconsistent_command_result"
    if value["command_failed"] or value["network_blocked"]:
        return "failed", None
    if "backfill_skip_reason" in value or "legacy_skip_recorded_at" in value:
        if (value.get("backfill_cached") is not True or value.get("account_backfill_complete") is not False
                or value.get("completed_at") is not None or not value.get("backfill_skip_reason")
                or not value.get("legacy_skip_recorded_at")):
            return "review", "inconsistent_skip_result"
        return "skipped", None
    return "succeeded", None


def migrate(db):
    db.execute("""CREATE TABLE n8n_receipt_imports(
        source_uuid TEXT NOT NULL, input_sha256 TEXT NOT NULL, manifest BLOB NOT NULL,
        file_count INTEGER NOT NULL CHECK(file_count BETWEEN 0 AND 10000),
        byte_count INTEGER NOT NULL CHECK(byte_count BETWEEN 0 AND 67108864),
        imported_at REAL NOT NULL, PRIMARY KEY(source_uuid,input_sha256)
    )""")
    db.execute("""CREATE TABLE legacy_n8n_receipts(
        token TEXT PRIMARY KEY CHECK(length(token)=32 AND token NOT GLOB '*[^0-9a-f]*'),
        source_uuid TEXT NOT NULL, input_sha256 TEXT NOT NULL, sha256 TEXT NOT NULL,
        body BLOB NOT NULL CHECK(length(body) BETWEEN 1 AND 131072),
        outcome TEXT NOT NULL CHECK(outcome IN ('succeeded','skipped','failed','review')),
        FOREIGN KEY(source_uuid,input_sha256) REFERENCES n8n_receipt_imports(source_uuid,input_sha256)
    )""")
    db.execute("CREATE INDEX legacy_n8n_receipt_sources ON legacy_n8n_receipts(source_uuid,input_sha256)")
    for table in ("n8n_receipt_imports", "legacy_n8n_receipts"):
        for action in ("UPDATE", "DELETE"):
            db.execute(f"""CREATE TRIGGER {table}_{action.lower()} BEFORE {action} ON {table}
                BEGIN SELECT RAISE(ABORT,'legacy n8n evidence is immutable'); END""")
    db.execute("""CREATE TRIGGER legacy_n8n_token_unique BEFORE INSERT ON legacy_n8n_receipts
        WHEN EXISTS(SELECT 1 FROM backfill_calls WHERE uuid=
            substr(NEW.token,1,8)||'-'||substr(NEW.token,9,4)||'-'||substr(NEW.token,13,4)||'-'||substr(NEW.token,17,4)||'-'||substr(NEW.token,21,12))
        BEGIN SELECT RAISE(ABORT,'result token already identifies native work'); END""")
    db.execute("""CREATE TRIGGER native_n8n_token_unique BEFORE INSERT ON backfill_calls
        WHEN EXISTS(SELECT 1 FROM legacy_n8n_receipts WHERE token=replace(NEW.uuid,'-',''))
        BEGIN SELECT RAISE(ABORT,'result token already identifies legacy evidence'); END""")


def manifest(records):
    entries = []
    total = 0
    seen = set()
    for token, body in records:
        if not isinstance(token, str) or not TOKEN.fullmatch(token) or token in seen:
            raise InvalidData("Invalid or duplicated legacy n8n result token")
        value = decode(body, MAX_RECEIPT)
        outcome, reason = classify(value)
        entries.append({"token": token, "sha256": digest(body), "bytes": len(body), "outcome": outcome, "review_reason": reason})
        total += len(body)
        seen.add(token)
        if len(entries) > MAX_FILES or total > MAX_BYTES:
            raise Capacity("Legacy n8n input exceeds the bounded snapshot capacity")
    return encode({"version": 1, "files": sorted(entries, key=lambda item: item["token"])}, 4 << 20)


class LegacyReceipts:
    def __init__(self, box):
        self.box, self.db = box, box.db

    def import_snapshot(self, source_uuid, records, expected_sha256):
        identifier(source_uuid)
        records = list(records)
        body = manifest(records)
        input_sha = digest(body)
        if input_sha != expected_sha256:
            raise Conflict("Legacy n8n snapshot differs from the reviewed digest")
        entries = decode(body, 4 << 20)["files"]
        lookup = dict(records)
        total = sum(item["bytes"] for item in entries)
        added = 0
        with self.box.transaction():
            prior = self.db.execute("SELECT manifest FROM n8n_receipt_imports WHERE source_uuid=? AND input_sha256=?",
                                    (source_uuid, input_sha)).fetchone()
            if prior is not None and prior[0] != body:
                raise Conflict("Legacy n8n import manifest differs from its recorded bytes")
            if prior is None:
                imports, bytes_used = self.db.execute("SELECT count(*),coalesce(sum(length(manifest)),0) FROM n8n_receipt_imports").fetchone()
                if imports >= 1000 or bytes_used + len(body) > 16 << 20:
                    raise Capacity("Legacy n8n import manifest capacity exhausted; retained history was not removed")
                self.db.execute("INSERT INTO n8n_receipt_imports VALUES(?,?,?,?,?,?)",
                                (source_uuid, input_sha, body, len(entries), total, self.box.clock()))
            count, size = self.db.execute("SELECT count(*),coalesce(sum(length(body)),0) FROM legacy_n8n_receipts").fetchone()
            for entry in entries:
                token = entry["token"]
                existing = self.db.execute("SELECT source_uuid,sha256,body,outcome FROM legacy_n8n_receipts WHERE token=?", (token,)).fetchone()
                if existing is not None:
                    if tuple(existing) != (source_uuid, entry["sha256"], lookup[token], entry["outcome"]):
                        raise Conflict("Legacy n8n token already identifies different evidence or a different source")
                    continue
                native_uuid = f"{token[:8]}-{token[8:12]}-{token[12:16]}-{token[16:20]}-{token[20:]}"
                if self.db.execute("SELECT 1 FROM backfill_calls WHERE uuid=?", (native_uuid,)).fetchone():
                    raise Conflict("Legacy n8n token already identifies native work")
                count += 1
                size += entry["bytes"]
                if count > MAX_FILES or size > MAX_BYTES:
                    raise Capacity("Legacy n8n receipt capacity exhausted; retained history was not removed")
                self.db.execute("INSERT INTO legacy_n8n_receipts VALUES(?,?,?,?,?,?)",
                                (token, source_uuid, input_sha, entry["sha256"], lookup[token], entry["outcome"]))
                added += 1
        return {"added": added, "replayed": len(entries) - added, "input_sha256": input_sha}

    def result(self, token):
        if not isinstance(token, str) or not TOKEN.fullmatch(token):
            raise InvalidData("Invalid legacy n8n result token")
        row = self.db.execute("SELECT * FROM legacy_n8n_receipts WHERE token=?", (token,)).fetchone()
        if row is None:
            return None
        if digest(row["body"]) != row["sha256"]:
            raise InvalidData("Legacy n8n receipt differs from its recorded digest")
        value = decode(row["body"], MAX_RECEIPT)
        outcome, reason = classify(value)
        if outcome != row["outcome"]:
            raise InvalidData("Legacy n8n result classification differs from its recorded evidence")
        result = (dict(value) if outcome != "review" else
                  {"command_failed": True, "exit_code": 1, "network_blocked": False,
                   "stdout_tail": "", "stderr_tail": "Legacy command result requires review: " + reason})
        if outcome == "failed":
            # Some existing workflow branches inspect only command_failed. The
            # old backfill guard also treated a network block as failure even
            # when the child returned zero. Preserve its raw result in storage.
            result["command_failed"] = True
            result["exit_code"] = value["exit_code"] or 1
        result.update(token=token, state="legacy_" + outcome, backfill_pending=False,
                      legacy_receipt={"source_uuid": row["source_uuid"], "sha256": row["sha256"],
                                      "input_sha256": row["input_sha256"], "outcome": outcome, "review_reason": reason,
                                      "account_attribution": "not_recorded"},
                      intake_completion="unverified")
        return result

    def summary(self):
        counts = dict.fromkeys(("succeeded", "skipped", "failed", "review"), 0)
        counts.update({row["outcome"]: row["total"] for row in self.db.execute(
            "SELECT outcome,count(*) AS total FROM legacy_n8n_receipts GROUP BY outcome")})
        return {"receipts": sum(counts.values()), "counts": counts}


def review(records):
    body = manifest(records)
    entries = decode(body, 4 << 20)["files"]
    return {"input_sha256": digest(body), "files": entries, "receipts": len(entries),
            "bytes": sum(item["bytes"] for item in entries), "counts": dict(Counter(item["outcome"] for item in entries))}
