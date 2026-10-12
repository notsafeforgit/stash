"""Retain unfinished metadata work, not obsolete scheduler bookkeeping."""

import secrets

from .encoding import InvalidData
from .events import sha256
from .outbox import Capacity

DISPATCH_LIMIT = 10000
RECLAIM_BATCH = 1000


class DispatchCapacity(Capacity):
    def __init__(self, resource, records, limit=DISPATCH_LIMIT):
        self.resource, self.records, self.limit = resource, records, limit
        super().__init__(f"{resource} progress capacity exhausted ({records}/{limit}); "
                         "retire obsolete policy cursors; pending jobs and deliveries are retained")

    def result(self):
        return {"state": "unavailable", "error_code": "dispatch_capacity_exhausted",
                "resource": self.resource, "records": self.records, "limit": self.limit}


def initial_revision():
    # A recreated cursor must not accept a save from an evicted incarnation.
    # Leave ample signed-64-bit space for subsequent revision increments.
    return secrets.randbits(62) + 1


def reclaim_idle(box, table, journal_table, policy):
    """Called under the insert transaction, only when the cursor cache is full.

    Preserve partial pages, retry deadlines, the selected policy and every
    collection with unfinished local work. An expired, empty cursor has no
    remaining progress to lose. Native ready queries rediscover its work.
    """
    if (table, journal_table) not in (
            ("enrichment_dispatch", "enrichment_executions"),
            ("discovery_detail_dispatch", "discovery_detail_executions")):
        raise InvalidData("Unknown metadata dispatch cache")
    return box.db.execute(f"""DELETE FROM {table} WHERE rowid IN (
        SELECT d.rowid FROM {table} d
        WHERE d.policy_sha256<>? AND d.delivery_after='' AND d.local_after=''
          AND d.job_after=0 AND d.target_cursor IS NULL AND d.failures=0
          AND d.error_code IS NULL AND d.available_at>0 AND d.available_at<=?
          AND NOT EXISTS (SELECT 1 FROM {journal_table} j
              WHERE j.phase IN ('active','review')
                AND json_extract(j.definition,'$.arguments.collection_uuid')=d.collection_uuid)
        ORDER BY d.available_at,d.rowid LIMIT ?)
        """, (policy, box.clock(), RECLAIM_BATCH)).rowcount


def retire_enrichment_policies(box, policies, *, apply=False):
    """Explicitly retire reviewed, no-longer-configured enrichment policies.

    Stop dispatchers using these policies first. This never edits job journals,
    source requests, event bodies or server records. Policies with unfinished
    local jobs are protected even when explicitly included by an operator.
    """
    policies = sorted(set(policies))
    if not policies or len(policies) > 1024 or any(not sha256(p) or p == "0" * 64 for p in policies):
        raise InvalidData("Specify 1 to 1024 retired enrichment policy SHA-256 values")
    with box.transaction():
        protected = {row[0] for row in box.db.execute("""SELECT DISTINCT
            json_extract(definition,'$.arguments.policy_sha256') FROM enrichment_executions
            WHERE phase IN ('active','review')""")}
        eligible = [policy for policy in policies if policy not in protected]
        counts = {}
        for table in ("enrichment_dispatch", "enrichment_collection_dispatch"):
            total = box.db.execute(f"SELECT count(*) FROM {table}").fetchone()[0]
            count = 0
            if eligible:
                slots = ','.join('?' for _ in eligible)
                count = box.db.execute(f"SELECT count(*) FROM {table} WHERE policy_sha256 IN ({slots})", eligible).fetchone()[0]
                if apply:
                    box.db.execute(f"DELETE FROM {table} WHERE policy_sha256 IN ({slots})", eligible)
            counts[table] = {"before": total, "eligible": count, "deleted": count if apply else 0,
                             "remaining": total - count if apply else total}
    return {"state": "applied" if apply else "preview", "policies": len(policies),
            "protected_policies": len(set(policies) & protected), "cursors": counts}
