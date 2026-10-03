"""Bounded, resumable metadata fetch transcripts with shared post fields.

These are producer checkpoints, not native captures or completion receipts.
The server must still verify post identity before accepting their evidence.
"""

from datetime import datetime, timezone
import re
from urllib.parse import urlsplit

from .encoding import InvalidData, MAX_PAYLOAD_BYTES, decode, digest, encode, identifier, utc_now
from .retention import POLICY, retain

SCHEMA = "stash-metadata-fetch-v1"
RETAINED_SCHEMA = "stash-metadata-fetch-v2"
MAX_BYTES = 32 << 20
MAX_RECORDS = 1024
MAX_REFERENCES = 256
MAX_EXPANDED_BYTES = 128 << 20
OBSERVED_TIME = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$")
ERRORS = frozenset({"rate_limited", "authentication", "access_denied", "challenge",
                    "not_found", "unsupported_extractor", "extraction_failed", "timeout",
                    "result_too_large", "invalid_checkpoint", "not_a_post_url",
                    "worker_failed", "runtime_changed", "source_busy"})


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
    if not isinstance(value, int) or isinstance(value, bool) or not 0 <= value < before:
        raise InvalidData("Checkpoint references must point to preceding records")


def _native_size(value, limit):
    body = encode(value, limit)
    # Native JSON keeps Unicode but escapes these two JavaScript separators.
    # Measure that representation too before accepting a compact checkpoint.
    size = len(body) + 3 * (body.count(b"\xe2\x80\xa8") + body.count(b"\xe2\x80\xa9"))
    if size > limit:
        raise InvalidData("Metadata checkpoint exceeds the native byte limit")
    return size


def retained_ancestors(data, depth=0):
    """Resolve saved parent config without dropping either provenance branch.

    A direct ancestor shortcut may repeat the end of a longer chain. Competing
    category chains cannot safely select gallery-dl's inherited settings.
    """
    if not isinstance(data, dict) or depth > 2:
        raise InvalidData("Invalid retained metadata ancestry")
    category = data.get("category")
    if not isinstance(category, str) or not 1 <= len(category) <= 128:
        raise InvalidData("Retained metadata is missing its extractor category")
    paths = [retained_ancestors(data[key], depth + 1) for key in ("_parent", "_reddit")
             if data.get(key) is not None]
    longest = max(paths, key=len, default=[])
    if any(path != longest[-len(path):] for path in paths):
        raise InvalidData("Retained metadata has competing parent categories")
    return [category, *longest]


