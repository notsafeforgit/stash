"""Bounded enrichment transport; only server publication certifies completion."""

import hashlib
from datetime import datetime
import re

from .client import Unavailable
from .encoding import InvalidData, encode, identifier, native_json
from .events import sha256
from .metadata_bundle import Bundle, ERRORS, MAX_BYTES, MAX_RECORDS, MAX_REFERENCES, SCHEMA, RETAINED_SCHEMA, public_url


PREFIX = "/enrichment"
MAX_RESPONSE = MAX_BYTES + 4096
RETRYABLE = {"rate_limited", "extraction_failed", "timeout", "worker_failed", "source_busy"}
CAPTURE_POLICY = "source-retention-v1+capture-context-v1"


def checkpoint_bytes(body):
    return native_json(body, MAX_BYTES)


class EnrichmentClient:
    prefix = PREFIX
    minimum_records = 1

    def __init__(self, client):
        self.client = client

    @classmethod
    def path(cls, job, suffix=""):
        return cls.prefix + "/jobs/" + identifier(job) + suffix

    def capabilities(self):
        capabilities = self.client.capabilities()
        if (type(capabilities.get("enrichment_protocol")) is not int or capabilities["enrichment_protocol"] != 2
                or type(capabilities.get("enrichment_source_pacing_protocol")) is not int
                or capabilities["enrichment_source_pacing_protocol"] != 1
                or type(capabilities.get("max_enrichment_checkpoint_bytes")) is not int
                or capabilities["max_enrichment_checkpoint_bytes"] < MAX_BYTES):
            raise Unavailable("native_enrichment_worker_unavailable")
        return capabilities

    @staticmethod
    def target_cursor(target):
        value = {key: target.get(key) for key in ("priority", "not_before", "uuid")}
        EnrichmentClient._target_order(value)
        return value

    @staticmethod
    def _target_order(value):
        if (not isinstance(value, dict) or set(value) != {"priority", "not_before", "uuid"}
                or type(value["priority"]) is not int or not 0 <= value["priority"] <= 100
                or not isinstance(value["not_before"], str)):
            raise InvalidData("Invalid enrichment target cursor")
        identifier(value["uuid"])
        match = re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.([0-9]{1,9}))?(?:Z|[+-]\d\d:\d\d)", value["not_before"])
        if match is None:
            raise InvalidData("Invalid enrichment target time")
        try:
            when = datetime.fromisoformat(value["not_before"]).replace(microsecond=0)
        except ValueError:
            raise InvalidData("Invalid enrichment target time") from None
        # Keep native nanosecond ordering even though datetime stores microseconds.
        return -value["priority"], when, int((match[1] or "").ljust(9, "0")), value["uuid"]

    def ready(self, collection, limit=20, *, after=None):
        if type(limit) is not int or not 1 <= limit <= 100:
            raise InvalidData("Invalid enrichment candidate limit")
        previous = self._target_order(after) if after is not None else None
        request = {"limit": limit}
        if after is not None:
            request["after"] = after
        result = self.client._request("POST", PREFIX + "/collections/" + identifier(collection) + "/ready", encode(request))
        if (not isinstance(result, list) or len(result) > limit
                or any(not isinstance(t, dict) or t.get("collection_uuid") != collection or t.get("state") != "pending" for t in result)):
            raise Unavailable("invalid_response")
        try:
            for target in result:
                identifier(target.get("uuid"))
                public_url(target.get("url"))
                if type(target.get("revision")) is not int or target["revision"] < 1:
                    raise InvalidData("Invalid target revision")
                order = self._target_order(self.target_cursor(target))
                if previous is not None and order <= previous:
                    raise InvalidData("Enrichment target page did not advance")
                previous = order
            if len({t["uuid"] for t in result}) != len(result):
                raise InvalidData("Duplicate candidate")
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return result

    def ready_jobs(self, collection, policy, extractor, *, after=0, limit=20):
        if (not sha256(policy) or not isinstance(extractor, str) or not extractor or len(extractor) > 128
                or any(c in extractor for c in "\r\n\x00") or type(after) is not int or not 0 <= after <= 9223372036854775807
                or type(limit) is not int or not 1 <= limit <= 100):
            raise InvalidData("Invalid enrichment job discovery")
        result = self.client._request("POST", self.prefix + "/collections/" + identifier(collection) + "/jobs/ready", encode({
            "policy_sha256": policy, "extractor_version": extractor, "after": after, "limit": limit}))
        if not isinstance(result, list) or len(result) > limit:
            raise Unavailable("invalid_response")
        try:
            seen = set()
            for item in result:
                if (not isinstance(item, dict) or set(item) != {"sequence", "uuid"}
                        or type(item["sequence"]) is not int or not after < item["sequence"] <= 9223372036854775807):
                    raise InvalidData("Invalid enrichment job page")
                identifier(item["uuid"])
                if item["uuid"] in seen:
                    raise InvalidData("Repeated enrichment job candidate")
                after = item["sequence"]
                seen.add(item["uuid"])
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return result

    def ready_collections(self, policy, extractor, *, after="", limit=20):
        if (not sha256(policy) or not isinstance(extractor, str) or not extractor or len(extractor) > 128
                or any(c in extractor for c in "\r\n\x00") or type(limit) is not int or not 1 <= limit <= 100):
            raise InvalidData("Invalid enrichment collection discovery")
        if after:
            identifier(after)
        elif after != "":
            raise InvalidData("Invalid enrichment collection cursor")
        result = self.client._request("POST", self.prefix + "/collections/ready", encode({
            "policy_sha256": policy, "extractor_version": extractor, "after": after, "limit": limit}))
        if not isinstance(result, list) or len(result) > limit:
            raise Unavailable("invalid_response")
        try:
            for item in result:
                if not isinstance(item, dict) or set(item) != {"uuid"}:
                    raise InvalidData("Invalid enrichment collection page")
                identifier(item["uuid"])
                if item["uuid"] <= after:
                    raise InvalidData("Enrichment collection page did not advance")
                after = item["uuid"]
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return result

    @staticmethod
    def _job(value, expected=None):
        work = value.get("arguments") if isinstance(value, dict) else None
        if (not isinstance(work, dict) or value.get("kind") != "post.enrich"
                or (expected is not None and value.get("uuid") != expected)
                or type(work.get("version")) is not int or work["version"] not in (1, 2)
                or not sha256(work.get("policy_sha256"))
                or not isinstance(work.get("extractor_version"), str) or not work["extractor_version"]
                or any(type(work.get(k)) is not int or work[k] < 1 for k in ("target_revision", "collection_revision"))
                or type(value.get("revision")) is not int or value["revision"] < 1
                or type(value.get("fence")) is not int or not 0 <= value["fence"] <= 8
                or type(value.get("max_attempts")) is not int or value["max_attempts"] != 8
                or value.get("state") not in ("queued", "running", "succeeded", "failed", "cancelled")):
            raise Unavailable("invalid_response")
        try:
            if work["version"] == 1:
                if "capture_policy" in work or "handoff" in work:
                    raise InvalidData("V1 execution cannot reuse a retained checkpoint")
            else:
                if work.get("capture_policy") != CAPTURE_POLICY:
                    raise InvalidData("Unknown enrichment capture policy")
                if "handoff" in work:
                    handoff = work["handoff"]
                    if (not isinstance(handoff, dict) or set(handoff) != {"uuid", "plan_sha256", "seed_sha256"}
                            or not sha256(handoff["plan_sha256"]) or not sha256(handoff["seed_sha256"])):
                        raise InvalidData("Invalid retained checkpoint identity")
                    identifier(handoff["uuid"])
            identifier(value.get("uuid"))
            for key in ("target_uuid", "post_uuid", "collection_uuid"):
                identifier(work.get(key))
            if work.get("root_uuid") is not None:
                identifier(work["root_uuid"])
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return work

    def admit(self, target, revision, policy, extractor):
        if type(revision) is not int or revision < 1 or not sha256(policy) or not isinstance(extractor, str) or not extractor:
            raise InvalidData("Invalid enrichment admission")
        value = self.client._request("POST", PREFIX + "/targets/" + identifier(target) + "/jobs", encode({
            "expected_revision": revision, "policy_sha256": policy, "extractor_version": extractor}))
        work = self._job(value)
        if (work.get("target_uuid") != target or work.get("target_revision") != revision
                or work.get("policy_sha256") != policy or work.get("extractor_version") != extractor):
            raise Unavailable("invalid_response")
        return value

    def describe(self, job):
        value = self.client._request("GET", self.path(job))
        current = value.get("job") if isinstance(value, dict) else None
        target = value.get("target") if isinstance(value, dict) else None
        work = self._job(current, job)
        if (not isinstance(target, dict)
                or any(work.get(k) != target.get(k) for k in ("post_uuid", "collection_uuid", "collection_revision"))
                or work.get("target_uuid") != target.get("uuid") or target.get("policy") != "gallery-dl-metadata-v1"):
            raise Unavailable("invalid_response")
        try:
            public_url(target.get("url"))
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return value

    def admit_handoff(self, handoff, plan, policy, extractor):
        if not sha256(plan) or not sha256(policy) or not isinstance(extractor, str) or not extractor:
            raise InvalidData("Invalid checkpoint handoff admission")
        value = self.client._request("POST", PREFIX + "/handoffs/" + identifier(handoff) + "/jobs", encode({
            "expected_plan_sha256": plan, "policy_sha256": policy, "extractor_version": extractor}))
        work = self._job(value)
        if (work.get("handoff", {}).get("uuid") != handoff or work["handoff"]["plan_sha256"] != plan
                or work["policy_sha256"] != policy or work["extractor_version"] != extractor):
            raise Unavailable("invalid_response")
        return value

    def seed(self, job, url):
        work = self._job(job)
        handoff = work.get("handoff")
        if handoff is None:
            return None
        value = self.client._request("GET", self.path(job["uuid"], "/seed"), max_response_bytes=MAX_RESPONSE,
                                     preserve_numbers=True)
        if (not isinstance(value, dict) or set(value) != {"handoff_uuid", "plan_sha256", "sha256", "body"}
                or value["handoff_uuid"] != handoff["uuid"] or value["plan_sha256"] != handoff["plan_sha256"]
                or value["sha256"] != handoff["seed_sha256"] or not isinstance(value["body"], dict)):
            raise Unavailable("invalid_response")
        try:
            body = Bundle(url, work["extractor_version"], value["body"]).checkpoint()
            raw = checkpoint_bytes(body)
        except InvalidData:
            raise Unavailable("invalid_response") from None
        if (body["schema"] != RETAINED_SCHEMA or not body["records"]
                or any(not record.get("retained_capture") for record in body["records"])
                or hashlib.sha256(raw).hexdigest() != handoff["seed_sha256"]):
            raise Unavailable("invalid_response")
        return body

    @staticmethod
    def _seconds(seconds):
        if type(seconds) is not int or not 5 <= seconds <= 900:
            raise InvalidData("Invalid enrichment lease duration")

    def claim(self, job, owner, seconds=180):
        work = self._job(job)
        self._seconds(seconds)
        response = self.client._request("POST", self.path(job["uuid"], "/claim"), encode({
            "expected_revision": job["revision"], "owner_uuid": identifier(owner),
            "policy_sha256": work["policy_sha256"], "extractor_version": work["extractor_version"],
            "lease_seconds": seconds}), timed=True, allow_empty=True)
        if response[0] is not None:
            self._running(response[0], job, owner)
            expected_fence = job["fence"] if job["state"] == "running" else job["fence"] + 1
            if response[0]["fence"] != expected_fence:
                raise Unavailable("invalid_response")
        return response

    def renew(self, job, seconds=180):
        self._job(job)
        self._seconds(seconds)
        response = self.client._request("POST", self.path(job["uuid"], "/renew"), encode({
            **self.lease(job), "lease_seconds": seconds}), timed=True)
        self._running(response[0], job, job["owner_uuid"])
        if response[0]["fence"] != job["fence"]:
            raise Unavailable("invalid_response")
        return response

    def reserve_source(self, job, url):
        self._job(job)
        value = self.client._request("POST", self.path(job["uuid"], "/source"), encode({
            **self.lease(job), "url": public_url(url)}))
        if (not isinstance(value, dict) or value.get("job_uuid") != job["uuid"]
                or type(value.get("fence")) is not int or value["fence"] != job["fence"]
                or type(value.get("ready")) is not bool):
            raise Unavailable("invalid_response")
        return value["ready"]

    def _running(self, value, expected, owner):
        work = self._job(value, expected["uuid"])
        if (work != expected["arguments"] or value["state"] != "running"
                or value.get("owner_uuid") != owner or value["fence"] < 1
                or value["revision"] < expected["revision"]):
            raise Unavailable("invalid_response")

    def head(self, job, url, extractor):
        value = self.client._request("GET", self.path(job, "/checkpoint"), max_response_bytes=MAX_RESPONSE,
                                     preserve_numbers=True)
        if value is None:
            return None
        self._receipt(value, job)
        body = value.get("body")
        try:
            bundle = Bundle(url, extractor, body)
            raw = checkpoint_bytes(bundle.checkpoint())
        except InvalidData:
            raise Unavailable("invalid_response") from None
        if (body is None or value["sha256"] != hashlib.sha256(raw).hexdigest()
                or any(value[field] != len(body[key]) for field, key in
                       (("record_count", "records"), ("pending_count", "pending"), ("unresolved_count", "unresolved")))):
            raise Unavailable("invalid_response")
        return value

    @classmethod
    def _receipt(cls, value, job):
        if (not isinstance(value, dict) or value.get("job_uuid") != job or not sha256(value.get("sha256"))
                or any(type(value.get(k)) is not int or value[k] < minimum for k, minimum in
                       (("revision", 1), ("fence", 1), ("record_count", cls.minimum_records), ("pending_count", 0), ("unresolved_count", 0)))
                or value["record_count"] > MAX_RECORDS or value["pending_count"] > MAX_REFERENCES
                or value["unresolved_count"] > MAX_REFERENCES):
            raise Unavailable("invalid_response")

    @staticmethod
    def lease(lease):
        if not isinstance(lease, dict) or type(lease.get("fence")) is not int or lease["fence"] < 1:
            raise InvalidData("Invalid enrichment attempt fence")
        return {"owner_uuid": identifier(lease.get("owner_uuid")), "fence": lease["fence"]}

    def checkpoint(self, job, lease, expected, body):
        if type(expected) is not int or expected < 0 or not isinstance(body, dict) or body.get("schema") not in (SCHEMA, RETAINED_SCHEMA):
            raise InvalidData("Invalid enrichment checkpoint")
        bundle = Bundle(body.get("url"), body.get("extractor_version"), body)
        raw = checkpoint_bytes(bundle.checkpoint())
        # Keep the body as one JSON object, never an escaped string. Its digest
        # matches the native canonical body rather than the HTTP envelope.
        envelope = encode({**self.lease(lease), "expected_revision": expected})
        request = envelope[:-1] + b',"body":' + raw + b'}'
        value = self.client._request("POST", self.path(job, "/checkpoint"), request)
        self._receipt(value, job)
        # Resending unchanged evidence can return an older receipt, including
        # one owned by a predecessor. Its original provenance must stay intact.
        if (value["revision"] > expected + 1 or value["fence"] > lease["fence"]
                or (value["revision"] == expected + 1 and value["fence"] != lease["fence"])
                or value["sha256"] != hashlib.sha256(raw).hexdigest()
                or any(value[field] != len(body[key]) for field, key in
                       (("record_count", "records"), ("pending_count", "pending"), ("unresolved_count", "unresolved")))):
            raise Unavailable("invalid_response")
        return value

    def publication(self, job):
        value = self.client._request("GET", self.path(job, "/publication"))
        if value is not None:
            self._publication(value, job)
        return value

    @staticmethod
    def _publication(value, job):
        if (not isinstance(value, dict) or value.get("job_uuid") != job or not sha256(value.get("checkpoint_sha256"))
                or any(type(value.get(k)) is not int or value[k] < minimum for k, minimum in
                       (("checkpoint_revision", 1), ("fence", 1), ("record_count", 1), ("capture_count", 1), ("unresolved_count", 0)))
                or not value["capture_count"] <= value["record_count"] <= MAX_RECORDS
                or value["unresolved_count"] > MAX_REFERENCES):
            raise Unavailable("invalid_response")
        try:
            identifier(value.get("completion_uuid"))
        except InvalidData:
            raise Unavailable("invalid_response") from None

    def publish(self, job, lease, receipt):
        self._receipt(receipt, job)
        value = self.client._request("POST", self.path(job, "/publish"), encode({**self.lease(lease),
            "checkpoint_revision": receipt["revision"], "checkpoint_sha256": receipt["sha256"]}))
        self._publication(value, job)
        if (value["fence"] != lease["fence"] or value["checkpoint_revision"] != receipt["revision"]
                or value["checkpoint_sha256"] != receipt["sha256"] or value["record_count"] != receipt["record_count"]
                or value["unresolved_count"] != receipt["unresolved_count"]):
            raise Unavailable("invalid_response")
        return value

    def completion(self, job):
        return self.publication(job)

    def complete(self, job, lease, receipt):
        return self.publish(job, lease, receipt)

    @classmethod
    def _completion(cls, value, job, *, work=None, receipt=None, lease=None):
        cls._publication(value, job)
        if receipt is not None and any(value[k] != receipt[v] for k, v in (
                ("checkpoint_revision", "revision"), ("checkpoint_sha256", "sha256"),
                ("record_count", "record_count"), ("unresolved_count", "unresolved_count"))):
            raise Unavailable("invalid_response")
        if lease is not None and value["fence"] != lease["fence"]:
            raise Unavailable("invalid_response")

    def fail(self, job, lease, code):
        if not isinstance(code, str) or code not in ERRORS | {"post_identity_conflict"}:
            raise InvalidData("Invalid enrichment failure code")
        value = self.client._request("POST", self.path(job, "/failure"), encode({**self.lease(lease), "error_code": code}))
        outcome = "retry" if code in RETRYABLE and lease["fence"] < 8 else "failed"
        if (not isinstance(value, dict) or value.get("job_uuid") != job or value.get("producer_uuid") != self.client.producer
                or value.get("owner_uuid") != lease["owner_uuid"] or type(value.get("fence")) is not int
                or value["fence"] != lease["fence"]
                or value.get("error_code") != code or value.get("outcome") != outcome or not value.get("ended_at")):
            raise Unavailable("invalid_response")
        try:
            if datetime.fromisoformat(value["ended_at"]).tzinfo is None:
                raise ValueError()
        except (TypeError, ValueError):
            raise Unavailable("invalid_response") from None
        return value
