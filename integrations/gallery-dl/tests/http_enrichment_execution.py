"""Restartable selected-job execution against the actual native API."""

from contextlib import redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import sys
from unittest.mock import patch

from stash_ingest.cli import main
from stash_ingest.client import Client
from stash_ingest.encoding import decode, encode
from stash_ingest.enrichment_client import EnrichmentClient, checkpoint_bytes
from stash_ingest.enrichment_configuration import EnrichmentConfiguration, SCHEMA
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.job_lease import JobLease
from stash_ingest.enrichment_worker import execute
from stash_ingest.outbox import Outbox
from stash_ingest.runs import SourcePaused


setup = json.load(sys.stdin)
directory = Path(setup["directory"])
transport = Client(setup["endpoint"], setup["producer"], timeout=30)
client = EnrichmentClient(transport)
profile = None
if not setup["deliver_only"]:
    profile = EnrichmentConfiguration.from_document({"schema": SCHEMA, "source_category": "reddit",
        "gallery": {"extractor": {"sleep-request": 0, "sleep-extractor": 0, "reddit": {"cookies": "${stash:login}"}}},
        "bindings": {"login": {"kind": "private", "env": "ENRICHMENT_FIXTURE_LOGIN"}}}, directory)
job_path = directory / "job.json"
if not job_path.exists():
    target, = client.ready(setup["collection"])
    job = client.admit(target["uuid"], target["revision"], profile.policy_sha256, profile.extractor_version)
    job_path.write_bytes(encode(job))
job = decode(job_path.read_bytes())
raw = Path(setup["fixture"]).read_bytes().replace(b'"legacy_field": "old"',
    b'"legacy_field": "old", "source_fraction": 0.12345678901234567890, "source_zero": -0')
bodies = decode(raw, preserve_numbers=True)
count_path = directory / "fetches.txt"
source_paused = False
original_check = JobLease.check


def lease_check(lease):
    if source_paused:
        raise SourcePaused("Fixture ownership lost after source returned")
    original_check(lease)


def fetched(url, settings, *, resume, check, reserve_source):
    assert reserve_source(url)
    global source_paused
    assert setup["fetch"] != "forbidden", "recovery must not refetch original observations"
    check()
    assert url == bodies["complete"]["url"]
    assert settings["extractor"]["reddit"]["cookies"] == "fixture-private-site-token"
    assert (resume is not None) == setup["resumed"]
    if resume is not None:
        assert checkpoint_bytes(resume) == checkpoint_bytes(bodies["initial"])
    count = int(count_path.read_text()) if count_path.exists() else 0
    count_path.write_text(str(count + 1))
    source_paused = setup["pause_after_fetch"]
    return deepcopy(bodies[setup["fetch"]])


box = Outbox(directory / "outbox.sqlite", transport.endpoint, transport.producer,
             max_bytes=setup.get("max_bytes", 512 << 20))
try:
    with (patch("requests.sessions.Session.request", side_effect=AssertionError("No website requests in native execution fixture")),
          patch.object(JobLease, "check", lease_check)):
        if setup["deliver_only"]:
            output = io.StringIO()
            with redirect_stdout(output):
                status = main(["--outbox", str(box.path), "--endpoint", transport.endpoint, "--producer", transport.producer,
                               "deliver-enrichment", job["uuid"]])
            result = json.loads(output.getvalue())
            assert status == (0 if result["state"] == "completed" else 2)
        else:
            result = execute(box, transport, profile, job["uuid"], fetcher=fetched)
    journal = EnrichmentJournal(box)
    value = journal.find(job["uuid"])
    if value.body is not None:
        assert value.body == checkpoint_bytes(bodies["complete"])
    assert "fixture-private-site-token" not in "\n".join(box.db.iterdump())
    assert result["state"] == setup["expected"], result
    print(json.dumps({"job_uuid": job["uuid"], "state": result["state"], "journal": journal.summary(job["uuid"]),
                      "fetches": int(count_path.read_text()) if count_path.exists() else 0}))
finally:
    box.close()
