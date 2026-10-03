"""Translation-specific bounds and review semantics."""

from .activation_client import ActivationClient, activation_input, integer, sha256, validate_receipt as _receipt
from .catalog_source import source_time
from .encoding import InvalidData, encode, identifier

MAX_BATCH = 100
MAX_HTTP_BYTES = 128 << 10
MAX_CANDIDATES = 1000000
MAX_REVISION = (1 << 31) - 2
DISPOSITIONS = {"eligible", "changed", "completed", "post_forgotten"}


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
    return _receipt(value, preview, MAX_HTTP_BYTES)


class TranslationActivationClient(ActivationClient):
    kind = "translation"
    max_batch = MAX_BATCH
    max_http_bytes = MAX_HTTP_BYTES
    max_candidates = MAX_CANDIDATES
    validate_candidate = staticmethod(validate_candidate)
    validate_preview = staticmethod(validate_preview)
    validate_receipt = staticmethod(validate_receipt)
