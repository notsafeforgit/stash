"""Application-authenticated physical deduplication and checked receipts."""

from http.client import HTTPException
import math
import os
from urllib.error import HTTPError, URLError
from urllib.request import Request

from .album_client import integer, sha256
from .backfill_import import ImportClient
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier

LIMIT = 16384
BLOCKED = {
    "file_requires_intake", "archive_member_requires_review", "unsupported_file_type",
    "ambiguous_or_missing_media_owner", "archive_membership_requires_review",
    "different_or_empty_files", "different_media_owners", "duplicate_has_captions",
    "source_history_requires_review", "file_requires_rescan", "same_filesystem_entry",
}
REJECTIONS = {"deduplication_preview_changed", "deduplication_bytes_differ", "file_not_found"}


def relative_path(value):
    if (not isinstance(value, str) or not value or value.startswith("/") or "\\" in value
            or any(ord(c) < 32 or ord(c) == 127 for c in value)
            or any(part in ("", ".", "..") for part in value.split("/"))):
        raise InvalidData("Expected a canonical root-relative media path")
    if len(value.encode("utf-8")) > 4096:
        raise InvalidData("Media path exceeds its byte limit")
    return value


def validate_pair(value):
    if not isinstance(value, dict) or set(value) != {"root_uuid", "keep_path", "remove_path"}:
        raise InvalidData("Invalid deduplication pair")
    identifier(value["root_uuid"])
    if relative_path(value["keep_path"]) == relative_path(value["remove_path"]):
        raise InvalidData("Deduplication paths must differ")
    return value


def validate_preview(value, pair):
    validate_pair(pair)
    if (not isinstance(value, dict) or any(value.get(k) != v for k, v in pair.items())
            or type(value.get("eligible")) is not bool or type(value.get("replace_primary")) is not bool):
        raise InvalidData("Deduplication preview differs from its pair")
    sha256(value.get("signature"))
    integer(value.get("root_revision"), 1, (1 << 63) - 1)
    integer(value.get("bytes"), 0, (1 << 63) - 1)
    integer(value.get("source_matches"), 0, 1001)
    reason = value.get("blocked_reason", "")
    if (value["eligible"] and reason) or (not value["eligible"] and reason not in BLOCKED):
        raise InvalidData("Deduplication eligibility is contradictory")
    for key in ("media_uuid", "kept_file_uuid", "removed_file_uuid"):
        if value["eligible"] or value.get(key):
            identifier(value.get(key))
    if value["eligible"] and (value["bytes"] == 0 or value["kept_file_uuid"] == value["removed_file_uuid"]):
        raise InvalidData("Eligible deduplication must identify distinct nonempty files")
    return value


def validate_request(value, pair):
    if not isinstance(value, dict) or set(value) != set(pair) | {"request_uuid", "signature"}:
        raise InvalidData("Invalid saved deduplication request")
    if any(value[k] != v for k, v in pair.items()):
        raise InvalidData("Saved deduplication request changed its pair")
    identifier(value["request_uuid"])
    sha256(value["signature"])
    return value


def validate_receipt(value, request):
    expected = {k: request[k] for k in ("root_uuid", "keep_path", "remove_path", "signature")}
    expected["uuid"] = request["request_uuid"]
    if not isinstance(value, dict) or any(value.get(k) != v for k, v in expected.items()):
        raise InvalidData("Deduplication receipt differs from the saved request")
    # Canonical identities may change after UUID adoption. The original request
    # signature and paths remain authoritative for committed replay.
    for key in ("kept_file_uuid", "removed_file_uuid", "media_uuid"):
        identifier(value.get(key))
    for key in ("kept_generation", "removed_generation"):
        integer(value.get(key), 1, (1 << 63) - 1)
    sha256(value.get("sha256"))
    source_time(value.get("committed_at"))
    return value


class DeduplicationClient(ImportClient):
    def __init__(self, endpoint, key_env="STASH_API_KEY", *, timeout=900):
        super().__init__(endpoint, key_env)
        if type(timeout) not in (int, float) or not math.isfinite(timeout) or not 1 <= timeout <= 3600:
            raise InvalidData("Deduplication request timeout must be 1–3600 seconds")
        self.timeout = timeout

    def request(self, method, path, value=None):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        request = Request(self.endpoint + "/api/v3/archive/file-deduplication" + path,
                          method=method, data=None if value is None else encode(value, LIMIT),
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_deduplication_response")
                return decode(response.read(LIMIT + 1), LIMIT)
        except HTTPError as error:
            try:
                if (error.headers.get_content_type() != "application/json"
                        or error.headers.get("Content-Encoding") is not None):
                    raise Unavailable("deduplication_request_rejected", error.code)
                body = decode(error.read(LIMIT + 1), LIMIT)
                code = body.get("error") if isinstance(body, dict) else None
                allowed = {400: {"invalid_event"}, 401: {"unauthorized"}, 403: {"outside_scope"},
                           404: {"not_found", "file_not_found"},
                           409: REJECTIONS | {"deduplication_request_changed"}}
                if not isinstance(code, str) or code not in allowed.get(error.code, set()):
                    code = "deduplication_request_rejected"
                raise Unavailable(code, error.code)
            except (InvalidData, HTTPException, OSError):
                raise Unavailable("invalid_deduplication_response", error.code) from None
            finally:
                error.close()
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_deduplication_response") from None

    def preview(self, pair):
        value = self.request("POST", "/preview", validate_pair(pair))
        try:
            return validate_preview(value, pair)
        except InvalidData:
            raise Unavailable("invalid_deduplication_preview") from None

    def apply(self, request):
        value = self.request("POST", "/apply", request)
        return self.checked_receipt(value, request)

    @staticmethod
    def checked_receipt(value, request):
        try:
            return validate_receipt(value, request)
        except InvalidData:
            raise Unavailable("invalid_deduplication_receipt") from None

    def receipt(self, request):
        try:
            value = self.request("GET", "/requests/" + identifier(request["request_uuid"]))
        except Unavailable as error:
            if error.status == 404 and error.code == "not_found":
                return None
            raise
        return self.checked_receipt(value, request)
