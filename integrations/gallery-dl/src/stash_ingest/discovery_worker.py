"""Execute one owned listing page or deliver its original saved observations."""

from .client import Unavailable
from .discovery_client import DiscoveryClient
from .discovery_fetch import ERRORS, fetch
from .discovery_journal import DiscoveryJournal
from .encoding import decode, identifier
from .job_lease import JobLease
from .metadata_bundle import MAX_BYTES
from .outbox import Capacity, Conflict
from .runs import SourcePaused
from .worker_policy import execution_policy


def _deliver(journal, client, value):
    pending = value.state["pending"]
    if pending["kind"] == "page":
        receipt = client.append_page(value.state["claim"]["description"], pending["lease"],
            decode(value.body, MAX_BYTES, preserve_numbers=True))
        outcome = "page_delivered"
    else:
        receipt = client.fail(value.job_uuid, pending["lease"], pending["error_code"])
        outcome = "failed" if receipt["outcome"] == "failed" else "retry"
    return journal.acknowledged(value, receipt), outcome


def _result(journal, job, state, receipt=None):
    return {"state": state, "job_uuid": job, "execution": journal.summary(job), "receipt": receipt}


def _configuration(configuration, description):
    configuration.check()
    listing = description["listing"]
    if (configuration.operation != "account.list_page" or execution_policy(description["job"], listing["policy_sha256"]) != configuration.policy_sha256
            or listing["extractor_version"] != configuration.extractor_version
            or not configuration.accepts(listing["profile_url"])):
        raise Conflict("Discovery job does not match this reviewed listing profile")


def execute(box, transport, configuration, job_uuid, *, fetcher=fetch):
    """configuration=None only delivers saved work; it cannot claim or fetch."""
    identifier(job_uuid)
    if (box.endpoint, box.producer) != (transport.endpoint, transport.producer):
        raise Conflict("Discovery client and outbox identify different producers")
    journal, client = DiscoveryJournal(box), DiscoveryClient(transport)
    with journal.execution() as owned:
        if not owned:
            return _result(journal, job_uuid, "waiting")
        value = journal.find(job_uuid)
        if value is not None and value.phase != "active":
            state = "page_delivered" if value.phase == "completed" else value.phase
            return _result(journal, job_uuid, state, journal.summary(job_uuid)["receipt"])
        lease = None
        try:
            client.capabilities()
            if value is not None and value.state["pending"] is not None:
                try:
                    value, outcome = _deliver(journal, client, value)
                    return _result(journal, job_uuid, outcome, value.state["receipt"])
                except Unavailable as error:
                    if error.status != 409:
                        raise
                    if value.state["pending"]["kind"] == "failure":
                        journal.note(value, "failure_requires_review", review=True)
                        return _result(journal, job_uuid, "review")
            current = client.describe(job_uuid)
            if value is None and configuration is None:
                return _result(journal, job_uuid, "not_recorded")
            job = current["job"]
            if value is None and job["state"] in ("succeeded", "failed", "cancelled"):
                state = "page_delivered" if job["state"] == "succeeded" else "failed"
                return _result(journal, job_uuid, state, current["receipt"])
            if value is None:
                _configuration(configuration, current)
            value = journal.prepare(current)
            if job["state"] in ("succeeded", "failed", "cancelled"):
                if value.body is not None or value.state["pending"] is not None:
                    journal.note(value, "unacknowledged_source_evidence", review=True)
                    return _result(journal, job_uuid, "review")
                journal.observe_terminal(value, current)
                state = "page_delivered" if job["state"] == "succeeded" else "failed"
                return _result(journal, job_uuid, state, current["receipt"])
            if configuration is None:
                return _result(journal, job_uuid, "ownership_required" if value.state["pending"] else "waiting")
            _configuration(configuration, current)
            claim = value.state["claim"]
            if job["state"] == "running" and (claim is None or job.get("owner_uuid") != claim["owner_uuid"]):
                return _result(journal, job_uuid, "waiting")
            value = journal.claim(value, current)
            claim = value.state["claim"]
            lease = JobLease.claim(client, claim["description"], owner=claim["owner_uuid"], seconds=180)
            if lease is None:
                return _result(journal, job_uuid, "waiting")
            value = journal.claimed(value, lease.job)
            lease.start()
            if value.state["pending"] is not None:
                # A definitive conflict followed by a new owned attempt is the
                # only execution path that rebinds retained bytes. Never refetch
                # or alter the original source observations to fit a successor.
                value = journal.rebind_page(value, lease.job)
                try:
                    value, outcome = _deliver(journal, client, value)
                    return _result(journal, job_uuid, outcome, value.state["receipt"])
                except Unavailable as error:
                    if error.status == 409:
                        journal.note(value, "page_requires_review", review=True)
                        return _result(journal, job_uuid, "review")
                    raise

            def check():
                lease.check()
                configuration.check()

            try:
                value = journal.reserve(value)
            except Capacity:
                value = journal.failure(value, lease.job, "worker_failed")
                _deliver(journal, client, value)
                return _result(journal, job_uuid, "capacity")
            result = fetcher(current["listing"]["profile_url"], configuration.settings(), current["cursor"],
                             check=check, reserve_source=lease.reserve_source)
            if (isinstance(result, dict) and set(result) == {"error"}
                    and isinstance(result["error"], str) and result["error"] in ERRORS):
                value = journal.failure(value, lease.job, result["error"])
            else:
                # Save returned evidence before checking the possibly expired
                # lease. A late renewal failure must not erase a fetched page.
                value = journal.page(value, lease.job, result)
            lease.check()
            value, outcome = _deliver(journal, client, value)
            return _result(journal, job_uuid, outcome, value.state["receipt"])
        except Unavailable as error:
            value = journal.find(job_uuid)
            if value is not None and value.phase == "active":
                value = journal.note(value, "native_delivery_unavailable", review=error.status in {400, 404, 413, 422})
            return _result(journal, job_uuid, "review" if value is not None and value.phase == "review" else "delivery_pending")
        except SourcePaused:
            value = journal.find(job_uuid)
            if value is not None and value.phase == "active":
                journal.note(value, "source_ownership_unavailable")
            return _result(journal, job_uuid, "waiting")
        finally:
            if lease is not None:
                lease.close()
            value = journal.find(job_uuid)
            if value is not None and value.phase == "active" and value.body is None and value.reserved_bytes:
                journal.release_reservation(value)
