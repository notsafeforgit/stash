"""Application-authorized source album operations and checked responses."""

from datetime import datetime
from http.client import HTTPException
import os
import re
from urllib.error import HTTPError, URLError
from urllib.request import Request

from .backfill_import import ImportClient
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier
from .post_identity import current_post

POLICIES = ("source-identifiers-v1", "legacy-reddit-filename-v1", "legacy-twitter-filename-v1")
MAX_PREVIEW_BYTES = 64 << 20
MAX_POSTS = 100000
ACTIONS = {"create", "sync", "disabled", "ineligible", "review"}
STATES = {"queued", "running", "succeeded", "failed", "cancelled"}


def sha256(value):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{64}", value):
        raise InvalidData("Expected a SHA-256 digest")
    return value


def integer(value, minimum=0, maximum=8192):
    if type(value) is not int or not minimum <= value <= maximum:
        raise InvalidData("Invalid album counter")
    return value


def validate_preview(value, post, policy):
    if (not isinstance(value, dict) or value.get("post_uuid") != identifier(post)
            or value.get("policy") != policy or policy not in POLICIES or value.get("action") not in ACTIONS):
        raise InvalidData("Album preview does not match its request")
    sha256(value.get("signature"))
    for key in ("entries", "add", "remove", "matches"):
        if not isinstance(value.get(key), list) or len(value[key]) > 8192 or any(not isinstance(row, dict) for row in value[key]):
            raise InvalidData("Invalid album preview list")
    positions = []
    for entry in value["entries"]:
        positions.append(integer(entry.get("position"), 0, 2147483647))
        identifier(entry.get("attachment_uuid"))
        if entry.get("media_uuid") is not None:
            identifier(entry["media_uuid"])
    if positions != sorted(set(positions)):
        raise InvalidData("Album preview positions are not uniquely ordered")
    for key in ("add", "remove"):
        ids = []
        for entity in value[key]:
            ids.append(identifier(entity.get("uuid")))
            if entity.get("kind") not in ("scene", "image"):
                raise InvalidData("Album member must be an image or scene")
        if len(set(ids)) != len(ids):
            raise InvalidData("Duplicate album member")
    attachments = []
    for match in value["matches"]:
        attachments.append(identifier(match.get("attachment_uuid")))
        if match.get("status") not in ("matched", "preserved", "ambiguous", "review", "unavailable"):
            raise InvalidData("Unsupported album match status")
    if len(set(attachments)) != len(attachments):
        raise InvalidData("Duplicate attachment decision")
    return value


def validate_publication(value, post):
    if not isinstance(value, dict) or value.get("post_uuid") != post:
        raise InvalidData("Publication belongs to a different post")
    identifier(value.get("event_uuid"))
    action = value.get("action")
    if action not in ACTIONS - {"review"} or type(value.get("created")) is not bool or value["created"] != (action == "create"):
        raise InvalidData("Invalid album publication action")
    for key in ("selected", "review", "unavailable", "added", "removed"):
        integer(value.get(key))
    if sum(value[key] for key in ("selected", "review", "unavailable")) > 8192:
        raise InvalidData("Album publication exceeds its limit")
    changed = value["created"] or value["added"] or value["removed"]
    if value.get("gallery_uuid"):
        identifier(value["gallery_uuid"])
    if changed and not value.get("gallery_uuid"):
        raise InvalidData("Changed gallery identity is missing")
    if action in ("disabled", "ineligible") and any(value[key] for key in ("selected", "review", "unavailable", "added", "removed")):
        raise InvalidData("No-op album claims a mutation")
    return value


def validate_status(value, record):
    preview, operation = record["preview"], record["operation"]
    try:
        if (not isinstance(value, dict) or value.get("post_uuid") != preview["post_uuid"]
                or value.get("policy") != preview["policy"] or value.get("signature") != preview["signature"]
                or value.get("state") not in STATES):
            raise InvalidData("Album job differs from the saved review")
        identifier(value.get("job_uuid"))
        for key in ("sequence", "revision"):
            integer(value.get(key), 1, (1 << 63) - 1)
        maximum = integer(value.get("max_attempts"), 1, 100)
        integer(value.get("attempts"), 0, maximum)
        for key in ("publication_committed", "hooks_finished"):
            if type(value.get(key)) is not bool:
                raise InvalidData("Album progress must be explicit")
        if value["hooks_finished"] != (value["state"] == "succeeded"):
            raise InvalidData("Incomplete notification delivery cannot report success")
        for key in ("created_at", "updated_at", "available_at"):
            source_time(value.get(key))
        if datetime.fromisoformat(value["updated_at"]) < datetime.fromisoformat(value["created_at"]):
            raise InvalidData("Album job timestamps regressed")
        if not isinstance(value.get("error_code", ""), str) or not re.fullmatch(r"[a-z0-9_.-]{0,128}", value.get("error_code", "")):
            raise InvalidData("Invalid album error code")
        expected_parent = operation.get("parent_job_uuid", "")
        if value.get("resume_from_job_uuid", "") != expected_parent:
            raise InvalidData("Album retry changed its parent")
        publication = value.get("publication")
        if value["publication_committed"] != (publication is not None) or (value["hooks_finished"] and publication is None):
            raise InvalidData("Album publication status is contradictory")
        resume = operation.get("resume_publication")
        if resume is not None and publication != resume:
            raise InvalidData("Retry lost or replaced its committed publication")
        if publication is not None:
            validate_publication(publication, preview["post_uuid"])
            if resume is None and publication["event_uuid"] != value["job_uuid"]:
                raise InvalidData("Album publication event identity differs from its job")
            expected = {"action": preview["action"], "added": len(preview["add"]), "removed": len(preview["remove"]),
                        "selected": sum(m["status"] == "matched" for m in preview["matches"]),
                        "review": sum(m["status"] in ("review", "ambiguous") for m in preview["matches"]),
                        "unavailable": sum(m["status"] == "unavailable" for m in preview["matches"])}
            if any(publication[key] != expected_value for key, expected_value in expected.items()):
                raise InvalidData("Published changes differ from the reviewed preview")
            if preview.get("gallery") and publication.get("gallery_uuid") != preview["gallery"]["uuid"]:
                raise InvalidData("Album publication changed its gallery")
    except (InvalidData, KeyError, TypeError, ValueError):
        raise Unavailable("invalid_album_job_status") from None
    return value


