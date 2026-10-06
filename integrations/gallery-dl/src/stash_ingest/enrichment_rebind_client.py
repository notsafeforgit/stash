"""Review unstarted metadata work against a newer collection definition."""

import uuid
from urllib.parse import urlencode

from .activation_client import ActivationClient, integer, sha256
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier, native_json
from .enrichment_activation_client import ENTRY_FIELDS
from .metadata_bundle import public_url

MAX_BATCH = 100
MAX_HTTP_BYTES = 8 << 20
MAX_INPUT_BYTES = 64 << 10
MAX_REVISION = (1 << 63) - 3
POLICY = "gallery-dl-metadata-v1"
PREVIEW_KEYS = {"version", "input", "collection", "activation", "plan_sha256"}
COLLECTION_KEYS = {"uuid", "label", "kind", "namespace", "state", "target_url", "account_uuid", "root_uuid", "path_prefix", "revision", "created_at"}
TARGET_KEYS = {"uuid", "post_uuid", "url_uuid", "url", "collection_uuid", "collection_revision", "policy", "origin", "state",
               "priority", "not_before", "reason", "revision", "created_at", "updated_at"}


def canonical_input(value):
    value = decode(encode(value, MAX_INPUT_BYTES), MAX_INPUT_BYTES)
    if not isinstance(value, dict) or set(value) != {"uuid", "collection_uuid", "collection_revision", "targets", "reason"}:
        raise InvalidData("Invalid collection review input")
    identifier(value["uuid"])
    identifier(value["collection_uuid"])
    integer(value["collection_revision"], 2)
    if not isinstance(value["reason"], str) or len(value["reason"].encode("utf-8")) > 4096:
        raise InvalidData("Collection review reason exceeds its limit")
    if not isinstance(value["targets"], list) or not 1 <= len(value["targets"]) <= MAX_BATCH:
        raise InvalidData("Select between 1 and 100 pending targets")
    seen = set()
    for target in value["targets"]:
        if not isinstance(target, dict) or set(target) != {"target_uuid", "revision"}:
            raise InvalidData("Invalid collection review target")
        identifier(target["target_uuid"])
        integer(target["revision"], 1, MAX_REVISION)
        if target["target_uuid"] in seen:
            raise InvalidData("Duplicate collection review target")
        seen.add(target["target_uuid"])
    value["targets"].sort(key=lambda target: target["target_uuid"])
    return value


def target_identity(post, url, collection, revision):
    body = native_json(["stash-enrichment-target-v1", identifier(post), identifier(url), identifier(collection), integer(revision, 1), POLICY], 4096)
    return str(uuid.uuid5(uuid.NAMESPACE_URL, body.decode("utf-8")))


def validate_candidate(row, collection, revision, after=None):
    keys = {"target", "current_collection_revision", "collection_state", "post_state", "post_revision", "released_target_uuid", "disposition"}
    if not isinstance(row, dict) or set(row) != keys or not isinstance(row["target"], dict) or set(row["target"]) != TARGET_KEYS:
        raise InvalidData("Invalid collection review candidate")
    target = row["target"]
    for key in ("uuid", "post_uuid", "url_uuid", "collection_uuid"):
        identifier(target[key])
    integer(target["revision"], 1, MAX_REVISION + 2)
    integer(target["collection_revision"], 1, revision - 1)
    integer(row["current_collection_revision"], revision, revision)
    integer(row["post_revision"], 1)
    integer(target["priority"], 0, 100)
    for key in ("not_before", "created_at", "updated_at"):
        source_time(target[key])
    public_url(target["url"])
    if (target["collection_uuid"] != collection or target["state"] != "pending" or target["policy"] != POLICY
            or target["origin"] not in ("review", "migration") or not isinstance(target["reason"], str)
            or row["collection_state"] not in ("active", "disabled", "retired") or row["post_state"] not in ("active", "forgotten")):
        raise InvalidData("Collection review candidate has a different scope")
    if (target["uuid"] != target_identity(target["post_uuid"], target["url_uuid"], collection, target["collection_revision"])
            or row["released_target_uuid"] != target_identity(target["post_uuid"], target["url_uuid"], collection, revision)):
        raise InvalidData("Collection review changed a target identity")
    cursor = (target["collection_revision"], target["uuid"])
    if after is not None and cursor <= after:
        raise InvalidData("Collection review cursor did not advance")
    possible = ({"collection_disabled"} if row["collection_state"] != "active" else
                {"post_forgotten"} if row["post_state"] != "active" else
                {"target_changed"} if target["revision"] > MAX_REVISION else {"eligible", "worker_history", "replacement_exists"})
    if row["disposition"] not in possible:
        raise InvalidData("Collection review disposition contradicts its state")
    return row


