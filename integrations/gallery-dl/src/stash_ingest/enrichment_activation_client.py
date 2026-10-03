"""Enrichment-specific source scope, schedule and preview validation."""

from .activation_client import ActivationClient, activation_input as _activation_input, integer, sha256, validate_receipt as _receipt
from .catalog_source import source_time
from .encoding import InvalidData, encode, identifier
from .metadata_bundle import public_url

MAX_BATCH = 100
MAX_HTTP_BYTES = 8 << 20
MAX_CANDIDATES = 1000000
MAX_REVISION = (1 << 31) - 2
DISPOSITIONS = {"eligible", "changed", "completed", "post_forgotten", "replacement_exists", "collection_retired", "collection_disabled"}
ENTRY_FIELDS = {"released_target_uuid", "released_revision", "activation_collection_revision", "target_uuid", "revision", "post_uuid", "post_revision", "url_uuid", "url",
                "collection_uuid", "collection_revision", "policy", "priority", "not_before"}


def validate_candidate(row, after):
    fields = ENTRY_FIELDS | {"ordinal", "current_revision", "state", "post_state", "current_collection_revision",
                             "collection_state", "disposition"}
    if not isinstance(row, dict) or set(row) != fields:
        raise InvalidData("Invalid enrichment candidate")
    integer(row["ordinal"], after + 1)
    for key in ("target_uuid", "released_target_uuid", "post_uuid", "url_uuid", "collection_uuid"):
        identifier(row[key])
    integer(row["revision"], 1, MAX_REVISION)
    integer(row["current_revision"], row["revision"], MAX_REVISION + 1)
    for key in ("post_revision", "collection_revision", "current_collection_revision"):
        integer(row[key], 1, MAX_REVISION + 1)
    integer(row["activation_collection_revision"], row["current_collection_revision"], row["current_collection_revision"])
    integer(row["released_revision"], 1, MAX_REVISION + 1)
    same_scope = row["collection_revision"] == row["activation_collection_revision"]
    if ((row["target_uuid"] == row["released_target_uuid"]) != same_scope
            or row["released_revision"] != (row["revision"] + 1 if same_scope else 1)):
        raise InvalidData("Enrichment release does not match its reviewed collection revision")
    integer(row["priority"], 0, 100)
    source_time(row["not_before"])
    public_url(row["url"])
    if (row["state"] not in ("held", "pending", "completed", "review", "excluded")
            or row["post_state"] not in ("active", "forgotten") or row["collection_state"] not in ("active", "disabled", "retired")
            or row["policy"] != "gallery-dl-metadata-v1" or row["current_collection_revision"] < row["collection_revision"]):
        raise InvalidData("Invalid enrichment candidate scope or state")
    expected = ("post_forgotten" if row["post_state"] != "active" else
                "completed" if row["state"] == "completed" else
                "collection_retired" if row["collection_state"] == "retired" else
                "collection_disabled" if row["collection_state"] != "active" else
                "changed" if row["state"] != "held" or row["current_revision"] != row["revision"] else "eligible")
    if expected == "eligible" and not same_scope and row["disposition"] == "replacement_exists":
        expected = "replacement_exists"
    if row["disposition"] != expected:
        raise InvalidData("Enrichment candidate disposition contradicts its state")
    return row


def activation_input(operation, snapshot, manifest_sha256, candidates):
    value = _activation_input(operation, snapshot, manifest_sha256, candidates)
    by_target = {row["target_uuid"]: row for row in candidates}
    for ref in value["targets"]:
        ref["collection_revision"] = by_target[ref["target_uuid"]]["activation_collection_revision"]
    return value


def validate_preview(value, expected, candidates):
    if (not isinstance(value, dict) or set(value) != {"version", "input", "entries", "plan_sha256"}
            or type(value["version"]) is not int or value["version"] != 1 or encode(value["input"]) != encode(expected)
            or not isinstance(value["entries"], list) or not 1 <= len(value["entries"]) <= MAX_BATCH
            or len(value["entries"]) != len(expected["targets"])):
        raise InvalidData("Enrichment preview differs from its requested targets")
    sha256(value["plan_sha256"])
    by_target = {row["target_uuid"]: row for row in candidates if row["disposition"] == "eligible"}
    for entry, ref in zip(value["entries"], expected["targets"]):
        candidate = by_target[ref["target_uuid"]]
        if (not isinstance(entry, dict) or set(entry) != ENTRY_FIELDS
                or encode(entry) != encode({key: candidate[key] for key in ENTRY_FIELDS})):
            raise InvalidData("Enrichment preview changed its URL, source revision or schedule")
    return value


def validate_receipt(value, preview):
    return _receipt(value, preview, MAX_HTTP_BYTES, [{"target_uuid": entry["released_target_uuid"], "revision": entry["released_revision"]} for entry in preview["entries"]])


class EnrichmentActivationClient(ActivationClient):
    kind = "enrichment"
    activation_input = staticmethod(activation_input)
    max_batch = MAX_BATCH
    max_http_bytes = MAX_HTTP_BYTES
    max_candidates = MAX_CANDIDATES
    validate_candidate = staticmethod(validate_candidate)
    validate_preview = staticmethod(validate_preview)
    validate_receipt = staticmethod(validate_receipt)
