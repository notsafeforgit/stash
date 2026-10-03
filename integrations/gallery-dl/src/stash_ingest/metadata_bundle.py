"""Bounded, resumable metadata fetch transcripts with shared post fields.

These are producer checkpoints, not native captures or completion receipts.
The server must still verify post identity before accepting their evidence.
"""

from datetime import datetime
from urllib.parse import urlsplit

from .encoding import InvalidData, MAX_PAYLOAD_BYTES, decode, digest, encode, utc_now
from .retention import POLICY, retain

SCHEMA = "stash-metadata-fetch-v1"
MAX_BYTES = 32 << 20
MAX_RECORDS = 1024
MAX_REFERENCES = 256
ERRORS = frozenset({"rate_limited", "authentication", "access_denied", "challenge",
                    "not_found", "unsupported_extractor", "extraction_failed", "timeout",
                    "result_too_large", "invalid_checkpoint", "not_a_post_url",
                    "worker_failed", "runtime_changed"})


def public_url(value):
    if (not isinstance(value, str) or not 1 <= len(value.encode("utf-8")) <= 8192
            or any(ord(c) <= 32 or ord(c) == 127 for c in value)):
        raise InvalidData("Expected a bounded public source URL")
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in {"http", "https"} or not parsed.hostname
                or parsed.username is not None or parsed.password is not None):
            raise ValueError()
        parsed.port
    except ValueError:
        raise InvalidData("Expected a public HTTP source URL") from None
    return value


def _index(value, before, nullable=True):
    if value is None and nullable:
        return
    if type(value) is not int or not 0 <= value < before:
        raise InvalidData("Checkpoint references must point to preceding records")


class Bundle:
    def __init__(self, url, extractor_version, resume=None):
        self.value = {"schema": SCHEMA, "url": public_url(url), "retention_policy": POLICY,
                      "extractor_version": extractor_version, "records": [], "pending": [], "unresolved": []}
        self._metadata, self._seen = [], {}
        if resume is not None:
            saved = decode(encode(resume, MAX_BYTES), MAX_BYTES)
            if (not isinstance(saved, dict) or set(saved) != set(self.value)
                    or any(saved[k] != self.value[k] for k in
                           ("schema", "url", "retention_policy", "extractor_version"))):
                raise InvalidData("Metadata checkpoint belongs to another request or policy")
            if not isinstance(saved["records"], list) or len(saved["records"]) > MAX_RECORDS:
                raise InvalidData("Invalid metadata checkpoint record count")
            for record in saved["records"]:
                self._restore(record)
            for kind in ("pending", "unresolved"):
                if not isinstance(saved[kind], list) or len(saved[kind]) > MAX_REFERENCES:
                    raise InvalidData("Invalid metadata checkpoint reference count")
                for entry in saved[kind]:
                    self.reference(kind, entry)
        self._bytes = len(encode(self.value, MAX_BYTES))

    def _restore(self, record):
        index = len(self._metadata)
        if (not isinstance(record, dict) or set(record) != {"kind", "base", "parent", "patch", "removed", "observed_at"}
                or not isinstance(record["kind"], str) or record["kind"] not in {"post", "media", "context"}
                or not isinstance(record["patch"], dict) or not isinstance(record["removed"], list)
                or any(not isinstance(k, str) for k in record["removed"])
                or record["removed"] != sorted(set(record["removed"]))):
            raise InvalidData("Invalid metadata checkpoint record")
        try:
            observed = datetime.fromisoformat(record["observed_at"])
            if observed.tzinfo is None or "T" not in record["observed_at"]:
                raise ValueError()
        except (ValueError, TypeError):
            raise InvalidData("Expected a metadata observation time with timezone") from None
        _index(record["base"], index)
        _index(record["parent"], index)
        if record["parent"] is not None and self.depth(record["parent"]) >= 2:
            raise InvalidData("Metadata checkpoint child depth exceeded")
        data = {} if record["base"] is None else dict(self._metadata[record["base"]])
        for key in record["removed"]:
            if key not in data or key in record["patch"]:
                raise InvalidData("Invalid metadata checkpoint removal")
            del data[key]
        data.update(record["patch"])
        if encode(retain(data)) != encode(data):
            raise InvalidData("Metadata checkpoint violates the source retention policy")
        public_url(data.get("source_extractor_url"))
        self.value["records"].append(record)
        self._metadata.append(data)
        self._seen[self._key(record["kind"], record["parent"], data)] = index

    @staticmethod
    def _key(kind, parent, data):
        return kind, parent, digest(encode(data))

    def depth(self, index):
        depth = 0
        while index is not None:
            index = self.value["records"][index]["parent"]
            if index is not None:
                depth += 1
        return depth

    def append(self, kind, data, parent=None, base=None):
        data = retain(data)
        key = self._key(kind, parent, data)
        if key in self._seen:
            return self._seen[key]
        if len(self._metadata) >= MAX_RECORDS:
            raise InvalidData("Metadata result contains too many records")
        previous = {} if base is None else self._metadata[base]
        record = {"kind": kind, "parent": parent, "base": base, "observed_at": utc_now(),
                  "patch": {k: v for k, v in data.items() if k not in previous or previous[k] != v},
                  "removed": sorted(set(previous) - set(data))}
        size = len(encode(record)) + 1
        if self._bytes + size > MAX_BYTES:
            raise InvalidData("Metadata result exceeds its byte limit")
        self._restore(record)
        self._bytes += size
        return len(self._metadata) - 1

    def metadata(self, index, *, with_parent=False):
        _index(index, len(self._metadata), False)
        result = dict(self._metadata[index])
        parent = self.value["records"][index]["parent"]
        if with_parent and parent is not None:
            data = self.metadata(parent, with_parent=True)
            result["_reddit" if data.get("category") == "reddit" else "_parent"] = data
        encode(result, MAX_PAYLOAD_BYTES)
        return result

    def reference(self, kind, entry):
        if (kind not in {"pending", "unresolved"} or not isinstance(entry, dict)
                or set(entry) != {"url", "parent", "depth", "reason"}
                or not isinstance(entry["reason"], str) or entry["reason"] not in ERRORS | {"external_reference_only"}):
            raise InvalidData("Invalid metadata checkpoint reference")
        public_url(entry["url"])
        _index(entry["parent"], len(self._metadata), False)
        if type(entry["depth"]) is not int or entry["depth"] != self.depth(entry["parent"]) + 1:
            raise InvalidData("Invalid metadata checkpoint child depth")
        if kind == "pending" and not 1 <= entry["depth"] <= 2:
            raise InvalidData("Pending child exceeds the extraction depth")
        entries = self.value[kind]
        if entry in entries:
            return
        if len(entries) >= MAX_REFERENCES:
            raise InvalidData("Metadata result contains too many references")
        entries.append(dict(entry))

    def checkpoint(self):
        # References are small but still count toward the final envelope limit.
        encode(self.value, MAX_BYTES)
        return self.value
