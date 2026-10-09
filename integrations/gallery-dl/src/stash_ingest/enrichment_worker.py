"""Execute or recover one enrichment job without losing returned observations."""

from .client import Unavailable
from .encoding import InvalidData, decode, identifier
from .enrichment_client import EnrichmentClient
from .enrichment_journal import EnrichmentJournal
from .job_lease import JobLease
from .metadata_bundle import ERRORS, MAX_BYTES
from .metadata_fetch import fetch
from .outbox import Capacity, Conflict
from .runs import SourcePaused
from .worker_policy import execution_policy


def _deliver(journal, client, value):
    # A checkpoint acknowledgement atomically stages publication or the precise
    # child failure. Process death cannot turn a lost failure into an eager retry.
    for _ in range(2):
        pending = value.state["pending"]
        if pending is None:
            return value, None
        kind, lease = pending["kind"], pending["lease"]
        if kind == "checkpoint":
            result = client.checkpoint(value.job_uuid, lease, pending["expected_revision"],
                                       decode(value.body, MAX_BYTES, preserve_numbers=True))
        elif kind == journal.completion_intent:
            result = client.complete(value.job_uuid, lease, pending["receipt"])
        elif kind == "failure":
            result = client.fail(value.job_uuid, lease, pending["error_code"])
        else:
            raise InvalidData("Unknown enrichment delivery intent")
        value = journal.acknowledged(value, result)
        if kind == journal.completion_intent:
            return value, "completed"
        if kind == "failure":
            return value, "failed" if result["outcome"] == "failed" else "retry"
    raise InvalidData("Enrichment delivery did not reach a bounded outcome")


def _note(journal, value, code, *, review=False):
    return journal.change(value, state={**value.state, "error_code": code},
                          phase="review" if review else value.phase)


def _result(journal, job, state):
    return {"state": state, "job_uuid": job, "execution": journal.summary(job)}


def execute(box, transport, configuration, job_uuid, *, fetcher=fetch):
    """configuration=None delivers persisted intents but never claims/fetches."""
    return execute_metadata(box, transport, configuration, job_uuid, EnrichmentJournal, EnrichmentClient, fetcher=fetcher)


