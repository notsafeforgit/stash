"""Application-authorized historical post/file matching and checked receipts."""

from collections import Counter
from http.client import HTTPException
import os
from urllib.error import HTTPError, URLError
from urllib.request import Request
import uuid

from .album_client import integer, sha256
from .backfill_import import ImportClient
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier

POLICY = "catalog-files-v1"
MAX_RESPONSE = 64 << 20
MAX_POSTS = 1_000_000
STATUSES = {"matched", "preserved", "review", "unavailable"}
PROOF_STATUSES = {"valid", "evidence-only", "file-changed", "owner-changed", "media-unavailable", "ambiguous-observation"}


def validate_decision(value, post):
    if not isinstance(value, dict) or value.get("post_uuid") != post:
        raise InvalidData("Post/media decision belongs to another post")
    for key in ("uuid", "media_uuid"):
        identifier(value.get(key))
    for key in ("post_revision", "media_revision"):
        integer(value.get(key), 1, (1 << 63) - 1)
    if value.get("state") not in ("linked", "unlinked", "undecided") or value.get("origin") not in ("review", "migration"):
        raise InvalidData("Unsupported post/media decision")
    source_time(value.get("created_at"))
    return value


def validate_preview(value, post):
    identifier(post)
    if (not isinstance(value, dict) or value.get("post_uuid") != post or value.get("policy") != POLICY
            or value.get("post_state") not in ("active", "forgotten")):
        raise InvalidData("Post/media preview differs from its request")
    sha256(value.get("signature"))
    integer(value.get("post_revision"), 1, (1 << 63) - 1)
    candidates = value.get("candidates")
    if not isinstance(candidates, list) or len(candidates) > 8192:
        raise InvalidData("Invalid post/media candidates")
    last, evidence = "", set()
    for candidate in candidates:
        if not isinstance(candidate, dict):
            raise InvalidData("Invalid post/media candidate")
        media = identifier(candidate.get("media_uuid"))
        if media <= last or candidate.get("media_kind") not in ("scene", "image") or candidate.get("media_state") not in ("active", "deleted"):
            raise InvalidData("Post/media candidates are not uniquely ordered media")
        last = media
        integer(candidate.get("media_revision"), 1, (1 << 63) - 1)
        if candidate.get("status") not in STATUSES:
            raise InvalidData("Unsupported post/media match status")
        decisions, proofs = candidate.get("decisions"), candidate.get("proofs")
        if not isinstance(decisions, list) or len(decisions) > 1024 or not isinstance(proofs, list) or len(proofs) > 8192:
            raise InvalidData("Invalid post/media proof or decision list")
        states, ids = set(), set()
        for decision in decisions:
            validate_decision(decision, post)
            if decision["uuid"] in ids or decision["post_revision"] > value["post_revision"]:
                raise InvalidData("Invalid current post/media decision")
            states.add(decision["state"])
            ids.add(decision["uuid"])
        state = "conflict" if len(states) > 1 else next(iter(states), "undecided")
        if candidate.get("association_state") != state:
            raise InvalidData("Preview obscures current post/media decisions")
        for proof in proofs:
            if not isinstance(proof, dict) or proof.get("basis") != "catalog-file" or proof.get("status") not in PROOF_STATUSES:
                raise InvalidData("Unsupported post/media proof")
            proof_id = identifier(proof.get("evidence_uuid"))
            if proof_id in evidence:
                raise InvalidData("Duplicate post/media evidence")
            evidence.add(proof_id)
            if len(evidence) > 8192:
                raise InvalidData("Post/media proof limit exceeded")
            if proof["status"] == "valid":
                for key in ("post_file_uuid", "match_uuid", "file_uuid", "observation_uuid"):
                    identifier(proof.get(key))
                integer(proof.get("generation"), 1, (1 << 63) - 1)
                if (proof.get("archive_file_uuid") is None) != (proof.get("archive_generation") is None):
                    raise InvalidData("Archive member proof is incomplete")
                if proof.get("archive_file_uuid") is not None:
                    identifier(proof["archive_file_uuid"])
                    integer(proof["archive_generation"], 1, (1 << 63) - 1)
        if candidate["status"] == "matched" and (decisions or value["post_state"] != "active" or candidate["media_state"] != "active"
                or not any(proof["status"] == "valid" for proof in proofs)):
            raise InvalidData("Matched media lacks current proof or overrides a choice")
        if candidate["status"] == "preserved" and (not decisions or state == "conflict"):
            raise InvalidData("Preserved media lacks an unambiguous current choice")
        if state == "conflict" and candidate["status"] != "review":
            raise InvalidData("Merged post/media choices require review")
    return value


