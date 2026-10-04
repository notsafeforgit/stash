"""Separate candidate evidence and comparison receipts from native publications."""

from .discovery_detail_client import DiscoveryDetailClient
from .encoding import InvalidData
from .enrichment_journal import EnrichmentJournal, migrate as migrate_metadata
from .metadata_bundle import SCHEMA


def migrate(db):
    migrate_metadata(db, "discovery_detail")


class DiscoveryDetailJournal(EnrichmentJournal):
    table = "discovery_detail_executions"
    lock_name = "discovery_detail"
    client_type = DiscoveryDetailClient
    completion_field = "comparison"
    completion_intent = "complete"

    @staticmethod
    def definition(execution):
        job = execution["job"]
        work = DiscoveryDetailClient._job(job)
        return {"job_uuid": job["uuid"], "arguments": work, "url": work["url"]}

    def checkpoint(self, value, lease, expected, body):
        if not isinstance(body, dict) or body.get("schema") != SCHEMA:
            raise InvalidData("Detail requires newly observed metadata")
        return super().checkpoint(value, lease, expected, body)
