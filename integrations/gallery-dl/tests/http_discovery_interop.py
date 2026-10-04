"""Exercise native listing page receipts over the actual producer transport."""

import hashlib
import json
import sys

from stash_ingest.client import Client, Unavailable
from stash_ingest.discovery_fetch import validate_page
from stash_ingest.encoding import decode, encode
from stash_ingest.enrichment_client import checkpoint_bytes
from stash_ingest.metadata_bundle import Bundle


setup = json.load(sys.stdin)
client = Client(setup["endpoint"], setup["producer"], timeout=30)
capabilities = client.capabilities()
assert capabilities["discovery_protocol"] == 1
assert capabilities["discovery_source_pacing_protocol"] == 1
assert "discovery_dispatch_protocol" not in capabilities
listing = setup["listing"]
prefix = "/discovery"
admission = encode({"expected_definition_sha256": listing["sha256"],
    "policy_sha256": listing["policy_sha256"], "extractor_version": listing["extractor_version"]})
admit_path = prefix + "/listings/" + listing["uuid"] + "/jobs"


def lost_once(operation):
    try:
        operation()
    except Unavailable as exc:
        assert exc.status == 503
    else:
        raise AssertionError("The committed acknowledgement was not lost")
    return operation()


job = lost_once(lambda: client._request("POST", admit_path, admission))
path = prefix + "/jobs/" + job["uuid"]
description = client._request("GET", path)
assert description["cursor"] is None and description["receipt"] is None
assert description["listing"] == listing
owner = "12519d09-6b84-4f46-bd89-9a7848b6ee29"
claim = encode({"expected_revision": job["revision"], "owner_uuid": owner,
    "policy_sha256": listing["policy_sha256"], "extractor_version": listing["extractor_version"], "lease_seconds": 180})
running = lost_once(lambda: client._request("POST", path + "/claim", claim))
assert running["fence"] == 1 and running["owner_uuid"] == owner
lease = {"owner_uuid": owner, "fence": running["fence"]}
reservation = client._request("POST", path + "/source", encode({**lease, "url": listing["profile_url"]}))
assert reservation["ready"] is True

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
raw = checkpoint_bytes(page)
assert len(raw) > 4 << 20
request = encode({**lease, "ordinal": 1})[:-1] + b',"body":' + raw + b'}'
receipt = lost_once(lambda: client._request("POST", path + "/page", request))
assert receipt["sha256"] == hashlib.sha256(raw).hexdigest()
assert receipt["job_uuid"] == job["uuid"] and receipt["producer_uuid"] == setup["producer"]
assert receipt["ordinal"] == 1 and receipt["fence"] == 1 and receipt["record_count"] == 3
assert receipt["complete"] is False
assert client._request("GET", path)["receipt"] == receipt
assert client._request("POST", path + "/page", request) == receipt

following = client._request("POST", admit_path, admission)
assert following["uuid"] != job["uuid"]
next_path = prefix + "/jobs/" + following["uuid"]
assert client._request("GET", next_path)["cursor"] == page["next_cursor"]
claim_next = encode({"expected_revision": following["revision"], "owner_uuid": owner,
    "policy_sha256": listing["policy_sha256"], "extractor_version": listing["extractor_version"], "lease_seconds": 180})
next_running = client._request("POST", next_path + "/claim", claim_next)
failed = lost_once(lambda: client._request("POST", next_path + "/failure", encode({"owner_uuid": owner,
    "fence": next_running["fence"], "error_code": "pagination_stalled"})))
assert failed["outcome"] == "failed" and failed["job_uuid"] == following["uuid"]
assert client._request("GET", next_path)["job"]["state"] == "failed"
assert client._request("GET", path)["receipt"] == receipt
print(json.dumps({"receipt": receipt, "failed_job": following["uuid"], "page_bytes": len(raw)}))