class Bundle:
    def __init__(self, url, extractor_version, resume=None):
        self.value = {"schema": SCHEMA, "url": public_url(url), "retention_policy": POLICY,
                      "extractor_version": extractor_version, "records": [], "pending": [], "unresolved": []}
        self._metadata, self._seen = [], {}
        self._expanded_bytes = 0
        if resume is not None:
            saved = decode(encode(resume, MAX_BYTES), MAX_BYTES, preserve_numbers=True)
            if isinstance(saved, dict) and saved.get("schema") in (SCHEMA, RETAINED_SCHEMA):
                self.value["schema"] = saved["schema"]
            if (not isinstance(saved, dict) or set(saved) != set(self.value)
                    or any(saved[k] != self.value[k] for k in
                           ("schema", "url", "retention_policy", "extractor_version"))):
                raise InvalidData("Metadata checkpoint belongs to another request or policy")
            if not isinstance(saved["records"], list) or len(saved["records"]) > MAX_RECORDS:
                raise InvalidData("Invalid metadata checkpoint record count")
            for record in saved["records"]:
                self._restore(record)
            if self.value["schema"] == RETAINED_SCHEMA and (
                    not self.value["records"] or "retained_capture" not in self.value["records"][0]):
                raise InvalidData("Retained metadata needs its accepted capture prefix")
            for kind in ("pending", "unresolved"):
                if not isinstance(saved[kind], list) or len(saved[kind]) > MAX_REFERENCES:
                    raise InvalidData("Invalid metadata checkpoint reference count")
                for entry in saved[kind]:
                    count = len(self.value[kind])
                    self.reference(kind, entry)
                    if count == len(self.value[kind]):
                        raise InvalidData("Duplicate metadata checkpoint reference")
        self._bytes = _native_size(self.value, MAX_BYTES)

    def _restore(self, record):
        index = len(self._metadata)
        retained = isinstance(record, dict) and "retained_capture" in record
        keys = {"kind", "base", "parent", "patch", "removed", "observed_at"}
        if retained and self.value["schema"] == RETAINED_SCHEMA:
            keys.add("retained_capture")
        if (not isinstance(record, dict) or set(record) != keys
                or not isinstance(record["kind"], str) or record["kind"] not in {"post", "media", "context"}
                or not isinstance(record["patch"], dict) or not isinstance(record["removed"], list)
                or any(not isinstance(k, str) for k in record["removed"])
                or record["removed"] != sorted(set(record["removed"]))):
            raise InvalidData("Invalid metadata checkpoint record")
        if retained:
            identifier(record["retained_capture"])
            if (record["kind"] != "context" or record["observed_at"] is not None
                    or record["base"] is not None or record["parent"] is not None
                    or (index and "retained_capture" not in self.value["records"][-1])):
                raise InvalidData("Retained captures are context, not new observations")
        else:
            self._observation_time(record["observed_at"])
        _index(record["base"], index)
        _index(record["parent"], index)
        if self.value["schema"] == RETAINED_SCHEMA and not retained and record["parent"] is None:
            raise InvalidData("Reviewed legacy work may only fetch its saved children")
        if record["parent"] is not None and self.depth(record["parent"]) >= 2:
            raise InvalidData("Metadata checkpoint child depth exceeded")
        if record["base"] is not None and "retained_capture" in self.value["records"][record["base"]]:
            raise InvalidData("New observations cannot copy retained fields as a delta")
        data = {} if record["base"] is None else dict(self._metadata[record["base"]])
        for key in record["removed"]:
            if key not in data or key in record["patch"]:
                raise InvalidData("Invalid metadata checkpoint removal")
            del data[key]
        data.update(record["patch"])
        if retained:
            retained_ancestors(data)
            key = "retained", record["retained_capture"]
        else:
            if self.value["schema"] == RETAINED_SCHEMA and {"_parent", "_reddit"} & data.keys():
                raise InvalidData("New child context must come from its bound parent record")
            if encode(retain(data)) != encode(data):
                raise InvalidData("Metadata checkpoint violates the source retention policy")
            public_url(data.get("source_extractor_url"))
            key = self._key(record["kind"], record["parent"], data)
        if key in self._seen:
            raise InvalidData("Duplicate metadata checkpoint record")
        expanded = dict(data)
        if record["parent"] is not None:
            parent = self.metadata(record["parent"], with_parent=True)
            expanded["_reddit" if parent.get("category") == "reddit" else "_parent"] = parent
        size = _native_size(expanded, MAX_PAYLOAD_BYTES)
        if self._expanded_bytes + size > MAX_EXPANDED_BYTES:
            raise InvalidData("Metadata checkpoint expansion exceeds its byte limit")
        self.value["records"].append(record)
        self._metadata.append(data)
        self._seen[key] = index
        self._expanded_bytes += size

    @staticmethod
    def _observation_time(value):
        try:
            match = OBSERVED_TIME.fullmatch(value) if isinstance(value, str) else None
            if match is None:
                raise ValueError()
            observed = datetime.fromisoformat(value).astimezone(timezone.utc)
            # Python truncates nanoseconds, while native capture timestamps retain
            # them. Reject the zero instant without changing valid sub-microseconds.
            if observed == datetime.min.replace(tzinfo=timezone.utc) and not int((match[1] or ".0")[1:]):
                raise ValueError()
        except (ValueError, TypeError, OverflowError):
            raise InvalidData("Expected a metadata observation time with timezone") from None

    @staticmethod
    def _key(kind, parent, data):
        return kind, parent, digest(encode(data))

    def depth(self, index):
        depth = 0
        while index is not None:
            if "retained_capture" in self.value["records"][index]:
                return depth + len(retained_ancestors(self._metadata[index])) - 1
            index = self.value["records"][index]["parent"]
            if index is not None:
                depth += 1
        return depth

    def ancestors(self, index):
        categories = []
        while index is not None:
            record = self.value["records"][index]
            if "retained_capture" in record:
                return categories + retained_ancestors(self._metadata[index])
            categories.append(self._metadata[index]["category"])
            index = record["parent"]
        return categories

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
        size = _native_size(record, MAX_BYTES) + 1
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
                or not isinstance(entry["reason"], str)
                or entry["reason"] not in ERRORS | {"external_reference_only", "legacy_pending"}):
            raise InvalidData("Invalid metadata checkpoint reference")
        public_url(entry["url"])
        _index(entry["parent"], len(self._metadata), False)
        if entry["reason"] == "legacy_pending" and (
                self.value["schema"] != RETAINED_SCHEMA or kind != "pending"
                or "retained_capture" not in self.value["records"][entry["parent"]]):
            raise InvalidData("Legacy pending state needs a retained parent")
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
        _native_size(self.value, MAX_BYTES)
        return self.value
