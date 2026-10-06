"""Validate durable event envelopes before they reach disk.

The server remains responsible for source identity and domain validation. This
boundary excludes unsupported envelopes, unretained source data and credentials
from the local queue; it does not assert that a file has been imported.
"""

from datetime import datetime
import re
import unicodedata

from .encoding import (InvalidData, MAX_EVENT_BYTES, MAX_FILE_EVENT_BYTES,
                       decode, encode, identifier)
from .retention import POLICY, retain

COMMON = frozenset({"protocol", "producer_uuid", "event_uuid", "run_uuid",
                    "collection_uuid", "collection_revision", "root_uuid",
                    "kind", "observed_at"})
CAPTURE = frozenset({"extractor_version", "retention_policy", "post", "metadata", "source"})
FILE = frozenset({"relative_path", "size", "sha256", "media_kind"})
DOWNLOAD = frozenset({"owner_uuid", "fence", "transfer_sequence", "capture_event_uuid", "attachment", "state"})
METADATA = frozenset({"title", "original_text", "published_at", "date_basis", "language"})


def sha256(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def reference(value):
    if (not isinstance(value, dict) or set(value) != {"namespace", "value"}
            or any(not isinstance(part, str) or not part or len(part) > 1024
                   or any(ord(c) < 32 for c in part) for part in value.values())):
        raise InvalidData("Invalid qualified source identifier")


def validate(body):
    event = decode(body)
    if not isinstance(event, dict) or not COMMON <= event.keys():
        raise InvalidData("Missing event envelope fields")
    if type(event["protocol"]) is not int or event["protocol"] != 1:
        raise InvalidData("Unsupported ingestion protocol")
    for key in ("producer_uuid", "event_uuid", "run_uuid", "collection_uuid"):
        identifier(event[key])
    if event["root_uuid"] is not None:
        identifier(event["root_uuid"])
    if type(event["collection_revision"]) is not int or event["collection_revision"] < 1:
        raise InvalidData("Invalid collection revision")
    try:
        observed = datetime.fromisoformat(event["observed_at"])
        if observed.tzinfo is None or "T" not in event["observed_at"]:
            raise ValueError()
    except (ValueError, TypeError):
        raise InvalidData("Expected an observed timestamp with timezone") from None

    kind = event["kind"]
    if kind == "source.capture":
        if set(event) != COMMON | CAPTURE:
            raise InvalidData("Unsupported source envelope fields")
        version = event["extractor_version"]
        if (not isinstance(version, str) or not 1 <= len(version.encode("utf-8")) <= 128
                or any(c in version for c in "\r\n\x00")):
            raise InvalidData("Invalid extractor version")
        if event["retention_policy"] != POLICY:
            raise InvalidData("Unsupported source retention policy")
        reference(event["post"])
        metadata = event["metadata"]
        if (not isinstance(metadata, dict) or not set(metadata) <= METADATA
                or any(v is not None and not isinstance(v, str) for v in metadata.values())):
            raise InvalidData("Invalid source metadata fields")
        encode(metadata, 262144)
        if encode(retain(event["source"])) != encode(event["source"]):
            raise InvalidData("Source must satisfy the retention policy before queuing")
    elif kind == "file.completed":
        if (not COMMON | FILE <= event.keys()
                or not set(event) <= COMMON | FILE | {"source"}
                or len(body) > MAX_FILE_EVENT_BYTES or event["root_uuid"] is None):
            raise InvalidData("Invalid file event envelope")
        path = event["relative_path"]
        if (not isinstance(path, str) or not path or path.startswith("/")
                or "\\" in path or any(ord(c) < 32 for c in path)
                or any(p in {"", ".", ".."} for p in path.split("/"))
                or path.lower().endswith(".part")):
            raise InvalidData("Expected a final root-relative file path")
        if (type(event["size"]) is not int or not 0 < event["size"] < 2**63
                or not sha256(event["sha256"])
                or event["media_kind"] not in {"scene", "image"}):
            raise InvalidData("Invalid final file claim")
        source = event.get("source")
        if source is not None:
            if not isinstance(source, dict) or set(source) != {"capture_event_uuid", "attachment"}:
                raise InvalidData("Invalid file source reference")
            identifier(source["capture_event_uuid"])
            reference(source["attachment"])
    elif kind == "attachment.download":
        if (not COMMON | DOWNLOAD <= event.keys()
                or not set(event) <= COMMON | DOWNLOAD | {"file_event_uuid", "reason_code"}
                or len(body) > MAX_FILE_EVENT_BYTES or event["root_uuid"] is None):
            raise InvalidData("Invalid attachment download envelope")
        for key in ("owner_uuid", "capture_event_uuid"):
            identifier(event[key])
        for key in ("fence", "transfer_sequence"):
            if type(event[key]) is not int or not 1 <= event[key] < 2**53:
                raise InvalidData("Invalid attachment transfer attempt or sequence")
        reference(event["attachment"])
        if any(len(part.encode("utf-8")) > 1024
               or any(unicodedata.category(c) == "Cc" for c in part)
               for part in event["attachment"].values()):
            raise InvalidData("Invalid attachment reference")
        state, file_id, reason = event["state"], event.get("file_event_uuid", ""), event.get("reason_code", "")
        allowed = {"started": {""}, "downloaded": {""},
                   "failed": {"download_failed", "postprocess_failed", "source_failure"},
                   "excluded": {"unsupported_media", "filter"},
                   "skipped": {"archive_entry_without_file", "existing_without_file"}}
        if (not isinstance(state, str) or state not in allowed or not isinstance(reason, str)
                or reason not in allowed[state] or not isinstance(file_id, str)
                or (state == "downloaded") != bool(file_id)):
            raise InvalidData("Invalid attachment download state")
        if file_id:
            identifier(file_id)
    else:
        raise InvalidData("Unsupported event kind")
    if body != encode(event, MAX_EVENT_BYTES):
        raise InvalidData("Queue events must use the producer's stable JSON encoding")
    return event