class AlbumClient(ImportClient):
    def request(self, method, path, value=None, *, limit=64 << 10, accepted=(200,)):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        request = Request(self.endpoint + "/api/v3/archive" + path, method=method,
                          data=None if value is None else encode(value, 4096),
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status not in accepted or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_album_response")
                return response.status, decode(response.read(limit + 1), limit)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable("album_request_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_album_response") from None

    def selected_posts(self, policy=None):
        if policy is not None and policy not in POLICIES:
            raise InvalidData("Unsupported album matching policy")
        after, count = "", 0
        while True:
            _, page = self.request("GET", f"/album-backfill-posts?limit=100&after={after}" + (f"&policy={policy}" if policy else ""))
            if not isinstance(page, list) or len(page) > 100:
                raise Unavailable("invalid_album_discovery")
            for row in page:
                try:
                    post = identifier(row["post_uuid"])
                    if row["mode"] == "unselected" and policy == "legacy-twitter-filename-v1":
                        if row.get("selection_uuid"):
                            raise InvalidData("Unselected post has a saved selection")
                    else:
                        identifier(row["selection_uuid"])
                        if row["mode"] == "unselected":
                            raise InvalidData("Unexpected unselected post")
                    if post <= after or row["post_state"] not in ("active", "forgotten") or row["mode"] not in ("automatic", "pinned", "disabled", "unselected"):
                        raise InvalidData("Invalid selected-post page")
                except (KeyError, TypeError, InvalidData):
                    raise Unavailable("invalid_album_discovery") from None
                after, count = post, count + 1
                if count > MAX_POSTS:
                    raise InvalidData("Album plan exceeds 100000 posts; use explicit smaller post lists")
                yield row
            if len(page) < 100:
                break

    def current_post(self, post):
        identifier(post)
        _, value = self.request("GET", f"/posts/{post}/identity", limit=16 << 10)
        try:
            return current_post(value, post)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_post_identity") from None

    def preview(self, post, policy):
        identifier(post)
        if policy not in POLICIES:
            raise InvalidData("Unsupported album matching policy")
        _, value = self.request("POST", f"/posts/{post}/album-backfill/preview", {"policy": policy}, limit=MAX_PREVIEW_BYTES)
        try:
            return validate_preview(value, post, policy)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_album_preview") from None

    def status(self, record):
        request = identifier(record["operation"]["request_uuid"])
        try:
            _, value = self.request("GET", f"/album-backfill-requests/{request}")
        except Unavailable as error:
            if error.status == 404:
                return None
            raise
        return validate_status(value, record)

    def apply(self, record):
        prior = self.status(record)
        if prior is not None:
            return prior
        preview, operation = record["preview"], record["operation"]
        if operation["kind"] == "apply":
            path = f"/posts/{preview['post_uuid']}/album-backfills"
            body = {"request_uuid": operation["request_uuid"], "policy": preview["policy"], "signature": preview["signature"]}
        else:
            path = f"/album-backfills/{operation['parent_job_uuid']}/retry"
            body = {"request_uuid": operation["request_uuid"], "expected_revision": operation["expected_revision"]}
        code, value = self.request("POST", path, body, accepted=(200, 202))
        result = validate_status(value, record)
        if (code == 202) != (result["state"] in ("queued", "running")):
            raise Unavailable("invalid_album_admission_status")
        return result

    def cancel(self, record, revision):
        integer(revision, 1, (1 << 63) - 1)
        prior = self.status(record)
        if prior is None:
            raise InvalidData("This album request has not been submitted")
        if prior["state"] == "cancelled":
            return prior
        _, value = self.request("POST", f"/album-backfills/{prior['job_uuid']}/cancel", {"expected_revision": revision})
        result = validate_status(value, record)
        if result["job_uuid"] != prior["job_uuid"] or result["state"] != "cancelled":
            raise Unavailable("invalid_album_cancellation")
        return result
