"""Real native API fixture: bounded checkpoints and lost acknowledgements."""

import json
from pathlib import Path
import sys

from stash_ingest.client import Client, Unavailable
from stash_ingest.enrichment_client import EnrichmentClient, checkpoint_bytes
from stash_ingest.enrichment_lease import EnrichmentLease
from stash_ingest.encoding import decode
from stash_ingest.metadata_bundle import Bundle
from stash_ingest.runs import SourcePaused


setup = json.load(sys.stdin)
client = EnrichmentClient(Client(setup["endpoint"], setup["producer"], timeout=30))
client.capabilities()
targets = client.ready(setup["collection"])
assert len(targets) == 1 and targets[0]["uuid"] == setup["target"]
target = targets[0]
raw_fixture = Path(setup["fixture"]).read_bytes().replace(b'"legacy_field": "old"',
    b'"legacy_field": "old", "source_number": 1.2300e+02, "source_precision": 0.123456789012345678901234567890, "source_zero": -0')
fixture = decode(raw_fixture, preserve_numbers=True)
complete = fixture["complete"]
if setup["large"]:
    bundle = Bundle(target["url"], "1.32.15-dev", complete)
    for index in range(3):
        bundle.append("media", {"category": "reddit", "id": "abc123", "num": index + 10,
            "source_extractor_url": target["url"], "retained_text": "<>&" * 550000,
            "caption": "Unicode \u2028 \u2029 🙂", "fraction": 1e-7, "large_id": 9223372036854775815})
    complete = bundle.checkpoint()
    assert len(checkpoint_bytes(complete)) > 4 << 20


def lost_once(operation):
    try:
        operation()
    except Unavailable as exc:
        assert exc.status == 503
    else:
        raise AssertionError("fixture did not lose the acknowledgement")
    return operation()


job = lost_once(lambda: client.admit(target["uuid"], target["revision"], "a" * 64, "1.32.15-dev"))
assert client.ready(setup["collection"]) == []
description = client.describe(job["uuid"])
assert description["job"] == job and description["target"] == target
owner = "b0d97f9c-84f2-4e7d-b315-322d451bb966"
lease = lost_once(lambda: EnrichmentLease.claim(client, job, owner=owner, seconds=180))
lease.check()
lease.start()
lease.renew()
assert client.head(job["uuid"], target["url"], "1.32.15-dev") is None
initial = lost_once(lambda: client.checkpoint(job["uuid"], lease.job, 0, fixture["initial"]))
head = client.head(job["uuid"], target["url"], "1.32.15-dev")
assert {k: v for k, v in head.items() if k != "body"} == initial
assert head["body"] == fixture["initial"]
assert b'1.2300e+02' in checkpoint_bytes(head["body"])
assert b'0.123456789012345678901234567890' in checkpoint_bytes(head["body"])
assert b'"source_zero":-0' in checkpoint_bytes(head["body"])
try:
    client.publish(job["uuid"], lease.job, initial)
except Unavailable as exc:
    assert exc.status == 409
else:
    raise AssertionError("pending lookups must not complete a target")
assert client.publication(job["uuid"]) is None
ready = client.checkpoint(job["uuid"], lease.job, initial["revision"], complete)
assert client.head(job["uuid"], target["url"], "1.32.15-dev")["body"] == complete
publication = lost_once(lambda: client.publish(job["uuid"], lease.job, ready))
assert client.publication(job["uuid"]) == publication
assert client.head(job["uuid"], target["url"], "1.32.15-dev") is None
assert client.checkpoint(job["uuid"], lease.job, initial["revision"], complete) == ready
lease.close()
try:
    lease.check()
except SourcePaused:
    pass
else:
    raise AssertionError("closed ownership must not permit another fetch")
assert client.describe(job["uuid"])["job"]["state"] == "succeeded"
print(json.dumps({"job_uuid": job["uuid"], "publication": publication,
                  "checkpoint_bytes": len(checkpoint_bytes(complete))}))
