"""Exercise native listing page receipts over the actual producer transport."""

import hashlib
import json
import sys

from stash_ingest.client import Client, Unavailable
from stash_ingest.discovery_client import DiscoveryClient, page_bytes
from stash_ingest.discovery_fetch import validate_page
from stash_ingest.encoding import decode
from stash_ingest.job_lease import JobLease
from stash_ingest.metadata_bundle import Bundle


setup = json.load(sys.stdin)
client = DiscoveryClient(Client(setup["endpoint"], setup["producer"], timeout=30))
capabilities = client.capabilities()
assert capabilities["discovery_protocol"] == 1
assert capabilities["discovery_source_pacing_protocol"] == 1
assert "discovery_dispatch_protocol" not in capabilities
listing = setup["listing"]
admission = (listing["uuid"], listing["sha256"], listing["policy_sha256"], listing["extractor_version"])


def lost_once(operation):
    try:
        operation()
    except Unavailable as exc:
        assert exc.status == 503
    else:
        raise AssertionError("The committed acknowledgement was not lost")
    return operation()


job = lost_once(lambda: client.admit(*admission))
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
owner = "12519d09-6b84-4f46-bd89-9a7848b6ee29"
lease = lost_once(lambda: JobLease.claim(client, description, owner=owner, seconds=180))
assert lease.job["fence"] == 1 and lease.job["owner_uuid"] == owner
lease.start()
try:
    lease.renew()
    assert lease.reserve_source(listing["profile_url"]) is True
    receipt = lost_once(lambda: client.append_page(description, lease.job, page))
finally:
    lease.close()
assert receipt["sha256"] == hashlib.sha256(raw).hexdigest()
assert receipt["job_uuid"] == job["uuid"] and receipt["producer_uuid"] == setup["producer"]
assert receipt["ordinal"] == 1 and receipt["fence"] == 1 and receipt["record_count"] == 3
assert receipt["complete"] is False
assert client.describe(job["uuid"])["receipt"] == receipt
assert client.append_page(description, lease.job, page) == receipt

following = client.admit(*admission)
assert following["uuid"] != job["uuid"]
next_description = client.describe(following["uuid"])
assert next_description["cursor"] == page["next_cursor"]
next_lease = JobLease.claim(client, next_description, owner=owner, seconds=180)
next_lease.start()
try:
    failed = lost_once(lambda: client.fail(following["uuid"], next_lease.job, "pagination_stalled"))
finally:
    next_lease.close()
assert failed["outcome"] == "failed" and failed["job_uuid"] == following["uuid"]
assert client.describe(following["uuid"])["job"]["state"] == "failed"
assert client.describe(job["uuid"])["receipt"] == receipt
print(json.dumps({"receipt": receipt, "failed_job": following["uuid"], "page_bytes": len(raw)}))
