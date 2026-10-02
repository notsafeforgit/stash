"""Bounded application operations for reviewed translation activation."""

from http.client import HTTPException
import os
from urllib.error import HTTPError, URLError
from urllib.request import Request
import uuid

from .backfill_import import ImportClient
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier
from .events import sha256 as valid_sha256

MAX_BATCH = 100
MAX_HTTP_BYTES = 128 << 10
MAX_CANDIDATES = 1000000
MAX_REVISION = (1 << 31) - 2
DISPOSITIONS = {"eligible", "changed", "completed", "post_forgotten"}


def sha256(value):
    if not valid_sha256(value):
        raise InvalidData("Expected a SHA-256 digest")
    return value


def integer(value, minimum=0, maximum=(1 << 63) - 1):
    if type(value) is not int or not minimum <= value <= maximum:
        raise InvalidData("Invalid translation activation counter")
    return value


def validate_candidate(row, after):
    required = {"ordinal", "target_uuid", "revision", "current_revision", "state", "post_state",
                "request_uuid", "post_uuid", "field", "priority", "not_before", "disposition"}
    if not isinstance(row, dict) or not required <= set(row) or set(row) - required - {"collection_uuid", "collection_revision"}:
        raise InvalidData("Invalid translation candidate")
    integer(row["ordinal"], after + 1)
    for key in ("target_uuid", "request_uuid", "post_uuid"):
        identifier(row[key])
    integer(row["revision"], 1, MAX_REVISION)
    integer(row["current_revision"], row["revision"], MAX_REVISION + 1)
    integer(row["priority"], 0, 100)
    source_time(row["not_before"])
    if ("collection_uuid" in row) != ("collection_revision" in row):
        raise InvalidData("Incomplete translation collection binding")
    if "collection_uuid" in row:
        identifier(row["collection_uuid"])
        integer(row["collection_revision"], 1, MAX_REVISION + 1)
    if (row["state"] not in ("held", "pending", "completed", "review")
            or row["post_state"] not in ("active", "forgotten") or row["field"] not in ("title", "caption")):
        raise InvalidData("Invalid translation candidate state")
    expected = ("post_forgotten" if row["post_state"] != "active" else
                "completed" if row["state"] == "completed" else
                "changed" if row["state"] != "held" or row["current_revision"] != row["revision"] else "eligible")
    if row["disposition"] != expected:
        raise InvalidData("Translation candidate disposition contradicts its state")
    return row


def activation_input(operation, snapshot, manifest_sha256, candidates):
    return {"uuid": identifier(operation), "snapshot_uuid": identifier(snapshot), "manifest_sha256": sha256(manifest_sha256),
            "targets": sorted(({"target_uuid": row["target_uuid"], "revision": row["revision"]}
                               for row in candidates if row["disposition"] == "eligible"), key=lambda row: row["target_uuid"])}


def validate_preview(value, expected, candidates):
    if (not isinstance(value, dict) or set(value) != {"version", "input", "entries", "plan_sha256"}
            or type(value["version"]) is not int or value["version"] != 1 or encode(value["input"]) != encode(expected)
            or not isinstance(value["entries"], list) or not 1 <= len(value["entries"]) <= MAX_BATCH
            or len(value["entries"]) != len(expected["targets"])):
        raise InvalidData("Translation preview differs from its requested targets")
    sha256(value["plan_sha256"])
    by_target = {row["target_uuid"]: row for row in candidates if row["disposition"] == "eligible"}
    for entry, ref in zip(value["entries"], expected["targets"]):
        candidate = by_target[ref["target_uuid"]]
        fields = {"target_uuid", "revision", "request_uuid", "post_uuid", "field", "priority", "not_before"}
        if "collection_uuid" in candidate:
            fields |= {"collection_uuid", "collection_revision"}
        if not isinstance(entry, dict) or set(entry) != fields or encode(entry) != encode({key: candidate[key] for key in fields}):
            raise InvalidData("Translation preview changed an identity, schedule or source binding")
    return value


def validate_receipt(value, preview):
    if (not isinstance(value, dict) or set(value) != set(preview) | {"activated", "created_at"}
            or encode({key: value[key] for key in preview}) != encode(preview)):
        raise InvalidData("Activation receipt differs from the saved preview")
    expected = [{"target_uuid": ref["target_uuid"], "revision": ref["revision"] + 1} for ref in preview["input"]["targets"]]
    if encode(value["activated"]) != encode(expected):
        raise InvalidData("Activation receipt changed its released revisions")
    source_time(value["created_at"])
    return value


class TranslationActivationClient(ImportClient):
    def request(self, method, path, value=None):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        request = Request(self.endpoint + "/api/v3/archive" + path, method=method,
                          data=None if value is None else encode(value, MAX_HTTP_BYTES),
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_translation_activation_response")
                return decode(response.read(MAX_HTTP_BYTES + 1), MAX_HTTP_BYTES)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable("translation_activation_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_translation_activation_response") from None

    def candidate_pages(self, snapshot, manifest_sha256):
        identifier(snapshot)
        sha256(manifest_sha256)
        after, count = 0, 0
        while True:
            page = self.request("GET", f"/automation-snapshots/{snapshot}/translation-import/held-targets"
                                f"?expected_manifest_sha256={manifest_sha256}&after={after}&limit={MAX_BATCH}")
            try:
                if not isinstance(page, list) or len(page) > MAX_BATCH:
                    raise InvalidData("Invalid translation candidate page")
                for row in page:
                    validate_candidate(row, after)
                    after = row["ordinal"]
            except (InvalidData, KeyError, TypeError):
                raise Unavailable("invalid_translation_candidates") from None
            count += len(page)
            if count > MAX_CANDIDATES:
                raise InvalidData("Translation plan exceeds one million original holds")
            if page:
                yield page
            if len(page) < MAX_BATCH:
                return

    def preview(self, snapshot, manifest_sha256, candidates):
        expected = activation_input(str(uuid.uuid4()), snapshot, manifest_sha256, candidates)
        result = self.request("POST", "/translation-activations/preview", expected)
        try:
            return validate_preview(result, expected, candidates)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_translation_preview") from None

    def status(self, preview):
        try:
            result = self.request("GET", "/translation-activations/" + identifier(preview["input"]["uuid"]))
        except Unavailable as error:
            if error.status == 404:
                return None
            raise
        try:
            return validate_receipt(result, preview)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_translation_receipt") from None

    def apply(self, preview):
        previous = self.status(preview)
        if previous is not None:
            return previous
        result = self.request("POST", "/translation-activations", {"input": preview["input"], "expected_plan_sha256": preview["plan_sha256"]})
        try:
            return validate_receipt(result, preview)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_translation_receipt") from None
