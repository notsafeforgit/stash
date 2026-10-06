"""Review a saved discovery search against a newer collection binding."""

from urllib.parse import urlencode

from .activation_client import ActivationClient, integer, sha256
from .catalog_source import source_time
from .client import Unavailable
from .discovery_client import validate_listing_input
from .encoding import InvalidData, decode, digest, encode, identifier, native_json

MAX_HTTP_BYTES = 4 << 20
MAX_INPUT_BYTES = 16 << 10
MAX_PREVIEW_BYTES = 128 << 10
INPUT_KEYS = {"uuid", "listing_uuid", "expected_definition_sha256", "collection_revision", "reason"}
PREVIEW_KEYS = {"version", "input", "previous_review_uuid", "previous", "collection", "definition_sha256", "plan_sha256"}
COLLECTION_KEYS = {"uuid", "label", "kind", "namespace", "state", "target_url", "account_uuid", "root_uuid", "path_prefix", "revision", "created_at"}


def canonical_input(value):
    value = decode(encode(value, MAX_INPUT_BYTES), MAX_INPUT_BYTES)
    if not isinstance(value, dict):
        raise InvalidData("Invalid discovery collection review")
    value.setdefault("reason", "")
    if set(value) != INPUT_KEYS:
        raise InvalidData("Select one exact discovery definition and collection revision")
    identifier(value["uuid"])
    identifier(value["listing_uuid"])
    sha256(value["expected_definition_sha256"])
    integer(value["collection_revision"], 1)
    if not isinstance(value["reason"], str) or len(value["reason"].encode("utf-8")) > 4096:
        raise InvalidData("Invalid discovery review reason")
    return value


def listing_definition(value):
    if not isinstance(value, dict) or not {"sha256", "created_at"} <= set(value):
        raise InvalidData("Invalid saved discovery definition")
    definition = {key: item for key, item in value.items() if key not in ("sha256", "created_at")}
    validate_listing_input(definition)
    source_time(value["created_at"])
    if digest(native_json(definition, 32768)) != sha256(value["sha256"]):
        raise InvalidData("Discovery definition differs from its pinned digest")
    return definition


def validate_preview(value, expected=None):
    if not isinstance(value, dict) or set(value) != PREVIEW_KEYS or integer(value["version"], 1, 1) != 1:
        raise InvalidData("Invalid discovery collection preview")
    selection = canonical_input(value["input"])
    if encode(selection) != encode(value["input"]) or expected is not None and encode(selection) != encode(expected):
        raise InvalidData("Discovery review changed the selected definition")
    previous = value["previous"]
    definition = listing_definition(previous)
    if previous["uuid"] != selection["listing_uuid"] or previous["sha256"] != selection["expected_definition_sha256"]:
        raise InvalidData("Discovery review changed the previous definition")
    reference = value["previous_review_uuid"]
    if reference is not None and identifier(reference) == selection["uuid"]:
        raise InvalidData("Discovery review cannot refer to itself")
    collection = value["collection"]
    if (not isinstance(collection, dict) or set(collection) != COLLECTION_KEYS or collection["uuid"] != previous["collection_uuid"]
            or collection["revision"] != selection["collection_revision"] or collection["state"] != "active"):
        raise InvalidData("Discovery review changed its destination collection")
    integer(collection["revision"], previous["collection_revision"] + 1)
    source_time(collection["created_at"])
    for key in ("label", "kind", "namespace", "target_url", "path_prefix"):
        if not isinstance(collection[key], str):
            raise InvalidData("Invalid reviewed collection definition")
    for key in ("account_uuid", "root_uuid"):
        if collection[key] is not None:
            identifier(collection[key])
    proposed = definition | {"collection_revision": collection["revision"], "root_uuid": collection["root_uuid"]}
    if digest(native_json(proposed, 32768)) != sha256(value["definition_sha256"]):
        raise InvalidData("Discovery review changed more than the collection binding")
    sha256(value["plan_sha256"])
    encode(value, MAX_PREVIEW_BYTES)
    return value


def validate_receipt(value, preview):
    validate_preview(preview)
    if (not isinstance(value, dict) or set(value) != PREVIEW_KEYS | {"created_at"}
            or encode({key: value[key] for key in PREVIEW_KEYS}, MAX_PREVIEW_BYTES) != encode(preview, MAX_PREVIEW_BYTES)):
        raise InvalidData("Discovery receipt differs from the saved review")
    source_time(value["created_at"])
    return value


class DiscoveryScopeClient(ActivationClient):
    kind = "discovery_scope"
    max_http_bytes = MAX_HTTP_BYTES

    def candidates(self, collection, revision, *, after=None, limit=100):
        identifier(collection)
        integer(revision, 1)
        integer(limit, 1, 100)
        query = {"collection_revision": revision, "limit": limit}
        if after is not None:
            query["after"] = identifier(after)
        result = self.request("GET", f"/collections/{collection}/discovery-scope-candidates?" + urlencode(query))
        try:
            if not isinstance(result, list) or len(result) > limit:
                raise InvalidData("Invalid discovery candidate page")
            for row in result:
                if (not isinstance(row, dict) or set(row) != {"listing", "current_collection_revision", "collection_state", "disposition"}
                        or integer(row["current_collection_revision"], 1) != revision or row["collection_state"] not in ("active", "disabled", "retired")
                        or row["disposition"] not in ("eligible", "collection_disabled", "collection_changed", "source_changed", "search_replaced", "worker_history")):
                    raise InvalidData("Invalid discovery candidate")
                listing_definition(row["listing"])
                if row["listing"]["collection_uuid"] != collection or after is not None and row["listing"]["uuid"] <= after:
                    raise InvalidData("Discovery candidates changed their collection or ordering")
                if row["disposition"] == "eligible" and row["listing"]["collection_revision"] >= revision:
                    raise InvalidData("Discovery candidate has no newer collection binding")
                after = row["listing"]["uuid"]
        except (InvalidData, KeyError, TypeError, ValueError):
            raise Unavailable("invalid_discovery_scope_candidates") from None
        return result

    def preview(self, value):
        expected = canonical_input(value)
        result = self.request("POST", "/discovery-scope-reviews/preview", expected)
        try:
            return validate_preview(result, expected)
        except (InvalidData, KeyError, TypeError, ValueError):
            raise Unavailable("invalid_discovery_scope_preview") from None

    def status(self, preview):
        validate_preview(preview)
        try:
            result = self.request("GET", "/discovery-scope-reviews/" + identifier(preview["input"]["uuid"]))
        except Unavailable as error:
            if error.status == 404:
                return None
            raise
        return self._receipt(result, preview)

    @staticmethod
    def _receipt(result, preview):
        try:
            return validate_receipt(result, preview)
        except (InvalidData, KeyError, TypeError, ValueError):
            raise Unavailable("invalid_discovery_scope_receipt") from None

    def apply(self, preview):
        previous = self.status(preview)
        if previous is not None:
            return previous
        result = self.request("POST", "/discovery-scope-reviews", {"input": preview["input"], "expected_plan_sha256": preview["plan_sha256"]})
        return self._receipt(result, preview)
