"""Candidate detail transport. A comparison receipt never accepts a post identity."""

import hashlib
import re

from .client import Unavailable
from .discovery_client import _integer, _runtime, _time
from .encoding import InvalidData, encode, identifier, native_json
from .enrichment_client import CAPTURE_POLICY, EnrichmentClient
from .events import sha256
from .metadata_bundle import MAX_BYTES, MAX_RECORDS, MAX_REFERENCES, SCHEMA

WORK_KEYS = frozenset({"version", "generation", "target_uuid", "target_revision", "source_sha256",
    "post_uuid", "post_revision", "candidate_sequence", "post_namespace", "post_value", "url",
    "listing_uuid", "definition_sha256", "page_ordinal", "page_sha256", "collection_uuid",
    "collection_revision", "root_uuid", "policy_sha256", "extractor_version", "capture_policy"})
MATCH_POLICY = "retained-discovery-detail-v1"
BASES = frozenset({"exact-title-and-date", "exact-original-text-and-date",
                  "exact-title-and-original-text", "exact-source-url-and-date"})


def post_url(namespace, value):
    if not isinstance(value, str) or not 1 <= len(value) <= 256:
        raise InvalidData("Invalid selected source post")
    if namespace == "native:reddit" and re.fullmatch(r"[a-z0-9]+", value):
        return "https://www.reddit.com/comments/" + value
    if namespace == "native:twitter" and re.fullmatch(r"[0-9]+", value):
        return "https://x.com/i/web/status/" + value
    raise InvalidData("Unsupported selected source post")


