from copy import deepcopy
from datetime import datetime, timedelta, timezone
from email.utils import format_datetime
import hashlib
from pathlib import Path
import sys
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.client import Client, Unavailable
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.enrichment_client import EnrichmentClient, MAX_RESPONSE, CAPTURE_POLICY, checkpoint_bytes
from stash_ingest.job_lease import JobLease
from stash_ingest.metadata_bundle import Bundle, MAX_BYTES
from stash_ingest.metadata_fetch import _exchange
from stash_ingest.runs import SourcePaused


class EnrichmentClientTests(unittest.TestCase):
    def setUp(self):
        fixture = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
        self.body = decode(fixture.read_bytes())["initial"]
        self.transport = Mock(producer=str(uuid.uuid4()), timeout=15)
        self.client = EnrichmentClient(self.transport)
        self.job = {"uuid": str(uuid.uuid4()), "kind": "post.enrich", "state": "queued", "revision": 1,
            "fence": 0, "max_attempts": 8, "arguments": {"version": 1,
            "target_uuid": str(uuid.uuid4()), "target_revision": 1, "post_uuid": str(uuid.uuid4()),
            "collection_uuid": str(uuid.uuid4()), "collection_revision": 1, "root_uuid": None,
            "policy_sha256": "a" * 64, "extractor_version": "1.32.15-dev"}}
        self.owner = str(uuid.uuid4())
        self.lease = {"owner_uuid": self.owner, "fence": 2}
        self.receipt = {"job_uuid": self.job["uuid"], "revision": 1, "fence": 1,
            "sha256": hashlib.sha256(checkpoint_bytes(self.body)).hexdigest(),
            "record_count": 3, "pending_count": 1, "unresolved_count": 1}

    def test_capability_and_response_byte_limits(self):
        good = {"enrichment_protocol": 2, "max_enrichment_checkpoint_bytes": MAX_BYTES,
                "enrichment_source_pacing_protocol": 1}
        self.transport.capabilities.return_value = good
        self.client.capabilities()
        for invalid in ({"enrichment_protocol": True}, {"max_enrichment_checkpoint_bytes": MAX_BYTES - 1},
                        {"enrichment_source_pacing_protocol": None}, {"enrichment_source_pacing_protocol": True},
                        {"enrichment_source_pacing_protocol": 2}):
            self.transport.capabilities.return_value = dict(good, **invalid)
            with self.assertRaises(Unavailable):
                self.client.capabilities()
        actual = Client("http://localhost:8009", self.transport.producer)
        for limit in (0, True, MAX_RESPONSE + 1):
            with self.assertRaises(InvalidData):
                actual._request("GET", "/capabilities", max_response_bytes=limit)

    def test_retained_seed_pins_original_identity_and_rejects_replaced_bytes(self):
        body = deepcopy(self.body)
        record = body["records"][0]
        record.update(kind="context", observed_at=None, base=None, parent=None, retained_capture=str(uuid.uuid4()))
        record["patch"]["precise_source_id"] = 9007199254740993
        body.update(schema="stash-metadata-fetch-v2", records=[record], pending=[], unresolved=[])
        sha = hashlib.sha256(checkpoint_bytes(body)).hexdigest()
        handoff = {"uuid": str(uuid.uuid4()), "plan_sha256": "b" * 64, "seed_sha256": sha}
        self.job["arguments"].update(version=2, capture_policy=CAPTURE_POLICY, handoff=handoff)
        response = {"handoff_uuid": handoff["uuid"], "plan_sha256": handoff["plan_sha256"], "sha256": sha, "body": body}
        self.transport._request.return_value = response
        self.assertEqual(self.client.seed(self.job, body["url"]), body)
        self.assertTrue(self.transport._request.call_args.kwargs["preserve_numbers"])
        self.assertEqual(self.transport._request.call_args.kwargs["max_response_bytes"], MAX_RESPONSE)
        for key, value in (("handoff_uuid", str(uuid.uuid4())), ("plan_sha256", "c" * 64), ("sha256", "d" * 64)):
            self.transport._request.return_value = dict(response, **{key: value})
            with self.assertRaises(Unavailable):
                self.client.seed(self.job, body["url"])
        changed = deepcopy(body)
        changed["records"][0]["patch"]["precise_source_id"] = 9007199254740992
        self.transport._request.return_value = dict(response, body=changed)
        with self.assertRaises(Unavailable):
            self.client.seed(self.job, body["url"])
        for mutation in ({"capture_policy": "unknown"}, {"handoff": None}, {"version": 1}, {"version": True}):
            changed = deepcopy(self.job)
            changed["arguments"].update(mutation)
            with self.assertRaises(Unavailable):
                self.client._job(changed)

    def test_candidate_scope_duplicate_and_invalid_target_rejected(self):
        collection = self.job["arguments"]["collection_uuid"]
        target = {"uuid": self.job["arguments"]["target_uuid"], "revision": 1,
                  "state": "pending", "collection_uuid": collection, "url": self.body["url"],
                  "priority": 20, "not_before": "2026-10-03T00:00:00Z"}
        self.transport._request.return_value = [target]
        self.assertEqual(self.client.ready(collection), [target])
        for value in ([target, target], [dict(target, state="held")], [dict(target, revision=True)],
                      [dict(target, collection_uuid=str(uuid.uuid4()))], [dict(target, url="file:///secret")],
                      [dict(target, uuid=None)], None):
            self.transport._request.return_value = value
            with self.assertRaises(Unavailable):
                self.client.ready(collection)

    def test_admission_and_description_pin_scope_and_runtime(self):
        work = self.job["arguments"]
        self.transport._request.return_value = self.job
        self.assertEqual(self.client.admit(work["target_uuid"], 1, "a" * 64, "1.32.15-dev"), self.job)
        for key, invalid in (("uuid", "invalid"), ("revision", True), ("max_attempts", 9), ("kind", "media.verify"), ("state", {})):
            value = deepcopy(self.job)
            value[key] = invalid
            self.transport._request.return_value = value
            with self.assertRaises(Unavailable):
                self.client.admit(work["target_uuid"], 1, "a" * 64, "1.32.15-dev")
        target = {k: work[k] for k in ("post_uuid", "collection_uuid", "collection_revision")}
        target.update(uuid=work["target_uuid"], policy="gallery-dl-metadata-v1", url=self.body["url"])
        value = {"job": self.job, "target": target}
        self.transport._request.return_value = value
        self.assertEqual(self.client.describe(self.job["uuid"]), value)
        target["collection_revision"] += 1
        with self.assertRaises(Unavailable):
            self.client.describe(self.job["uuid"])

    def test_job_discovery_rejects_nonadvancing_duplicate_and_unbounded_pages(self):
        good = [{"sequence": 5, "uuid": str(uuid.uuid4())}, {"sequence": 6, "uuid": str(uuid.uuid4())}]
        self.transport._request.return_value = good
        self.assertEqual(self.client.ready_jobs(self.job["arguments"]["collection_uuid"], "a" * 64, "version", after=4), good)
        for value in (good[::-1], [{**good[0], "sequence": True}], [good[0], dict(good[1], uuid=good[0]["uuid"])],
                      [{**good[0], "extra": 1}], [{**good[0], "sequence": 4}], good * 20, {}):
            self.transport._request.return_value = value
            with self.assertRaises(Unavailable):
                self.client.ready_jobs(self.job["arguments"]["collection_uuid"], "a" * 64, "version", after=4)

    def test_target_cursor_preserves_native_nanoseconds_and_priority_order(self):
        collection = self.job["arguments"]["collection_uuid"]
        target = {"uuid": str(uuid.uuid4()), "revision": 1, "state": "pending", "collection_uuid": collection,
                  "url": self.body["url"], "priority": 10, "not_before": "2026-10-03T00:00:00.000000001Z"}
        later = {**target, "uuid": str(uuid.uuid4()), "not_before": "2026-10-03T00:00:00.000000002Z"}
        self.transport._request.return_value = [later]
        cursor = self.client.target_cursor(target)
        self.assertEqual(self.client.ready(collection, after=cursor), [later])
        for invalid in (target, dict(later, priority=11), dict(later, not_before="invalid")):
            self.transport._request.return_value = [invalid]
            with self.assertRaises(Unavailable):
                self.client.ready(collection, after=cursor)

    def test_checkpoint_is_object_and_unchanged_evidence_keeps_original_receipt(self):
        self.transport._request.return_value = self.receipt
        result = self.client.checkpoint(self.job["uuid"], self.lease, 1, self.body)
        self.assertEqual(result, self.receipt)
        sent = decode(self.transport._request.call_args.args[2], MAX_RESPONSE)
        self.assertEqual(sent["body"], self.body)
        self.assertEqual(sent["owner_uuid"], self.owner)
        self.assertNotIn("producer_uuid", sent)
        for mutation in ({"sha256": "f" * 64}, {"revision": 3}, {"record_count": 4},
                         {"fence": 3}, {"revision": 2, "fence": 1}, {"revision": True}):
            self.transport._request.return_value = dict(self.receipt, **mutation)
            with self.assertRaises(Unavailable):
                self.client.checkpoint(self.job["uuid"], self.lease, 1, self.body)

    def test_saved_head_hash_counts_and_request_identity_are_verified(self):
        self.transport._request.return_value = dict(self.receipt, body=self.body)
        value = self.client.head(self.job["uuid"], self.body["url"], "1.32.15-dev")
        self.assertEqual(value["body"], self.body)
        self.assertEqual(self.transport._request.call_args.kwargs["max_response_bytes"], MAX_RESPONSE)
        for mutation in ({"sha256": "0" * 64}, {"pending_count": 0}, {"body": None},
                         {"body": dict(self.body, url="https://example.invalid/wrong")},
                         {"body": dict(self.body, extractor_version="changed")}):
            self.transport._request.return_value = dict(self.receipt, body=self.body) | mutation
            with self.assertRaises(Unavailable):
                self.client.head(self.job["uuid"], self.body["url"], "1.32.15-dev")

    def test_unicode_separator_encoding_matches_native_hash_representation(self):
        body = deepcopy(self.body)
        body["records"][0]["patch"]["title"] = "<>& 🙂 \u2028 \u2029"
        raw = checkpoint_bytes(body)
        self.assertIn(b"<>&", raw)
        self.assertIn(b"\\u2028", raw)
        self.assertIn(b"\\u2029", raw)
        self.assertNotIn(b"\\ud83d", raw)
        self.assertEqual(decode(raw), body)

    def test_checkpoint_number_tokens_survive_resume_copy_and_encoding(self):
        source = b'{"precise":0.12345678901234567890123456789,"whole":1e+03,"zero":-0,"small":1e-9999,"large":1e9999}'
        payload = decode(source, preserve_numbers=True)
        raw = encode(deepcopy(payload))
        for token in (b"0.12345678901234567890123456789", b"1e+03", b"-0", b"1e-9999", b"1e9999"):
            self.assertIn(token, raw)
        body = deepcopy(self.body)
        body["records"][0]["patch"]["source_numbers"] = payload
        self.assertEqual(checkpoint_bytes(Bundle(body["url"], body["extractor_version"], body).checkpoint()), checkpoint_bytes(body))
        # Ordinary event encoding keeps its prior number representation.
        self.assertEqual(encode(decode(b'{"value":1.20}')), b'{"value":1.2}')
        for invalid in (b'{"v":NaN}', b'{"v":Infinity}', b'{"v":01}', b'{"v":1,"v":2}'):
            with self.assertRaises(InvalidData):
                decode(invalid, preserve_numbers=True)

    def test_child_protocol_preserves_retained_numbers_across_processes(self):
        raw = b'{"value":0.12345678901234567890,"zero":-0,"exponent":1.20e+03}'
        script = ("import sys; from stash_ingest.encoding import decode, encode; "
                  "sys.stdout.buffer.write(encode(decode(sys.stdin.buffer.read(), preserve_numbers=True)))")
        result = _exchange([sys.executable, "-B", "-c", script], raw, 5)
        self.assertEqual(encode(result), b'{"exponent":1.20e+03,"value":0.12345678901234567890,"zero":-0}')

    def test_claim_and_renew_refuse_changed_work_or_ownership(self):
        running = dict(self.job, state="running", owner_uuid=self.owner, revision=2, fence=1)
        self.transport._request.return_value = (running, "date", 100)
        self.assertEqual(self.client.claim(self.job, self.owner)[0], running)
        for mutation in ({"uuid": str(uuid.uuid4())}, {"owner_uuid": str(uuid.uuid4())}, {"fence": 2},
                         {"arguments": dict(self.job["arguments"], policy_sha256="b" * 64)}, {"state": "queued"}):
            self.transport._request.return_value = (dict(running, **mutation), "date", 100)
            with self.assertRaises(Unavailable):
                self.client.claim(self.job, self.owner)
            with self.assertRaises(Unavailable):
                self.client.renew(running)
        self.transport._request.return_value = (None, "date", 100)
        self.assertIsNone(self.client.claim(self.job, self.owner)[0])
        for seconds in (True, 4, 901):
            with self.assertRaises(InvalidData):
                self.client.claim(self.job, self.owner, seconds)

    def test_source_reservation_requires_exact_attempt_and_boolean_reply(self):
        running = dict(self.job, state="running", owner_uuid=self.owner, revision=2, fence=1)
        good = {"job_uuid": running["uuid"], "fence": 1, "ready": True}
        self.transport._request.return_value = good
        self.assertTrue(self.client.reserve_source(running, "https://redgifs.com/watch/example"))
        self.transport._request.return_value = dict(good, ready=False)
        self.assertFalse(self.client.reserve_source(running, "https://redgifs.com/watch/example"))
        for mutation in ({"job_uuid": str(uuid.uuid4())}, {"fence": 2}, {"fence": True}, {"ready": 1}):
            self.transport._request.return_value = dict(good, **mutation)
            with self.assertRaises(Unavailable):
                self.client.reserve_source(running, "https://redgifs.com/watch/example")

    def test_failure_receipt_is_bound_to_attempt_producer_and_outcome(self):
        result = {"job_uuid": self.job["uuid"], "producer_uuid": self.transport.producer,
                  **self.lease, "error_code": "timeout", "outcome": "retry", "ended_at": "2026-10-03T01:00:00Z"}
        self.transport._request.return_value = result
        self.assertEqual(self.client.fail(self.job["uuid"], self.lease, "timeout"), result)
        for mutation in ({"producer_uuid": str(uuid.uuid4())}, {"owner_uuid": str(uuid.uuid4())},
                         {"fence": 1}, {"outcome": "succeeded"}, {"error_code": "authentication"}, {"ended_at": None}, {"ended_at": "invalid"}):
            self.transport._request.return_value = dict(result, **mutation)
            with self.assertRaises(Unavailable):
                self.client.fail(self.job["uuid"], self.lease, "timeout")
        for code in ("succeeded", "private output", "", {}):
            with self.assertRaises(InvalidData):
                self.client.fail(self.job["uuid"], self.lease, code)
        self.transport._request.return_value = dict(result, fence=8, outcome="failed")
        self.client.fail(self.job["uuid"], dict(self.lease, fence=8), "timeout")

    def test_publication_cannot_certify_another_checkpoint_or_attempt(self):
        result = {"job_uuid": self.job["uuid"], "checkpoint_sha256": self.receipt["sha256"],
                  "checkpoint_revision": 1, "fence": 2, "record_count": 3, "capture_count": 2,
                  "unresolved_count": 1, "completion_uuid": str(uuid.uuid4())}
        self.transport._request.return_value = result
        self.assertEqual(self.client.publish(self.job["uuid"], self.lease, self.receipt), result)
        for mutation in ({"job_uuid": str(uuid.uuid4())}, {"fence": 1}, {"checkpoint_revision": 2},
                         {"checkpoint_sha256": "b" * 64}, {"capture_count": 4}, {"completion_uuid": None}):
            self.transport._request.return_value = dict(result, **mutation)
            with self.assertRaises(Unavailable):
                self.client.publish(self.job["uuid"], self.lease, self.receipt)


