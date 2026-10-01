"""Versioned, read-only access to the portable catalog migration inputs.

Physical records are read once per table. Captures are reconstructed for semantic
verification without changing the source or storing expanded profile/post copies.
"""

from collections import OrderedDict
import copy
from datetime import datetime
import hashlib
from pathlib import Path
import re
import sqlite3

from .encoding import InvalidData, MAX_PAYLOAD_BYTES, decode, digest, encode


READER_VERSION = "catalog-sqlite-v1"
APPLICATION_ID = 0x53435043
MAX_ROW_BYTES = 16 << 20
SHA256 = re.compile(r"[0-9a-f]{64}")
TABLES = {
    "catalog_info": ("key", "value"),
    "accounts": ("account_key", "platform", "source_id", "identity_basis", "created_at"),
    "handles": ("account_key", "handle", "first_observed"),
    "posts": ("post_key", "platform", "source_id", "account_key", "identity_basis", "created_at"),
    "post_urls": ("post_key", "url"),
    "post_aliases": ("alias_key", "post_key"),
    "observations": ("observation_id", "post_key", "origin", "captured_at", "payload_json", "title",
                     "original_text", "published_at", "date_basis", "language", "extractor_version"),
    "observation_details": ("capture_id", "observation_id", "captured_at", "extractor_version", "payload_patch"),
    "account_snapshots": ("snapshot_id", "platform", "payload_json"),
    "assets": ("asset_id", "digest_algorithm", "digest", "byte_size", "created_at"),
    "files": ("relpath", "asset_id", "state", "byte_size", "mtime_ns", "first_observed", "survivor_relpath", "role"),
    "appearances": ("post_key", "attachment_key", "asset_id", "source_media_id", "position", "source_relpath"),
    "memberships": ("post_key", "collection_key", "kind", "label"),
    "translations": ("translation_id", "post_key", "input_hash", "original_text", "translated_text",
                     "source_language", "target_language", "provider", "provenance", "captured_at"),
    "sidecars": ("relpath", "content_sha256", "raw_content", "encoding", "parse_status", "warnings_json",
                 "parsed_json", "post_key", "captured_at"),
    "sidecar_documents": ("document_id", "content_sha256", "raw_content", "encoding", "parse_status", "warnings_json", "parsed_json"),
    "sidecar_sources": ("relpath", "content_sha256", "document_id", "post_key", "captured_at"),
    "sidecar_heads": ("relpath", "content_sha256", "observed_at"),
    "metadata_edits": ("edit_id", "relpath", "fields_json", "created_at"),
    "dedupe_events": ("event_id", "asset_id", "paths_json", "survivor_relpath", "stage", "created_at"),
    "file_events": ("event_id", "relpath", "old_state", "new_state", "reason", "observed_at"),
    "metadata_prune_queue": ("post_key", "pruned_at"),
    "enrichment_receipts": ("post_key", "version", "completed_at", "details_json"),
}
KEYS = {
    "catalog_info": ("key",), "accounts": ("account_key",), "handles": ("account_key", "handle"),
    "posts": ("post_key",), "post_urls": ("post_key", "url"), "post_aliases": ("alias_key",),
    "observations": ("observation_id",), "observation_details": ("capture_id",),
    "account_snapshots": ("snapshot_id",), "assets": ("asset_id",), "files": ("relpath",),
    "appearances": ("post_key", "attachment_key", "asset_id"), "memberships": ("post_key", "collection_key"),
    "translations": ("translation_id",), "sidecars": ("relpath", "content_sha256"),
    "sidecar_documents": ("document_id",), "sidecar_sources": ("relpath", "content_sha256"),
    "sidecar_heads": ("relpath",), "metadata_edits": ("edit_id",), "dedupe_events": ("event_id",),
    "file_events": ("event_id",), "metadata_prune_queue": ("post_key",), "enrichment_receipts": ("post_key", "version"),
}
REQUIRED = {"catalog_info", "accounts", "handles", "posts", "observations", "post_urls", "assets",
            "files", "appearances", "memberships", "translations", "dedupe_events"}
