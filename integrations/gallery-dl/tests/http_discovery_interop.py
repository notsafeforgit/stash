"""Exercise native listing page receipts over the actual producer transport."""

from contextlib import closing, contextmanager
import hashlib
import json
from pathlib import Path
import sys
import tempfile

from stash_ingest.client import Client, Unavailable
from stash_ingest.discovery_client import DiscoveryClient, page_bytes
from stash_ingest.discovery_fetch import validate_page
from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.encoding import decode
from stash_ingest.job_lease import JobLease
from stash_ingest.metadata_bundle import Bundle, MAX_BYTES
from stash_ingest.outbox import Outbox


setup = json.load(sys.stdin)
client = DiscoveryClient(Client(setup["endpoint"], setup["producer"], timeout=30))
capabilities = client.capabilities(readiness=True)
assert capabilities["discovery_protocol"] == 1
assert capabilities["discovery_source_pacing_protocol"] == 1
assert capabilities["discovery_dispatch_protocol"] == 1
listing = setup["listing"]
admission = (listing["uuid"], listing["sha256"], listing["policy_sha256"], listing["extractor_version"])
readiness = (listing["collection_uuid"], listing["policy_sha256"], listing["extractor_version"])
candidates = client.ready_listings(*readiness, limit=1)
assert candidates == {"listings": [{"uuid": listing["uuid"], "definition_sha256": listing["sha256"]}],
    "after": listing["uuid"], "has_more": False}
assert client.ready_jobs(*readiness) == []
directory = tempfile.TemporaryDirectory()
outbox_path = Path(directory.name) / "outbox.sqlite"


@contextmanager
def reopened_journal():
    # Every operation reopens durable state. A lost native acknowledgement
    # cannot rely on the previous connection or an in-memory owner/body.
    with closing(Outbox(outbox_path, setup["endpoint"], setup["producer"])) as box:
        journal = DiscoveryJournal(box)
        with journal.execution() as owned:
            assert owned
            yield journal


def claim_native(description):
    with reopened_journal() as journal:
        value = journal.prepare(description)
        value = journal.claim(value, description)
        saved = value.state["claim"]
        lease = JobLease.claim(client, saved["description"], owner=saved["owner_uuid"], seconds=180)
        assert lease.job["fence"] == 1 and lease.job["owner_uuid"] == saved["owner_uuid"]
        journal.claimed(value, lease.job)
        return lease


def deliver(job_uuid):
    with reopened_journal() as journal:
        value = journal.find(job_uuid)
        pending = value.state["pending"]
        if pending["kind"] == "page":
            assert value.body == raw
            receipt = client.append_page(value.state["claim"]["description"], pending["lease"],
                decode(value.body, MAX_BYTES, preserve_numbers=True))
        else:
            receipt = client.fail(job_uuid, pending["lease"], pending["error_code"])
        done = journal.acknowledged(value, receipt)
        assert done.body is None and done.reserved_bytes == 0
        return receipt


def lost_once(operation):
    try:
        operation()
    except Unavailable as exc:
        assert exc.status == 503
    else:
        raise AssertionError("The committed acknowledgement was not lost")
    return operation()


job = lost_once(lambda: client.admit(*admission))
assert client.ready_listings(*readiness)["listings"] == []
assert client.ready_jobs(*readiness) == [{"uuid": job["uuid"], "sequence": job["sequence"]}]
description = client.describe(job["uuid"])
assert description["cursor"] is None and description["receipt"] is None
assert description["listing"] == listing
bundle = Bundle(listing["profile_url"], listing["extractor_version"], max_records=4096)
numbers = decode(b'{"decimal":1.2300e+05,"precise":0.123456789012345678901234567890,"zero":-0}', preserve_numbers=True)
for number in range(3):
    bundle.append("media", {"category": "reddit", "id": "abc123", "num": number + 1,
        "source_extractor_url": listing["profile_url"], "_url": "https://media.invalid/" + str(number) + ".jpg",
        "retained_text": "<>&" * 550000, "caption": "Unicode \u2028 \u2029 🙂", **numbers})
page = validate_page({"schema": "stash-discovery-page-v1", "retention_policy": capabilities["retention_policy"],
    "url": listing["profile_url"], "extractor_version": listing["extractor_version"],
    "cursor": None, "next_cursor": {"after": "t3_abc123"}, "complete": False,
    "records": bundle.checkpoint()["records"]}, listing["profile_url"], listing["extractor_version"])
raw = page_bytes(page)
assert len(raw) > 4 << 20
lease = lost_once(lambda: claim_native(description))
lease.start()
try:
    lease.renew()
    assert lease.reserve_source(listing["profile_url"]) is True
    with reopened_journal() as journal:
        value = journal.reserve(journal.find(job["uuid"]))
        journal.page(value, lease.job, page)
    receipt = lost_once(lambda: deliver(job["uuid"]))
finally:
    lease.close()
assert receipt["sha256"] == hashlib.sha256(raw).hexdigest()
assert receipt["job_uuid"] == job["uuid"] and receipt["producer_uuid"] == setup["producer"]
assert receipt["ordinal"] == 1 and receipt["fence"] == 1 and receipt["record_count"] == 3
assert receipt["complete"] is False
assert client.describe(job["uuid"])["receipt"] == receipt
assert client.append_page(description, lease.job, page) == receipt
assert client.ready_jobs(*readiness) == []
assert client.ready_listings(*readiness)["listings"] == candidates["listings"]

following = client.admit(*admission)
assert following["uuid"] != job["uuid"]
next_description = client.describe(following["uuid"])
assert next_description["cursor"] == page["next_cursor"]
next_lease = claim_native(next_description)
next_lease.start()
try:
    with reopened_journal() as journal:
        value = journal.find(following["uuid"])
        journal.failure(value, next_lease.job, "pagination_stalled")
    failed = lost_once(lambda: deliver(following["uuid"]))
finally:
    next_lease.close()
assert failed["outcome"] == "failed" and failed["job_uuid"] == following["uuid"]
assert client.describe(following["uuid"])["job"]["state"] == "failed"
assert client.ready_jobs(*readiness) == []
assert client.ready_listings(*readiness)["listings"] == []
assert client.describe(job["uuid"])["receipt"] == receipt
with reopened_journal() as journal:
    assert journal.find(job["uuid"]).state["receipt"] == receipt
    assert journal.find(following["uuid"]).state["failure"] == failed
    assert journal.summary()["staged_bytes"] == journal.summary()["reserved_bytes"] == 0
directory.cleanup()
print(json.dumps({"receipt": receipt, "failed_job": following["uuid"], "page_bytes": len(raw)}))
