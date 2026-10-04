"""Scoped listing transport; page acknowledgement is not a matched source post."""

from datetime import datetime
import hashlib

from .client import Unavailable
from .discovery_fetch import ERRORS, MAX_RECORDS, page_cursor, profile_platform, validate_page
from .encoding import InvalidData, encode, identifier, native_json
from .events import sha256
from .metadata_bundle import MAX_BYTES, OBSERVED_TIME

PREFIX = "/discovery"
RETRYABLE = frozenset({"rate_limited", "timeout", "extraction_failed", "worker_failed", "source_busy"})
LISTING_KEYS = frozenset({"uuid", "account_uuid", "collection_uuid", "collection_revision", "root_uuid",
    "profile_url", "policy_sha256", "extractor_version", "initial_cursor", "historical_pages", "legacy", "not_before"})
WORK_KEYS = frozenset({"version", "listing_uuid", "generation", "page_ordinal", "definition_sha256", "collection_uuid"})
MAX_INT = 9223372036854775807


def _integer(value, low, high=MAX_INT):
    return type(value) is int and low <= value <= high


def _time(value, *, milliseconds=False):
    match = OBSERVED_TIME.fullmatch(value) if isinstance(value, str) else None
    if match is None:
        raise InvalidData("Invalid native job time")
    try:
        parsed = datetime.fromisoformat(value)
        if parsed.timestamp() <= 0:
            raise ValueError()
    except (ValueError, OverflowError, OSError):
        raise InvalidData("Invalid native job time") from None
    fraction = (match[1] or ".0")[1:]
    if milliseconds and int(fraction.ljust(9, "0")) % 1000000:
        raise InvalidData("Listing deadline must use exact milliseconds")


def _runtime(value):
    try:
        size = len(value.encode("utf-8")) if isinstance(value, str) else 0
    except UnicodeError:
        raise InvalidData("Invalid discovery runtime") from None
    if not 1 <= size <= 128 or any(c in value for c in "\r\n\x00"):
        raise InvalidData("Invalid discovery runtime")


def page_bytes(value):
    return native_json(value, MAX_BYTES)