METADATA = ("title", "original_text", "published_at", "date_basis", "language")
REFERENCES = {
    "handles": (("account_key", "accounts", "account_key"),),
    "posts": (("account_key", "accounts", "account_key"),),
    "observations": (("post_key", "posts", "post_key"),),
    "observation_details": (("observation_id", "observations", "observation_id"),),
    "post_urls": (("post_key", "posts", "post_key"),),
    "post_aliases": (("post_key", "posts", "post_key"),),
    "files": (("asset_id", "assets", "asset_id"),),
    "appearances": (("post_key", "posts", "post_key"), ("asset_id", "assets", "asset_id")),
    "memberships": (("post_key", "posts", "post_key"),),
    "translations": (("post_key", "posts", "post_key"),),
    "sidecars": (("post_key", "posts", "post_key"),),
    "sidecar_sources": (("post_key", "posts", "post_key"), ("document_id", "sidecar_documents", "document_id")),
    "metadata_edits": (("relpath", "files", "relpath"),),
    "file_events": (("relpath", "files", "relpath"),),
    "dedupe_events": (("asset_id", "assets", "asset_id"),),
    "enrichment_receipts": (("post_key", "posts", "post_key"),),
}
SIDECAR_VIEW = """CREATE VIEW sidecars AS SELECT s.relpath,s.content_sha256,d.raw_content,
 d.encoding,d.parse_status,d.warnings_json,d.parsed_json,s.post_key,s.captured_at
 FROM sidecar_sources s JOIN sidecar_documents d USING(document_id)"""


def quoted(name):
    return '"' + name.replace('"', '""') + '"'


def json_value(value):
    if not isinstance(value, str):
        raise InvalidData("Catalog JSON column is not text")
    return decode(value.encode("utf-8"), MAX_PAYLOAD_BYTES)


def source_time(value):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:[0-9]{2})", value):
        raise InvalidData("Catalog capture time must have an explicit UTC offset")
    try:
        datetime.fromisoformat(value)
    except ValueError:
        raise InvalidData("Invalid catalog capture time") from None
    return value


