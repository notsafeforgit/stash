from copy import deepcopy
import hashlib
from pathlib import Path
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.discovery_client import DiscoveryClient, LISTING_KEYS, MAX_BYTES, page_bytes
from stash_ingest.encoding import InvalidData, decode, native_json


def execution_fixture():
    fixture = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/discovery-pages-v1.json"
    page = decode(fixture.read_bytes(), preserve_numbers=True)["pages"][1]["page"]
    when = "2026-10-04T14:00:00Z"
    listing = {"uuid": str(uuid.uuid4()), "account_uuid": str(uuid.uuid4()),
        "collection_uuid": str(uuid.uuid4()), "collection_revision": 1, "root_uuid": None,
        "profile_url": page["url"], "policy_sha256": "a" * 64,
        "extractor_version": page["extractor_version"], "initial_cursor": None,
        "historical_pages": 0, "legacy": None, "not_before": when, "created_at": when}
    listing["sha256"] = hashlib.sha256(native_json({k: listing[k] for k in LISTING_KEYS}, 32768)).hexdigest()
    job = {"uuid": str(uuid.uuid4()), "kind": "account.list_page", "state": "queued", "revision": 1,
        "fence": 0, "max_attempts": 8, "available_at": when, "created_at": when, "updated_at": when,
        "result": {}, "arguments": {"version": 1, "listing_uuid": listing["uuid"], "generation": 1,
            "page_ordinal": 1, "definition_sha256": listing["sha256"], "collection_uuid": listing["collection_uuid"]}}
    return {"job": job, "listing": listing, "cursor": None, "receipt": None}, page


