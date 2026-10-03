"""Resume a reviewed retained seed, then recover delivery in fresh processes."""

from copy import deepcopy
import json
from pathlib import Path
import sys
from unittest.mock import patch

from stash_ingest.client import Client, Unavailable
from stash_ingest.encoding import decode, encode
from stash_ingest.enrichment_client import EnrichmentClient, checkpoint_bytes
from stash_ingest.enrichment_configuration import EnrichmentConfiguration
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.enrichment_worker import execute
from stash_ingest.outbox import Outbox


setup = json.load(sys.stdin)
directory = Path(setup["directory"])
transport = Client(setup["endpoint"], setup["producer"], timeout=30)
client = EnrichmentClient(transport)
profile = EnrichmentConfiguration.from_document(setup["profile"], directory) if setup["step"] == 0 else None
job_path = directory / "handoff-job.json"
if setup["step"] == 0:
    try:
        client.admit_handoff(setup["handoff"], setup["plan"], profile.policy_sha256, profile.extractor_version)
        raise AssertionError("committed admission reply should be lost")
    except Unavailable:
        pass
    job = client.admit_handoff(setup["handoff"], setup["plan"], profile.policy_sha256, profile.extractor_version)
    assert job["arguments"]["version"] == 2 and job["arguments"]["handoff"]["uuid"] == setup["handoff"]
    job_path.write_bytes(encode(job))
job = decode(job_path.read_bytes())
count_path = directory / "handoff-fetches.txt"


def fetched(url, settings, *, resume, check, reserve_source):
    assert setup["step"] == 0, "delivery recovery cannot refetch source metadata"
    check()
    assert checkpoint_bytes(resume) == checkpoint_bytes(setup["seed"]["body"])
    assert resume["records"][0]["patch"]["author"]["id"] == 9007199254740993
    assert resume["records"][0]["observed_at"] is None
    assert not count_path.exists(), "the retained root must never be fetched again"
    child, = resume["pending"]
    assert reserve_source(child["url"])
    count_path.write_text("1")
    result = deepcopy(resume)
    result["records"].append({"kind": "media", "base": None, "parent": child["parent"], "removed": [],
                              "observed_at": "2026-10-03T12:00:00Z", "patch": {"category": "redgifs", "id": "pending",
                              "source_extractor_url": child["url"], "_url": "https://media.invalid/full.mp4"}})
    result["pending"] = []
    return result


box = Outbox(directory / "handoff-outbox.sqlite", transport.endpoint, transport.producer)
try:
    with patch("requests.sessions.Session.request", side_effect=AssertionError("No website requests in this fixture")):
        result = execute(box, transport, profile, job["uuid"], fetcher=fetched)
    value = EnrichmentJournal(box).find(job["uuid"])
    assert result["state"] == setup["expected"], result
    if setup["step"] == 0:
        assert value.body is not None and value.state["pending"]["kind"] == "checkpoint"
        assert b"9007199254740993" in value.body and b'"observed_at":null' in value.body
    elif setup["step"] == 1:
        assert value.body is None and value.state["pending"]["kind"] == "publish"
    else:
        assert value.body is None and value.reserved_bytes == 0 and value.phase == "completed"
        final = client.describe(job["uuid"])["job"]
        assert final["state"] == "succeeded" and final["fence"] == 1
    print(json.dumps({"state": result["state"], "fetches": int(count_path.read_text())}))
finally:
    box.close()