class CatalogSource:
    """One consistent SQLite read transaction; no schema migration or writer."""

    def __init__(self, path):
        self.path = Path(path).resolve(strict=True)
        self.db = None
        self.tables = {}
        self.profiles = OrderedDict()
        self.profile_bytes = 0

    def __enter__(self):
        try:
            self.db = sqlite3.connect(self.path.as_uri() + "?mode=ro", uri=True, timeout=5)
            self.db.row_factory = sqlite3.Row
            self.db.execute("PRAGMA query_only=ON")
            self.db.execute("PRAGMA trusted_schema=OFF")
            self.db.execute("BEGIN")
            self._schema()
            return self
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def __exit__(self, *_):
        if self.db is not None:
            self.db.close()
            self.db = None

    def _schema(self):
        self.version = self.db.execute("PRAGMA user_version").fetchone()[0]
        self.application_id = self.db.execute("PRAGMA application_id").fetchone()[0]
        if self.application_id != APPLICATION_ID or self.version not in (1, 2, 3):
            raise InvalidData("Unsupported catalog database lineage/version")
        self.schema = [dict(row) for row in self.db.execute("SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY type,name")]
        for item in self.schema:
            name = item["name"]
            if item["type"] in ("index", "trigger"):
                if item["tbl_name"] not in TABLES:
                    raise InvalidData("Unknown catalog schema object")
                continue
            if item["type"] == "view":
                normalize = lambda sql: re.sub(r"\s+", "", sql).lower().rstrip(";")
                if name != "sidecars" or normalize(item["sql"]) != normalize(SIDECAR_VIEW):
                    raise InvalidData("Unknown catalog view")
                continue
            if item["type"] != "table" or name not in TABLES:
                raise InvalidData("Unknown catalog table")
            columns = [dict(row) for row in self.db.execute("PRAGMA table_xinfo(" + quoted(name) + ")")]
            actual = {row["name"] for row in columns}
            expected = set(TABLES[name])
            if name in ("observations", "observation_details") and "account_refs_json" in actual:
                expected.add("account_refs_json")
            if actual != expected or any(row["hidden"] for row in columns):
                raise InvalidData("Unsupported columns in catalog table " + name)
            pk = tuple(row["name"] for row in sorted(columns, key=lambda row: row["pk"]) if row["pk"])
            if pk != KEYS[name]:
                raise InvalidData("Unsupported catalog primary key in " + name)
            self.tables[name] = columns
        names = self.tables.keys()
        if not REQUIRED <= names:
            raise InvalidData("Catalog is missing required record families")
        normalized = {"sidecar_documents", "sidecar_sources"}
        if bool(names & normalized) and not normalized <= names:
            raise InvalidData("Catalog sidecar families are incomplete")
        if ("sidecars" in names) == (normalized <= names):
            raise InvalidData("Catalog must have one physical sidecar representation")
        if self.version == 3 and any("account_refs_json" not in {c["name"] for c in self.tables[table]}
                                     for table in ("observations", "observation_details") if table in names):
            raise InvalidData("Version 3 catalog is missing profile reference columns")
        self.info = dict(self.db.execute("SELECT key,value FROM catalog_info"))
        if (not isinstance(self.info.get("id"), str) or not re.fullmatch(r"c_[0-9a-f]{32}", self.info["id"])
                or self.info.get("schema_version") != str(self.version)
                or self.info.get("path_base") != "media-root-relative"):
            raise InvalidData("Catalog identity/version/path convention is unsupported")

    def check_integrity(self):
        if [row[0] for row in self.db.execute("PRAGMA quick_check")] != ["ok"]:
            raise InvalidData("Catalog SQLite integrity check failed")
        if self.db.execute("PRAGMA foreign_key_check").fetchone() is not None:
            raise InvalidData("Catalog contains broken foreign-key references")
        # Check logical references too: older SQLite files may omit a constraint.
        # A JOIN must never silently hide a missing post, capture or source row.
        for table, refs in REFERENCES.items():
            if table not in self.tables:
                continue
            for column, parent, key in refs:
                if parent not in self.tables or self.db.execute(
                        "SELECT 1 FROM " + quoted(table) + " c WHERE c." + quoted(column)
                        + " IS NOT NULL AND NOT EXISTS(SELECT 1 FROM " + quoted(parent)
                        + " p WHERE p." + quoted(key) + "=c." + quoted(column) + ") LIMIT 1").fetchone():
                    raise InvalidData("Catalog contains a broken logical reference in " + table)
        if "sidecar_heads" in self.tables:
            parent = "sidecar_sources" if "sidecar_sources" in self.tables else "sidecars"
            if self.db.execute("SELECT 1 FROM sidecar_heads h WHERE NOT EXISTS(SELECT 1 FROM " + parent
                               + " s WHERE s.relpath=h.relpath AND s.content_sha256=h.content_sha256) LIMIT 1").fetchone():
                raise InvalidData("Catalog sidecar head references a missing source")
        if "sidecar_sources" in self.tables and self.db.execute(
                "SELECT 1 FROM sidecar_sources s JOIN sidecar_documents d USING(document_id) WHERE s.content_sha256!=d.content_sha256 LIMIT 1").fetchone():
            raise InvalidData("Catalog sidecar source and document hashes disagree")

    def rows(self, table):
        if table not in self.tables:
            raise InvalidData("Unknown retained catalog table")
        order = ",".join(quoted(key) + " COLLATE BINARY" for key in KEYS[table])
        for row in self.db.execute("SELECT * FROM " + quoted(table) + " ORDER BY " + order):
            result = dict(row)
            if any(result[key] is None for key in KEYS[table]):
                raise InvalidData("Catalog row has a null primary key")
            yield result

    def validate_row(self, table, row):
        for key, value in row.items():
            if isinstance(value, bytes) and not (table in ("sidecars", "sidecar_documents") and key == "raw_content"):
                raise InvalidData("Unexpected binary catalog column in " + table)
            if key.endswith("_json") or key == "payload_patch":
                json_value(value)
        if table in ("sidecars", "sidecar_documents"):
            if not isinstance(row["raw_content"], bytes) or digest(row["raw_content"]) != row["content_sha256"]:
                raise InvalidData("Catalog sidecar content checksum mismatch")
        if table == "account_snapshots":
            self._profile_value(row)

    @staticmethod
    def _profile_value(row):
        value = json_value(row["payload_json"])
        if (not isinstance(value, dict) or not value or row["platform"] not in ("reddit", "twitter")
                or digest(encode(["source-account-snapshot-v1", row["platform"], value], MAX_PAYLOAD_BYTES)) != row["snapshot_id"]):
            raise InvalidData("Catalog account snapshot checksum/shape mismatch")
        return value

    def _profile(self, key):
        if key not in self.profiles:
            if "account_snapshots" not in self.tables:
                raise InvalidData("Catalog account snapshot table is missing")
            row = self.db.execute("SELECT * FROM account_snapshots WHERE snapshot_id=?", (key,)).fetchone()
            if row is None:
                raise InvalidData("Catalog account snapshot reference is missing")
            value = self._profile_value(row)
            size = len(row["payload_json"].encode("utf-8"))
            self.profiles[key] = (value, size)
            self.profile_bytes += size
            while len(self.profiles) > 256 or self.profile_bytes > 8 << 20:
                _, (_, removed) = self.profiles.popitem(last=False)
                self.profile_bytes -= removed
        self.profiles.move_to_end(key)
        return copy.deepcopy(self.profiles[key][0])

    def reference_inventory(self):
        result = {}
        for table, refs in REFERENCES.items():
            if table in self.tables:
                for column, _, _ in refs:
                    result[table + "." + column] = self.db.execute(
                        "SELECT count(*) FROM " + quoted(table) + " WHERE " + quoted(column) + " IS NOT NULL").fetchone()[0]
        return result

    def hydrate(self, row, column):
        payload = json_value(row[column])
        if not isinstance(payload, dict):
            raise InvalidData("Catalog capture payload is not an object")
        refs = json_value(row.get("account_refs_json", "[]"))
        if not isinstance(refs, list) or len(refs) > 1024:
            raise InvalidData("Invalid catalog profile reference list")
        seen = set()
        for ref in refs:
            if not isinstance(ref, list) or len(ref) != 2:
                raise InvalidData("Invalid catalog profile reference")
            path, key = ref
            if (not isinstance(path, list) or not path or not isinstance(path[-1], str)
                    or any(type(part) not in (str, int) for part in path)
                    or not isinstance(key, str) or not SHA256.fullmatch(key) or tuple(path) in seen):
                raise InvalidData("Invalid catalog profile path/identity")
            seen.add(tuple(path))
            parent = payload
            try:
                for part in path[:-1]:
                    if (type(part) is int and (not isinstance(parent, list) or part < 0)
                            or isinstance(part, str) and not isinstance(parent, dict)):
                        raise InvalidData("Invalid catalog profile path")
                    parent = parent[part]
                if not isinstance(parent, dict) or path[-1] not in parent or parent[path[-1]] is not None:
                    raise InvalidData("Catalog profile reference would overwrite data")
                parent[path[-1]] = self._profile(key)
            except (KeyError, IndexError, TypeError):
                raise InvalidData("Invalid catalog profile path") from None
        return payload, len(refs)

    def captures(self):
        query = "SELECT o.*,p.platform FROM observations o JOIN posts p USING(post_key) ORDER BY o.post_key,o.observation_id"
        for stored in self.db.execute(query):
            observation = dict(stored)
            shared, shared_refs = self.hydrate(observation, "payload_json")
            details = (self.db.execute("SELECT * FROM observation_details WHERE observation_id=? ORDER BY captured_at,capture_id", (observation["observation_id"],))
                       if "observation_details" in self.tables else ())
            found = False
            for stored_detail in details:
                found = True
                detail = dict(stored_detail)
                patch, patch_refs = self.hydrate(detail, "payload_patch")
                payload = copy.deepcopy(shared)
                for key, value in patch.items():
                    if key == "_reddit" and isinstance(payload.get(key), dict) and isinstance(value, dict):
                        payload[key].update(value)
                    else:
                        payload[key] = value
                yield self._capture(observation, detail, payload, shared_refs + patch_refs)
            if not found:
                yield self._capture(observation, None, shared, shared_refs)

    @staticmethod
    def _capture(observation, detail, payload, references):
        row = observation if detail is None else detail
        metadata = {key: observation[key] for key in METADATA}
        if any(value is not None and not isinstance(value, str) for value in metadata.values()):
            raise InvalidData("Catalog projected metadata must be text or null")
        encode(metadata, 262144)
        body = encode(payload, MAX_PAYLOAD_BYTES)
        return {"post_key": observation["post_key"], "observation_id": observation["observation_id"],
                "capture_id": row["observation_id"] if detail is None else row["capture_id"],
                "origin": observation["origin"], "platform": observation["platform"],
                "captured_at": source_time(row["captured_at"]), "extractor_version": row["extractor_version"],
                "metadata": metadata, "payload": payload, "payload_sha256": digest(body),
                "payload_bytes": len(body), "profile_references": references, "flat_observation": detail is None}

    def capture_inventory(self):
        hashed = hashlib.sha256()
        count, flat, refs, largest = 0, 0, 0, 0
        for capture in self.captures():
            count += 1
            flat += capture["flat_observation"]
            refs += capture["profile_references"]
            largest = max(largest, capture["payload_bytes"])
            row = {key: value for key, value in capture.items() if key != "payload"}
            hashed.update(encode(row)); hashed.update(b"\n")
        return {"count": count, "flat_observations": flat, "profile_references": refs,
                "max_payload_bytes": largest, "sha256": hashed.hexdigest(),
                "encoding": "legacy-python-json-v1"}