class JobLeaseTests(unittest.TestCase):
    def setUp(self):
        self.client = EnrichmentClient(Mock(timeout=15))
        self.clock = 100.0
        self.lease = JobLease(self.client, str(uuid.uuid4()), seconds=60, clock=lambda: self.clock)
        self.date = datetime(2026, 10, 3, tzinfo=timezone.utc)

    def accept(self, seconds=60, started=100):
        self.lease._accept(({"lease_until": (self.date + timedelta(seconds=seconds)).isoformat()}, format_datetime(self.date), started))

    def test_server_deadline_uses_request_start_and_caps_clock_skew(self):
        self.accept()
        self.assertEqual(self.lease.deadline, 158)
        self.clock = 157
        self.lease.check()
        self.clock = 158
        with self.assertRaises(SourcePaused):
            self.lease.check()
        self.clock = 100
        self.accept(seconds=3600)
        self.assertEqual(self.lease.deadline, 160)
        self.lease.close()
        with self.assertRaises(SourcePaused):
            self.lease.check()

    def test_delayed_expired_or_missing_server_deadline_refuses_work(self):
        for seconds, started in ((1, 100), (60, 0)):
            with self.assertRaises(SourcePaused):
                self.accept(seconds, started)
        for value in (({}, format_datetime(self.date), 100), ({"lease_until": self.date.isoformat()}, None, 100),
                      ({"lease_until": "2026-10-03T00:01:00"}, format_datetime(self.date), 100)):
            with self.assertRaises(SourcePaused):
                self.lease._accept(value)

    def test_failed_renewal_permanently_stops_further_extraction(self):
        self.accept()
        self.client.renew = Mock(side_effect=Unavailable("network_unavailable"))
        with self.assertRaises(SourcePaused):
            self.lease.renew()
        with self.assertRaises(SourcePaused):
            self.lease.check()
        self.assertTrue(self.lease.failed)