class DiscoveryClientTests(unittest.TestCase):
    def setUp(self):
        self.description, self.page = execution_fixture()
        self.job, self.listing = self.description["job"], self.description["listing"]
        self.when = self.listing["created_at"]
        self.transport = Mock(producer=str(uuid.uuid4()), timeout=15)
        self.client = DiscoveryClient(self.transport)
        self.owner = str(uuid.uuid4())
        self.lease = {"owner_uuid": self.owner, "fence": 1}
        self.receipt = {"job_uuid": self.job["uuid"], "listing_uuid": self.listing["uuid"], "ordinal": 1,
            "fence": 1, "producer_uuid": self.transport.producer, "sha256": hashlib.sha256(page_bytes(self.page)).hexdigest(),
            "record_count": len(self.page["records"]), "complete": self.page["complete"], "created_at": self.when}

    @staticmethod
    def digest(listing):
        return hashlib.sha256(native_json({k: listing[k] for k in LISTING_KEYS}, 32768)).hexdigest()

    def test_capabilities_require_the_exact_protocol_and_full_page_capacity(self):
        good = {"discovery_protocol": 1, "discovery_source_pacing_protocol": 1, "max_discovery_page_bytes": MAX_BYTES}
        self.transport.capabilities.return_value = good
        self.assertEqual(self.client.capabilities(), good)
        for change in ({"discovery_protocol": True}, {"discovery_protocol": 2},
                       {"discovery_source_pacing_protocol": None}, {"discovery_source_pacing_protocol": True},
                       {"max_discovery_page_bytes": MAX_BYTES - 1}):
            self.transport.capabilities.return_value = {**good, **change}
            with self.assertRaises(Unavailable):
                self.client.capabilities()

    def test_description_checks_definition_digest_and_original_cursor_binding(self):
        self.transport._request.return_value = self.description
        self.assertEqual(self.client.describe(self.job["uuid"]), self.description)
        for change in ({"profile_url": "https://reddit.com/user/other/submitted/"},
                       {"root_uuid": str(uuid.uuid4())}, {"not_before": "2026-10-04T14:00:00.000000001Z"},
                       {"account_uuid": str(uuid.uuid4())}, {"extra": None}, {"historical_pages": 67},
                       {"collection_revision": True}, {"legacy": {"snapshot_uuid": str(uuid.uuid4()), "account_ordinal": 0}}):
            value = deepcopy(self.description)
            value["listing"].update(change)
            self.transport._request.return_value = value
            with self.subTest(change=change), self.assertRaises(Unavailable):
                self.client.describe(self.job["uuid"])
        for change in ({"cursor": {"after": "t3_other"}}, {"receipt": self.receipt}, {"extra": None}):
            self.transport._request.return_value = {**self.description, **change}
            with self.assertRaises(Unavailable):
                self.client.describe(self.job["uuid"])
        retained = deepcopy(self.description)
        retained["listing"].update(initial_cursor={"after": "t3_saved"}, historical_pages=67,
            legacy={"snapshot_uuid": str(uuid.uuid4()), "account_ordinal": 12})
        retained["listing"]["sha256"] = self.digest(retained["listing"])
        retained["job"]["arguments"]["definition_sha256"] = retained["listing"]["sha256"]
        retained["cursor"] = {"after": "t3_saved"}
        self.transport._request.return_value = retained
        self.assertEqual(self.client.describe(self.job["uuid"]), retained)

    def test_admission_and_claim_cannot_switch_the_listing_page_or_attempt(self):
        self.transport._request.return_value = self.job
        args = (self.listing["uuid"], self.listing["sha256"], self.listing["policy_sha256"], self.listing["extractor_version"])
        self.assertEqual(self.client.admit(*args), self.job)
        bad = deepcopy(self.job)
        bad["arguments"]["listing_uuid"] = str(uuid.uuid4())
        self.transport._request.return_value = bad
        with self.assertRaises(Unavailable):
            self.client.admit(*args)
        running = {**self.job, **self.lease, "state": "running", "revision": 2, "lease_until": "2026-10-04T14:03:00Z"}
        response = (running, "Sun, 04 Oct 2026 14:00:00 GMT", 100.0)
        self.transport._request.return_value = response
        self.assertEqual(self.client.claim(self.description, self.owner), response)
        request = decode(self.transport._request.call_args.args[2])
        self.assertEqual(request["policy_sha256"], self.listing["policy_sha256"])
        self.assertEqual(request["extractor_version"], self.listing["extractor_version"])
        for change in ({"owner_uuid": str(uuid.uuid4())}, {"fence": 2}, {"fence": True}, {"state": "queued"},
                       {"arguments": {**self.job["arguments"], "page_ordinal": 2}}):
            self.transport._request.return_value = ({**running, **change}, response[1], response[2])
            with self.assertRaises(Unavailable):
                self.client.claim(self.description, self.owner)
        self.transport._request.return_value = (None, response[1], response[2])
        self.assertEqual(self.client.claim(self.description, self.owner)[0], None)
        for seconds in (True, 4, 901):
            with self.assertRaises(InvalidData):
                self.client.claim(self.description, self.owner, seconds)
        self.transport._request.return_value = response
        self.assertEqual(self.client.renew(running), response)
        self.transport._request.return_value = ({**running, "fence": 2}, response[1], response[2])
        with self.assertRaises(Unavailable):
            self.client.renew(running)

    def test_page_acknowledgement_binds_exact_bytes_identity_and_original_fence(self):
        self.transport._request.return_value = self.receipt
        self.assertEqual(self.client.append_page(self.description, self.lease, self.page), self.receipt)
        request = self.transport._request.call_args.args[2]
        self.assertIn(b'"body":{', request)
        sent = decode(request, MAX_BYTES + 4096, preserve_numbers=True)
        self.assertEqual(page_bytes(sent["body"]), page_bytes(self.page))
        self.assertNotIn("producer_uuid", sent)
        for change in ({"job_uuid": str(uuid.uuid4())}, {"listing_uuid": str(uuid.uuid4())},
                       {"producer_uuid": str(uuid.uuid4())}, {"ordinal": 2}, {"ordinal": True},
                       {"fence": 2}, {"sha256": "b" * 64}, {"record_count": 0}, {"complete": True},
                       {"created_at": "not a time"}):
            self.transport._request.return_value = {**self.receipt, **change}
            with self.subTest(change=change), self.assertRaises(Unavailable):
                self.client.append_page(self.description, self.lease, self.page)
        self.transport._request.reset_mock()
        altered = deepcopy(self.page)
        altered["cursor"] = {"after": "t3_unrequested"}
        with self.assertRaises(InvalidData):
            self.client.append_page(self.description, self.lease, altered)
        self.transport._request.assert_not_called()

    def test_success_requires_its_own_retained_receipt(self):
        done = deepcopy(self.description)
        done["job"].update(state="succeeded", fence=1, revision=3, result={"listing_uuid": self.listing["uuid"],
            "page_ordinal": 1, "page_sha256": self.receipt["sha256"]})
        done["receipt"] = self.receipt
        self.transport._request.return_value = done
        self.assertEqual(self.client.describe(self.job["uuid"]), done)
        for change in ({"fence": 2}, {"job_uuid": str(uuid.uuid4())}, {"sha256": "c" * 64}):
            self.transport._request.return_value = {**done, "receipt": {**self.receipt, **change}}
            with self.assertRaises(Unavailable):
                self.client.describe(self.job["uuid"])
        self.transport._request.return_value = {**done, "receipt": None}
        with self.assertRaises(Unavailable):
            self.client.describe(self.job["uuid"])

    def test_failure_and_source_receipts_cannot_cross_ownership(self):
        failure = {"job_uuid": self.job["uuid"], "producer_uuid": self.transport.producer, **self.lease,
            "outcome": "retry", "error_code": "rate_limited", "started_at": self.when, "ended_at": self.when}
        self.transport._request.return_value = failure
        self.assertEqual(self.client.fail(self.job["uuid"], self.lease, "rate_limited"), failure)
        for change in ({"producer_uuid": str(uuid.uuid4())}, {"owner_uuid": str(uuid.uuid4())},
                       {"fence": 2}, {"outcome": "failed"}, {"ended_at": None}, {"error_code": "timeout"}):
            self.transport._request.return_value = {**failure, **change}
            with self.assertRaises(Unavailable):
                self.client.fail(self.job["uuid"], self.lease, "rate_limited")
        self.transport._request.return_value = {**failure, "fence": 8, "outcome": "failed"}
        self.client.fail(self.job["uuid"], {**self.lease, "fence": 8}, "rate_limited")
        with self.assertRaises(InvalidData):
            self.client.fail(self.job["uuid"], self.lease, "private worker output")
        running = {**self.job, **self.lease, "state": "running", "lease_until": "2026-10-04T14:03:00Z"}
        ready = {"job_uuid": self.job["uuid"], "fence": 1, "ready": False}
        self.transport._request.return_value = ready
        self.assertFalse(self.client.reserve_source(running, self.listing["profile_url"]))
        for change in ({"job_uuid": str(uuid.uuid4())}, {"fence": True}, {"fence": 2}, {"ready": 1}):
            self.transport._request.return_value = {**ready, **change}
            with self.assertRaises(Unavailable):
                self.client.reserve_source(running, self.listing["profile_url"])