def validate_record(record):
    if not isinstance(record, dict):
        raise InvalidData("Invalid post/media plan record")
    post = identifier(record.get("post_uuid"))
    if record.get("disposition") == "review_limit":
        if set(record) != {"post_uuid", "disposition"}:
            raise InvalidData("Invalid bounded-review record")
        return record
    if set(record) != {"post_uuid", "disposition", "preview", "request_uuid"} or record["disposition"] != "ready":
        raise InvalidData("Unsupported post/media plan record")
    identifier(record["request_uuid"])
    validate_preview(record["preview"], post)
    return record


def validate_result(value, record):
    preview = record["preview"]
    try:
        if (not isinstance(value, dict) or value.get("uuid") != record["request_uuid"] or value.get("post_uuid") != record["post_uuid"]
                or value.get("signature") != preview["signature"]):
            raise InvalidData("Post/media receipt differs from the saved request")
        counts = Counter(row["status"] for row in preview["candidates"])
        for key, status in (("selected", "matched"), ("preserved", "preserved"), ("review", "review"), ("unavailable", "unavailable")):
            if integer(value.get(key)) != counts[status]:
                raise InvalidData("Post/media outcome differs from its preview")
        expected = [row for row in preview["candidates"] if row["status"] == "matched"]
        decisions = value.get("decisions")
        if not isinstance(decisions, list) or len(decisions) != len(expected):
            raise InvalidData("Post/media receipt lacks its committed choices")
        for index, (decision, candidate) in enumerate(zip(decisions, expected)):
            validate_decision(decision, record["post_uuid"])
            expected_id = str(uuid.uuid5(uuid.UUID(record["request_uuid"]), "post-media-backfill\0" + candidate["media_uuid"]))
            if (decision["uuid"] != expected_id or decision["media_uuid"] != candidate["media_uuid"]
                    or decision["media_revision"] != candidate["media_revision"] or decision["post_revision"] != preview["post_revision"] + index + 1
                    or decision["state"] != "linked" or decision["origin"] != "migration"):
                raise InvalidData("Post/media receipt changed the selected target")
    except (InvalidData, TypeError, KeyError, ValueError):
        raise Unavailable("invalid_post_media_receipt") from None
    return value


class PostMediaClient(ImportClient):
    def request(self, method, path, value=None, limit=MAX_RESPONSE):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        request = Request(self.endpoint + "/api/v3/archive" + path, method=method,
                          data=None if value is None else encode(value, 4096),
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if response.status != 200 or response.headers.get_content_type() != "application/json" or response.headers.get("Content-Encoding") is not None:
                    raise Unavailable("invalid_post_media_response")
                return decode(response.read(limit + 1), limit)
        except HTTPError as error:
            status = error.code
            error.close()
            raise Unavailable("post_media_request_rejected", status) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_post_media_response") from None

    def posts(self):
        after, count = "", 0
        while True:
            page = self.request("GET", f"/post-media-backfill-posts?limit=100&after={after}", limit=64 << 10)
            if not isinstance(page, list) or len(page) > 100:
                raise Unavailable("invalid_post_media_discovery")
            for post in page:
                try:
                    identifier(post)
                    if post <= after:
                        raise InvalidData("Post cursor did not advance")
                except InvalidData:
                    raise Unavailable("invalid_post_media_discovery") from None
                after, count = post, count + 1
                if count > MAX_POSTS:
                    raise InvalidData("Post/media plan exceeds one million posts; use an explicit smaller scope")
                yield post
            if len(page) < 100:
                return

    def preview(self, post):
        identifier(post)
        value = self.request("GET", f"/posts/{post}/media-backfill-preview")
        try:
            return validate_preview(value, post)
        except (InvalidData, TypeError, KeyError):
            raise Unavailable("invalid_post_media_preview") from None

    def status(self, record):
        validate_record(record)
        request = identifier(record.get("request_uuid"))
        try:
            result = self.request("GET", f"/post-media-backfills/{request}")
        except Unavailable as error:
            if error.status == 404:
                return None
            raise
        return validate_result(result, record)

    def apply(self, record):
        prior = self.status(record)
        if prior is not None:
            return prior
        body = {"uuid": record["request_uuid"], "post_uuid": record["post_uuid"], "signature": record["preview"]["signature"]}
        result = self.request("POST", f"/posts/{record['post_uuid']}/media-backfills", body)
        return validate_result(result, record)
