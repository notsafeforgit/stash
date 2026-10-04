"""Owned detail execution retaining source evidence before acknowledging success."""

from .discovery_detail_client import DiscoveryDetailClient
from .discovery_detail_journal import DiscoveryDetailJournal
from .enrichment_worker import execute_metadata
from .metadata_fetch import fetch


def fetch_detail(*args, **kwargs):
    return fetch(*args, **kwargs, allow_empty=True)


def execute(box, transport, configuration, job_uuid, *, fetcher=fetch_detail):
    return execute_metadata(box, transport, configuration, job_uuid,
                            DiscoveryDetailJournal, DiscoveryDetailClient, fetcher=fetcher)
