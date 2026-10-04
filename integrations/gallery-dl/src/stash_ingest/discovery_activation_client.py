"""Application review of exact held discovery records and their listing scope."""

from datetime import datetime, timezone
import uuid

from .activation_client import ActivationClient, integer, sha256
from .client import Unavailable
from .discovery_client import _time, validate_listing_input
from .encoding import InvalidData, decode, digest, encode, identifier, native_json

MAX_TARGETS = 1000
MAX_HTTP_BYTES = 4 << 20
INPUT_KEYS = {"uuid", "manifest_sha256", "listing", "targets"}
PREVIEW_KEYS = {"version", "input", "listing_sha256", "account_sha256", "entries", "plan_sha256"}
ENTRY_KEYS = {"source_ordinal", "source_sha256", "target_uuid", "post_uuid", "post_revision"}


def canonical_input(value):
    value = decode(encode(value, MAX_HTTP_BYTES), MAX_HTTP_BYTES)
    if not isinstance(value, dict) or set(value) != INPUT_KEYS:
        raise InvalidData("Invalid discovery activation input")
    identifier(value["uuid"])
    sha256(value["manifest_sha256"])
    listing = value["listing"]
    validate_listing_input(listing)
    if listing["legacy"] is None:
        raise InvalidData("Discovery activation requires its original snapshot and account record")
    # Go retains an exact UTC millisecond deadline, without trailing zeros.
    stamp = datetime.fromisoformat(listing["not_before"]).astimezone(timezone.utc)
    listing["not_before"] = stamp.isoformat(timespec="milliseconds")[:-6].rstrip("0").rstrip(".") + "Z"
    targets = value["targets"]
    if not isinstance(targets, list) or not 1 <= len(targets) <= MAX_TARGETS:
        raise InvalidData("Select between 1 and 1000 original discovery targets")
    seen = set()
    for target in targets:
        if not isinstance(target, dict) or set(target) != {"source_ordinal", "source_sha256"}:
            raise InvalidData("Invalid discovery target reference")
        ordinal = integer(target["source_ordinal"], 1)
        sha256(target["source_sha256"])
        if ordinal in seen:
            raise InvalidData("Duplicate original discovery target")
        seen.add(ordinal)
    targets.sort(key=lambda target: target["source_ordinal"])
    return value


def binding_input(value):
    value = decode(encode(value, MAX_HTTP_BYTES), MAX_HTTP_BYTES)
    if not isinstance(value, dict) or not isinstance(value.get("listing"), dict):
        raise InvalidData("Invalid discovery activation binding")
    value.setdefault("uuid", str(uuid.uuid4()))
    value["listing"].setdefault("uuid", str(uuid.uuid4()))
    return canonical_input(value)


def target_identity(listing, ordinal):
    return str(uuid.uuid5(uuid.UUID(identifier(listing)), "retained-discovery-target-v1\0" + str(integer(ordinal, 1))))


def validate_preview(value, expected=None):
    if (not isinstance(value, dict) or set(value) != PREVIEW_KEYS
            or integer(value["version"], 1, 1) != 1):
        raise InvalidData("Invalid discovery activation preview")
    source = canonical_input(value["input"])
    if (encode(source, MAX_HTTP_BYTES) != encode(value["input"], MAX_HTTP_BYTES)
            or (expected is not None and encode(source, MAX_HTTP_BYTES) != encode(expected, MAX_HTTP_BYTES))):
        raise InvalidData("Discovery preview changed its selected source, cursor or targets")
    for key in ("listing_sha256", "account_sha256", "plan_sha256"):
        sha256(value[key])
    if digest(native_json(source["listing"], 32768)) != value["listing_sha256"]:
        raise InvalidData("Discovery listing differs from its pinned digest")
    if not isinstance(value["entries"], list) or len(value["entries"]) != len(source["targets"]):
        raise InvalidData("Discovery preview omitted or added targets")
    for entry, target in zip(value["entries"], source["targets"]):
        if (not isinstance(entry, dict) or set(entry) != ENTRY_KEYS
                or entry["source_ordinal"] != target["source_ordinal"]
                or entry["source_sha256"] != target["source_sha256"]
                or entry["target_uuid"] != target_identity(source["listing"]["uuid"], target["source_ordinal"])):
            raise InvalidData("Discovery preview changed an original target reference")
        integer(entry["source_ordinal"], 1)
        identifier(entry["post_uuid"])
        integer(entry["post_revision"], 1)
    encode(value, MAX_HTTP_BYTES)
    return value


def validate_receipt(value, preview):
    validate_preview(preview)
    if (not isinstance(value, dict) or set(value) != PREVIEW_KEYS | {"created_at"}
            or encode({key: value[key] for key in PREVIEW_KEYS}, MAX_HTTP_BYTES) != encode(preview, MAX_HTTP_BYTES)):
        raise InvalidData("Discovery activation receipt differs from the saved review")
    _time(value["created_at"])
    return value


class DiscoveryActivationClient(ActivationClient):
    kind = "discovery"
    max_http_bytes = MAX_HTTP_BYTES
    validate_receipt = staticmethod(validate_receipt)

    def preview(self, value):
        expected = canonical_input(value)
        result = self.request("POST", "/discovery-activations/preview", expected)
        try:
            return validate_preview(result, expected)
        except (InvalidData, KeyError, TypeError, ValueError):
            raise Unavailable("invalid_discovery_preview") from None

    def status(self, preview):
        validate_preview(preview)
        return super().status(preview)

    def apply(self, preview):
        validate_preview(preview)
        return super().apply(preview)