def execute_metadata(box, transport, configuration, job_uuid, journal_type, client_type, *, fetcher):
    """Share ownership and durable delivery, with kind-specific receipt validation."""
    identifier(job_uuid)
    if (box.endpoint, box.producer) != (transport.endpoint, transport.producer):
        raise Conflict("Enrichment client and outbox identify different producers")
    journal, client = journal_type(box), client_type(transport)
    with journal.execution() as owned:
        if not owned:
            return _result(journal, job_uuid, "waiting")
        value = journal.find(job_uuid)
        if value is not None and value.phase != "active":
            return _result(journal, job_uuid, value.phase)
        lease = None
        try:
            client.capabilities()
            if value is not None and value.state["pending"] is not None:
                try:
                    value, outcome = _deliver(journal, client, value)
                    if outcome:
                        return _result(journal, job_uuid, outcome)
                except Unavailable as error:
                    # The checkpoint may have committed even when a later
                    # publication/failure reply was lost. Reload durable state.
                    value = journal.find(job_uuid)
                    if error.status != 409:
                        raise
            current = client.describe(job_uuid)
            if value is None and configuration is None:
                return _result(journal, job_uuid, "not_recorded")
            definition = journal.definition(current)
            if value is None and current["job"]["state"] not in ("succeeded", "failed", "cancelled"):
                _configuration(configuration, current["job"], definition["url"])
            value = journal.prepare(current)
            job = current["job"]
            if job["state"] == "succeeded":
                if value.body is not None:
                    _note(journal, value, "unacknowledged_source_evidence", review=True)
                    return _result(journal, job_uuid, "review")
                publication = client.completion(job_uuid)
                if publication is None:
                    raise Unavailable("publication_unconfirmed")
                journal.change(value, phase="completed", state={**value.state, journal.completion_field: publication,
                               "pending": None, "error_code": ""}, reserved=0)
                return _result(journal, job_uuid, "completed")
            if job["state"] in ("failed", "cancelled"):
                if value.body is not None:
                    _note(journal, value, "unacknowledged_source_evidence", review=True)
                    return _result(journal, job_uuid, "review")
                journal.change(value, phase="failed", state={**value.state, "pending": None,
                    "native_outcome": job["state"], "error_code": "native_job_" + job["state"]}, reserved=0)
                return _result(journal, job_uuid, "failed")
            if configuration is None:
                return _result(journal, job_uuid, "ownership_required" if value.state["pending"] else "waiting")
            _configuration(configuration, job, value.definition["url"])
            claim = value.state["claim"]
            if job["state"] == "running" and (claim is None or job.get("owner_uuid") != claim["owner_uuid"]):
                return _result(journal, job_uuid, "waiting")
            value = journal.claim(value, job)
            claim = value.state["claim"]
            lease = JobLease.claim(client, claim["job"], owner=claim["owner_uuid"], seconds=180)
            if lease is None:
                return _result(journal, job_uuid, "waiting")
            value = journal.claimed(value, lease.job)
            lease.start()
            # A definitive old-attempt conflict followed by a current claim can
            # hand off unacknowledged bytes within this same producer. Conditional
            # revision/prefix checks still belong to the server; never rewrite
            # old observations to fit a divergent successor checkpoint.
            pending = value.state["pending"]
            if pending is not None and pending["kind"] == "checkpoint":
                value = journal.change(value, state={**value.state, "pending": {
                    **pending, "lease": client.lease(lease.job)}})
                try:
                    value, outcome = _deliver(journal, client, value)
                    return _result(journal, job_uuid, outcome)
                except Unavailable as error:
                    value = journal.find(job_uuid)
                    if error.status == 409 and value.body is not None:
                        _note(journal, value, "checkpoint_requires_review", review=True)
                        return _result(journal, job_uuid, "review")
                    raise
            if pending is not None:
                # Failed publication/failure intent had no unacknowledged source
                # body. Its ended native attempt and retained head are now the
                # authority for this newly claimed attempt.
                value = journal.change(value, state={**value.state, "pending": None})

            def check():
                lease.check()
                configuration.check()

            head = client.head(job_uuid, value.definition["url"], configuration.extractor_version)
            resume = head["body"] if head else client.seed(lease.job, value.definition["url"])
            if head is not None and head["pending_count"] == 0:
                value = journal.intent(value, lease.job, journal.completion_intent, {"receipt": {k: v for k, v in head.items() if k != "body"}})
            else:
                try:
                    value = journal.reserve(value)
                except Capacity:
                    value = journal.intent(value, lease.job, "failure", {"error_code": "worker_failed"})
                    _deliver(journal, client, value)
                    return _result(journal, job_uuid, "capacity")
                result = fetcher(value.definition["url"], configuration.settings(),
                                 resume=resume, check=check,
                                 reserve_source=lease.reserve_source)
                if (isinstance(result, dict) and set(result) == {"error"}
                        and isinstance(result["error"], str) and result["error"] in ERRORS):
                    value = journal.intent(value, lease.job, "failure", {"error_code": result["error"]})
                else:
                    # Persist first, then inspect possibly expired ownership.
                    # A late network/lease failure must not erase returned data.
                    value = journal.checkpoint(value, lease.job, head["revision"] if head else 0, result)
                lease.check()
            _, outcome = _deliver(journal, client, value)
            return _result(journal, job_uuid, outcome)
        except Unavailable as error:
            value = journal.find(job_uuid)
            if value is not None and value.phase == "active":
                value = _note(journal, value, "native_delivery_unavailable", review=error.status in {400, 404, 413, 422})
            return _result(journal, job_uuid, "review" if value is not None and value.phase == "review" else "delivery_pending")
        except SourcePaused:
            value = journal.find(job_uuid)
            if value is not None and value.phase == "active":
                _note(journal, value, "source_ownership_unavailable")
            return _result(journal, job_uuid, "waiting")
        finally:
            if lease is not None:
                lease.close()
            value = journal.find(job_uuid)
            if value is not None and value.phase == "active" and value.body is None and value.reserved_bytes:
                journal.change(value, reserved=0)


def _configuration(configuration, job, url):
    configuration.check()
    if (execution_policy(job, job["arguments"]["policy_sha256"]) != configuration.policy_sha256
            or job["arguments"]["extractor_version"] != configuration.extractor_version
            or not configuration.accepts(url)):
        raise Conflict("Enrichment job does not match this reviewed metadata profile")