def validate_preview(value, expected=None):
    if not isinstance(value, dict) or set(value) != PREVIEW_KEYS or integer(value["version"], 1, 1) != 1:
        raise InvalidData("Invalid collection review preview")
    source = canonical_input(value["input"])
    if encode(source) != encode(value["input"]) or expected is not None and encode(source) != encode(expected):
        raise InvalidData("Collection review changed its selected targets")
    collection = value["collection"]
    if (not isinstance(collection, dict) or set(collection) != COLLECTION_KEYS or collection["uuid"] != source["collection_uuid"]
            or collection["revision"] != source["collection_revision"] or collection["state"] != "active"):
        raise InvalidData("Collection review changed its destination definition")
    integer(collection["revision"], 2)
    source_time(collection["created_at"])
    for key in ("label", "kind", "namespace", "target_url", "path_prefix"):
        if not isinstance(collection[key], str):
            raise InvalidData("Invalid collection definition")
    for key in ("account_uuid", "root_uuid"):
        if collection[key] is not None:
            identifier(collection[key])
    nested = value["activation"]
    if not isinstance(nested, dict) or set(nested) != {"version", "input", "entries", "plan_sha256"} or integer(nested["version"], 1, 1) != 1:
        raise InvalidData("Invalid collection review activation")
    operation = str(uuid.uuid5(uuid.UUID(source["uuid"]), "enrichment-collection-rebind-v1"))
    held = [{"target_uuid": ref["target_uuid"], "revision": ref["revision"] + 1, "collection_revision": source["collection_revision"]} for ref in source["targets"]]
    if (encode(nested["input"]) != encode({"uuid": operation, "targets": held})
            or not isinstance(nested["entries"], list) or len(nested["entries"]) != len(held)):
        raise InvalidData("Collection review changed its intermediate holds")
    released = set()
    for entry, ref in zip(nested["entries"], held):
        if (not isinstance(entry, dict) or set(entry) != ENTRY_FIELDS or entry["target_uuid"] != ref["target_uuid"]
                or integer(entry["revision"], 2) != ref["revision"] or integer(entry["released_revision"], 1, 1) != 1
                or entry["collection_uuid"] != source["collection_uuid"] or entry["activation_collection_revision"] != source["collection_revision"]
                or entry["policy"] != POLICY):
            raise InvalidData("Collection review changed its source or destination work")
        integer(entry["activation_collection_revision"], 2)
        integer(entry["collection_revision"], 1, source["collection_revision"] - 1)
        integer(entry["post_revision"], 1)
        integer(entry["priority"], 0, 100)
        source_time(entry["not_before"])
        public_url(entry["url"])
        if (entry["target_uuid"] != target_identity(entry["post_uuid"], entry["url_uuid"], entry["collection_uuid"], entry["collection_revision"])
                or entry["released_target_uuid"] != target_identity(entry["post_uuid"], entry["url_uuid"], entry["collection_uuid"], source["collection_revision"])
                or entry["released_target_uuid"] in released):
            raise InvalidData("Collection review changed or repeated a target identity")
        released.add(entry["released_target_uuid"])
    sha256(nested["plan_sha256"])
    sha256(value["plan_sha256"])
    encode(value, MAX_HTTP_BYTES)
    return value


def validate_receipt(value, preview):
    validate_preview(preview)
    if (not isinstance(value, dict) or set(value) != PREVIEW_KEYS | {"created_at"}
            or encode({key: value[key] for key in PREVIEW_KEYS}, MAX_HTTP_BYTES) != encode(preview, MAX_HTTP_BYTES)):
        raise InvalidData("Collection review receipt differs from its saved plan")
    source_time(value["created_at"])
    return value


class EnrichmentRebindClient(ActivationClient):
    kind = "enrichment_rebind"
    max_http_bytes = MAX_HTTP_BYTES

    def candidates(self, collection, revision, *, after=None, limit=MAX_BATCH):
        identifier(collection)
        integer(revision, 1)
        integer(limit, 1, MAX_BATCH)
        query = {"collection_revision": revision, "limit": limit}
        if after is not None:
            query.update(after_collection_revision=integer(after[0], 1, revision - 1), after_target=identifier(after[1]))
        result = self.request("GET", f"/collections/{collection}/enrichment-rebind-candidates?" + urlencode(query))
        try:
            if not isinstance(result, list) or len(result) > limit:
                raise InvalidData("Invalid candidate page")
            for row in result:
                validate_candidate(row, collection, revision, after)
                after = (row["target"]["collection_revision"], row["target"]["uuid"])
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_enrichment_rebind_candidates") from None
        return result

    def preview(self, value):
        expected = canonical_input(value)
        result = self.request("POST", "/enrichment-rebindings/preview", expected)
        try:
            return validate_preview(result, expected)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_enrichment_rebind_preview") from None

    def status(self, preview):
        validate_preview(preview)
        try:
            result = self.request("GET", "/enrichment-rebindings/" + identifier(preview["input"]["uuid"]))
        except Unavailable as error:
            if error.status == 404:
                return None
            raise
        return self._receipt(result, preview)

    @staticmethod
    def _receipt(result, preview):
        try:
            return validate_receipt(result, preview)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_enrichment_rebind_receipt") from None

    def apply(self, preview):
        previous = self.status(preview)
        if previous is not None:
            return previous
        result = self.request("POST", "/enrichment-rebindings", {"input": preview["input"], "expected_plan_sha256": preview["plan_sha256"]})
        return self._receipt(result, preview)