class DiscoveryDetailClient(EnrichmentClient):
    prefix = "/discovery-details"
    minimum_records = 0

    def capabilities(self):
        value = self.client.capabilities()
        if (not _integer(value.get("discovery_detail_protocol"), 1, 1)
                or not _integer(value.get("enrichment_source_pacing_protocol"), 1, 1)
                or not _integer(value.get("max_discovery_detail_bytes"), MAX_BYTES)):
            raise Unavailable("native_discovery_detail_unavailable")
        return value

    @staticmethod
    def _job(value, expected=None):
        work = value.get("arguments") if isinstance(value, dict) else None
        if (not isinstance(work, dict) or set(work) != WORK_KEYS or value.get("kind") != "post.verify_candidate"
                or (expected is not None and value.get("uuid") != expected)
                or not _integer(work["version"], 1, 1) or work["capture_policy"] != CAPTURE_POLICY
                or any(not _integer(work[k], 1) for k in ("generation", "target_revision", "post_revision",
                                                       "candidate_sequence", "collection_revision"))
                or not _integer(work["page_ordinal"], 1, 10000)
                or any(not sha256(work[k]) for k in ("source_sha256", "page_sha256", "definition_sha256", "policy_sha256"))
                or not _integer(value.get("revision"), 1) or not _integer(value.get("fence"), 0, 8)
                or not _integer(value.get("max_attempts"), 8, 8)
                or value.get("state") not in ("queued", "running", "succeeded", "failed", "cancelled")):
            raise Unavailable("invalid_response")
        try:
            identifier(value.get("uuid"))
            for key in ("target_uuid", "post_uuid", "listing_uuid", "collection_uuid"):
                identifier(work[key])
            if work["root_uuid"] is not None:
                identifier(work["root_uuid"])
            _runtime(work["extractor_version"])
            if post_url(work["post_namespace"], work["post_value"]) != work["url"]:
                raise InvalidData("Selected post and fetch URL differ")
            for key in ("available_at", "created_at", "updated_at"):
                _time(value.get(key), milliseconds=True)
            if value["state"] == "running":
                identifier(value.get("owner_uuid"))
                _time(value.get("lease_until"), milliseconds=True)
                if value["fence"] == 0:
                    raise InvalidData("Running detail requires ownership")
            elif value.get("owner_uuid") is not None or value.get("lease_until") is not None:
                raise InvalidData("Inactive detail cannot retain ownership")
            if value["state"] == "queued" and value["fence"] == 8:
                raise InvalidData("Exhausted detail cannot remain queued")
            if (value.get("work_key") != hashlib.sha256(native_json(work, 16384)).hexdigest()
                    or value.get("resource_key") != hashlib.sha256(
                        ("enrichment-collection\x00" + work["collection_uuid"]).encode()).hexdigest()):
                raise InvalidData("Detail job identity changed")
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return work

    def admit(self, target, revision, candidate, policy, extractor):
        _runtime(extractor)
        if not _integer(revision, 1) or not _integer(candidate, 1) or not sha256(policy):
            raise InvalidData("Invalid candidate detail admission")
        value = self.client._request("POST", self.prefix + "/targets/" + identifier(target) + "/jobs", encode({
            "expected_target_revision": revision, "candidate_sequence": candidate,
            "policy_sha256": policy, "extractor_version": extractor}))
        work = self._job(value)
        expected = {"target_uuid": target, "target_revision": revision, "candidate_sequence": candidate,
                    "policy_sha256": policy, "extractor_version": extractor, "generation": 1}
        if any(work[k] != v for k, v in expected.items()):
            raise Unavailable("invalid_response")
        return value

    def describe(self, job):
        value = self.client._request("GET", self.path(job))
        self._job(value, job)
        return {"job": value}

    def retry(self, job):
        original = self.describe(job)["job"]
        if original["state"] not in ("failed", "cancelled"):
            raise InvalidData("Only ended detail jobs can be retried")
        value = self.client._request("POST", self.path(job, "/retry"), b"{}")
        work = self._job(value)
        if work != {**original["arguments"], "generation": original["arguments"]["generation"] + 1}:
            raise Unavailable("invalid_response")
        return value

    def seed(self, job, url):
        self._job(job)
        return None

    @classmethod
    def _receipt(cls, value, job):
        super()._receipt(value, job)
        if not _integer(value["revision"], 1, 128) or not _integer(value["fence"], 1, 8):
            raise Unavailable("invalid_response")
        try:
            _time(value.get("created_at"))
        except InvalidData:
            raise Unavailable("invalid_response") from None

    def head(self, job, url, extractor):
        value = super().head(job, url, extractor)
        if value is not None and value["body"]["schema"] != SCHEMA:
            raise Unavailable("invalid_response")
        return value

    def checkpoint(self, job, lease, expected, body):
        if not isinstance(body, dict) or body.get("schema") != SCHEMA:
            raise InvalidData("Detail requires newly observed metadata")
        return super().checkpoint(job, lease, expected, body)

    @classmethod
    def _completion(cls, value, job, *, work=None, receipt=None, lease=None):
        evidence = value.get("evidence") if isinstance(value, dict) else None
        keys = {"policy", "post", "url", "page_sha256", "transcript_sha256", "status",
                "record_ordinals", "pending_count", "unresolved_count"}
        if (not isinstance(value, dict) or set(value) != {"job_uuid", "checkpoint_revision", "fence", "evidence", "created_at"}
                or value["job_uuid"] != job or not _integer(value["checkpoint_revision"], 1, 128)
                or not _integer(value["fence"], 1, 8) or not isinstance(evidence, dict)
                or not keys <= set(evidence) or set(evidence) - keys - {"basis", "witness_ordinal"}
                or evidence["policy"] != MATCH_POLICY or evidence["status"] not in ("corroborated", "uncorroborated")
                or not isinstance(evidence["post"], dict) or set(evidence["post"]) != {"Namespace", "Value"}
                or not sha256(evidence["page_sha256"]) or not sha256(evidence["transcript_sha256"])
                or not _integer(evidence["pending_count"], 0, 0)
                or not _integer(evidence["unresolved_count"], 0, MAX_REFERENCES)
                or not isinstance(evidence["record_ordinals"], list) or len(evidence["record_ordinals"]) > MAX_RECORDS
                or any(type(v) is not int or v != i for i, v in enumerate(evidence["record_ordinals"]))):
            raise Unavailable("invalid_response")
        try:
            _time(value["created_at"])
            if evidence["url"] != post_url(evidence["post"]["Namespace"], evidence["post"]["Value"]):
                raise InvalidData("Comparison identifies a different source URL")
        except InvalidData:
            raise Unavailable("invalid_response") from None
        if evidence["status"] == "corroborated":
            if (not isinstance(evidence.get("basis"), str) or evidence["basis"] not in BASES
                    or not _integer(evidence.get("witness_ordinal"), 0, len(evidence["record_ordinals"]) - 1)):
                raise Unavailable("invalid_response")
        elif "basis" in evidence or "witness_ordinal" in evidence:
            raise Unavailable("invalid_response")
        if work is not None and (evidence["post"] != {"Namespace": work["post_namespace"], "Value": work["post_value"]}
                or evidence["url"] != work["url"] or evidence["page_sha256"] != work["page_sha256"]):
            raise Unavailable("invalid_response")
        if receipt is not None and (value["checkpoint_revision"] != receipt["revision"]
                or evidence["transcript_sha256"] != receipt["sha256"]
                or len(evidence["record_ordinals"]) != receipt["record_count"]
                or evidence["unresolved_count"] != receipt["unresolved_count"]):
            raise Unavailable("invalid_response")
        if lease is not None and value["fence"] != lease["fence"]:
            raise Unavailable("invalid_response")

    def completion(self, job):
        value = self.client._request("GET", self.path(job, "/result"))
        if value is not None:
            self._completion(value, job)
        return value

    def complete(self, job, lease, receipt):
        self._receipt(receipt, job)
        if receipt["pending_count"]:
            raise InvalidData("Pending detail children cannot be completed")
        value = self.client._request("POST", self.path(job, "/complete"), encode({**self.lease(lease),
            "checkpoint_revision": receipt["revision"], "checkpoint_sha256": receipt["sha256"]}))
        self._completion(value, job, receipt=receipt, lease=lease)
        return value