class DiscoveryClient:
    def __init__(self, client):
        self.client = client

    @staticmethod
    def path(job, suffix=""):
        return PREFIX + "/jobs/" + identifier(job) + suffix

    def capabilities(self):
        value = self.client.capabilities()
        if (type(value.get("discovery_protocol")) is not int or value["discovery_protocol"] != 1
                or type(value.get("discovery_source_pacing_protocol")) is not int
                or value["discovery_source_pacing_protocol"] != 1
                or type(value.get("max_discovery_page_bytes")) is not int
                or value["max_discovery_page_bytes"] < MAX_BYTES):
            raise Unavailable("native_discovery_worker_unavailable")
        return value

    @staticmethod
    def _job(value, expected=None):
        work = value.get("arguments") if isinstance(value, dict) else None
        if (not isinstance(work, dict) or set(work) != WORK_KEYS or value.get("kind") != "account.list_page"
                or (expected is not None and value.get("uuid") != expected)
                or not _integer(work.get("version"), 1, 1)
                or not _integer(work.get("generation"), 1) or not _integer(work.get("page_ordinal"), 1, 10000)
                or not sha256(work.get("definition_sha256"))
                or not _integer(value.get("revision"), 1) or not _integer(value.get("fence"), 0, 8)
                or not _integer(value.get("max_attempts"), 8, 8)
                or value.get("state") not in ("queued", "running", "succeeded", "failed", "cancelled")):
            raise Unavailable("invalid_response")
        try:
            identifier(value.get("uuid"))
            identifier(work["listing_uuid"])
            identifier(work["collection_uuid"])
            for key in ("available_at", "created_at", "updated_at"):
                _time(value.get(key), milliseconds=True)
            if value["state"] == "running":
                identifier(value.get("owner_uuid"))
                _time(value.get("lease_until"), milliseconds=True)
                if value["fence"] < 1:
                    raise InvalidData("A running job requires an attempt")
            elif value.get("owner_uuid") is not None or value.get("lease_until") is not None:
                raise InvalidData("A terminal or queued job cannot hold ownership")
            if value["state"] == "queued" and value["fence"] == 8:
                raise InvalidData("Exhausted discovery work cannot be queued")
        except InvalidData:
            raise Unavailable("invalid_response") from None
        return work

    @staticmethod
    def _listing(value):
        if (not isinstance(value, dict) or set(value) != LISTING_KEYS | {"sha256", "created_at"}
                or not _integer(value["collection_revision"], 1)
                or not _integer(value["historical_pages"], 0, 10000000)
                or not sha256(value["policy_sha256"]) or not sha256(value["sha256"])):
            raise InvalidData("Invalid discovery definition")
        for key in ("uuid", "account_uuid", "collection_uuid"):
            identifier(value[key])
        if value["root_uuid"] is not None:
            identifier(value["root_uuid"])
        _time(value["not_before"], milliseconds=True)
        _time(value["created_at"])
        _runtime(value["extractor_version"])
        platform = profile_platform(value["profile_url"])
        page_cursor(platform, value["initial_cursor"])
        legacy = value["legacy"]
        if legacy is None:
            if value["initial_cursor"] is not None or value["historical_pages"] != 0:
                raise InvalidData("A fresh listing cannot invent historical progress")
        elif (not isinstance(legacy, dict) or set(legacy) != {"snapshot_uuid", "account_ordinal"}
                or not _integer(legacy["account_ordinal"], 1)):
            raise InvalidData("Invalid discovery resume reference")
        else:
            identifier(legacy["snapshot_uuid"])
        body = native_json({key: value[key] for key in LISTING_KEYS}, 32768)
        if hashlib.sha256(body).hexdigest() != value["sha256"]:
            raise InvalidData("Discovery definition differs from its pinned digest")
        return platform

    @staticmethod
    def _receipt(value, job):
        work = DiscoveryClient._job(job)
        if (not isinstance(value, dict) or value.get("job_uuid") != job["uuid"]
                or value.get("listing_uuid") != work["listing_uuid"]
                or not _integer(value.get("ordinal"), 1, 10000) or value["ordinal"] != work["page_ordinal"]
                or not _integer(value.get("fence"), 1, 8) or not sha256(value.get("sha256"))
                or not _integer(value.get("record_count"), 0, MAX_RECORDS) or type(value.get("complete")) is not bool):
            raise Unavailable("invalid_response")
        try:
            identifier(value.get("producer_uuid"))
            _time(value.get("created_at"))
        except InvalidData:
            raise Unavailable("invalid_response") from None

    @staticmethod
    def _description(value, expected=None):
        if not isinstance(value, dict) or set(value) != {"job", "listing", "cursor", "receipt"}:
            raise Unavailable("invalid_response")
        job, listing = value["job"], value["listing"]
        work = DiscoveryClient._job(job, expected)
        try:
            platform = DiscoveryClient._listing(listing)
            page_cursor(platform, value["cursor"])
            if (listing["uuid"] != work["listing_uuid"] or listing["sha256"] != work["definition_sha256"]
                    or listing["collection_uuid"] != work["collection_uuid"]
                    or (work["page_ordinal"] == 1 and value["cursor"] != listing["initial_cursor"])
                    or (work["page_ordinal"] > 1 and value["cursor"] is None)):
                raise InvalidData("Discovery description belongs to another page")
        except InvalidData:
            raise Unavailable("invalid_response") from None
        receipt = value["receipt"]
        if (receipt is not None) != (job["state"] == "succeeded"):
            raise Unavailable("invalid_response")
        if receipt is not None:
            DiscoveryClient._receipt(receipt, job)
            if (receipt["fence"] != job["fence"] or job.get("result") != {
                    "listing_uuid": listing["uuid"], "page_ordinal": work["page_ordinal"], "page_sha256": receipt["sha256"]}):
                raise Unavailable("invalid_response")
        return work, listing

    def admit(self, listing, definition, policy, extractor):
        identifier(listing)
        _runtime(extractor)
        if not sha256(definition) or not sha256(policy):
            raise InvalidData("Invalid discovery admission")
        value = self.client._request("POST", PREFIX + "/listings/" + listing + "/jobs", encode({
            "expected_definition_sha256": definition, "policy_sha256": policy, "extractor_version": extractor}))
        work = self._job(value)
        if work["listing_uuid"] != listing or work["definition_sha256"] != definition:
            raise Unavailable("invalid_response")
        return value

    def describe(self, job):
        value = self.client._request("GET", self.path(job))
        self._description(value, job)
        return value

    @staticmethod
    def _seconds(seconds):
        if not _integer(seconds, 5, 900):
            raise InvalidData("Invalid discovery lease duration")

    @staticmethod
    def lease(value):
        if not isinstance(value, dict) or not _integer(value.get("fence"), 1, 8):
            raise InvalidData("Invalid discovery attempt fence")
        return {"owner_uuid": identifier(value.get("owner_uuid")), "fence": value["fence"]}

    def _running(self, value, expected, owner, fence):
        work = self._job(value, expected["uuid"])
        if (work != expected["arguments"] or value["state"] != "running" or value.get("owner_uuid") != owner
                or value["fence"] != fence or value["revision"] < expected["revision"]):
            raise Unavailable("invalid_response")

    def claim(self, description, owner, seconds=180):
        _, listing = self._description(description)
        job = description["job"]
        self._seconds(seconds)
        response = self.client._request("POST", self.path(job["uuid"], "/claim"), encode({
            "expected_revision": job["revision"], "owner_uuid": identifier(owner),
            "policy_sha256": listing["policy_sha256"], "extractor_version": listing["extractor_version"],
            "lease_seconds": seconds}), timed=True, allow_empty=True)
        if response[0] is not None:
            expected = job["fence"] if job["state"] == "running" else job["fence"] + 1
            self._running(response[0], job, owner, expected)
        return response

    def renew(self, job, seconds=180):
        self._job(job)
        self._seconds(seconds)
        response = self.client._request("POST", self.path(job["uuid"], "/renew"), encode({
            **self.lease(job), "lease_seconds": seconds}), timed=True)
        self._running(response[0], job, job["owner_uuid"], job["fence"])
        return response

    def reserve_source(self, job, url):
        self._job(job)
        profile_platform(url)
        value = self.client._request("POST", self.path(job["uuid"], "/source"), encode({**self.lease(job), "url": url}))
        if (not isinstance(value, dict) or set(value) != {"job_uuid", "fence", "ready"}
                or value["job_uuid"] != job["uuid"] or not _integer(value["fence"], 1, 8)
                or value["fence"] != job["fence"] or type(value["ready"]) is not bool):
            raise Unavailable("invalid_response")
        return value["ready"]

    def append_page(self, description, lease, body):
        work, listing = self._description(description)
        body = validate_page(body, listing["profile_url"], listing["extractor_version"], description["cursor"])
        raw = page_bytes(body)
        owned = self.lease(lease)
        envelope = encode({**owned, "ordinal": work["page_ordinal"]})
        request = envelope[:-1] + b',"body":' + raw + b'}'
        value = self.client._request("POST", self.path(description["job"]["uuid"], "/page"), request)
        self._receipt(value, description["job"])
        if (value["sha256"] != hashlib.sha256(raw).hexdigest() or value["producer_uuid"] != self.client.producer
                or value["fence"] != owned["fence"] or value["record_count"] != len(body["records"])
                or value["complete"] != body["complete"]):
            raise Unavailable("invalid_response")
        return value

    def fail(self, job, lease, code):
        if not isinstance(code, str) or code not in ERRORS:
            raise InvalidData("Invalid discovery failure code")
        owned = self.lease(lease)
        value = self.client._request("POST", self.path(job, "/failure"), encode({**owned, "error_code": code}))
        self._failure(value, job, owned, self.client.producer, code)
        return value

    @staticmethod
    def _failure(value, job, owned, producer, code):
        outcome = "retry" if code in RETRYABLE and owned["fence"] < 8 else "failed"
        if (not isinstance(value, dict) or value.get("job_uuid") != job
                or value.get("producer_uuid") != producer or value.get("owner_uuid") != owned["owner_uuid"]
                or not _integer(value.get("fence"), 1, 8) or value["fence"] != owned["fence"]
                or value.get("error_code") != code or value.get("outcome") != outcome):
            raise Unavailable("invalid_response")
        try:
            _time(value.get("started_at"), milliseconds=True)
            _time(value.get("ended_at"), milliseconds=True)
        except InvalidData:
            raise Unavailable("invalid_response") from None
