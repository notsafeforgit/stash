"""Successful native publications and durable snapshot-retirement decisions.

The host's existing backup/dedupe lock excludes publication and cleanup. Remote
immutable receipts are authoritative; private local caches only avoid repeatedly
downloading unchanged history and large media manifests. No cold objects are
read, copied, thawed or deleted here. Standard object reclamation is separate.
"""

import base64
import hashlib
import os
from pathlib import Path
import re
import tempfile

from manifest_limits import MASTER_BYTES
from media_objects import relative_path, validate_store
from native_store import PREFIX, descriptor, selection_digest, validate_reference
from object_receipts import identity, inventory
from stash_archive.artwork_pins import directory
from stash_archive.filesystem_boundary import canonical_uuid
from stash_archive.storage import HEX, InvalidArchive, decode_json, json_bytes, open_regular, sync_directory

FORMAT = "org.notsafeforgit.stash.backup-history"
HISTORY = PREFIX + "history/"
RUN_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,127}\Z")


def policy(value=None):
    value = {"keep_last": 7, "pins": []} if value is None else value
    if (not isinstance(value, dict) or set(value) != {"keep_last", "pins"}
            or type(value["keep_last"]) is not int or not 1 <= value["keep_last"] <= 365
            or not isinstance(value["pins"], list) or len(value["pins"]) > 1000
            or any(not canonical_uuid(item) for item in value["pins"])
            or len(set(value["pins"])) != len(value["pins"])):
        raise InvalidArchive("Invalid snapshot retention: require keep_last (1..365) and unique archive UUID pins")
    return {"keep_last": value["keep_last"], "pins": list(value["pins"])}


def publication(body):
    if not isinstance(body, bytes) or not 0 < len(body) <= MASTER_BYTES:
        raise InvalidArchive("Invalid bounded native master manifest")
    catalog = decode_json(body)
    if (not isinstance(catalog, dict) or catalog.get("format") != "s3-log-backup"
            or type(catalog.get("version")) is not int or catalog["version"] not in (3, 4)
            or not isinstance(catalog.get("run_id"), str) or not RUN_NAME.fullmatch(catalog["run_id"])
            or type(catalog.get("created_epoch")) is not int or catalog["created_epoch"] <= 0):
        raise InvalidArchive("Invalid native master publication identity")
    reference = validate_reference(catalog.get("native_archive"))
    if selection_digest(catalog) != reference["selection_sha256"]:
        raise InvalidArchive("Native master differs from its retained media selection")
    record = {"format": FORMAT, "version": 1, "archive_uuid": reference["archive_uuid"],
              "run_id": catalog["run_id"], "created_epoch": catalog["created_epoch"],
              "master": descriptor("manifests/runs/" + catalog["run_id"] + "/manifest.json", body),
              "native_archive": reference}
    return record


def publication_key(archive_uuid):
    if not canonical_uuid(archive_uuid):
        raise InvalidArchive("Invalid backup history archive UUID")
    return HISTORY + "publications/" + archive_uuid + ".json"


def record_publication(store, body):
    """Call only after verifying the successful current-manifest commit.

    A failure must leave the host attempt uncommitted locally, so restart first
    completes this receipt for the original bytes before releasing its capture.
    """
    record = publication(body)
    return store.put_bytes(publication_key(record["archive_uuid"]), json_bytes(record))


def validate_publication(record, archive_uuid):
    if (not isinstance(record, dict) or set(record) != {"format", "version", "archive_uuid", "run_id",
                                                       "created_epoch", "master", "native_archive"}
            or record["format"] != FORMAT or type(record["version"]) is not int or record["version"] != 1
            or record["archive_uuid"] != archive_uuid or not canonical_uuid(archive_uuid)
            or not isinstance(record["run_id"], str) or not RUN_NAME.fullmatch(record["run_id"])
            or type(record["created_epoch"]) is not int or record["created_epoch"] <= 0
            or validate_reference(record["native_archive"])["archive_uuid"] != archive_uuid):
        raise InvalidArchive("Invalid backup history publication")
    master = record["master"]
    if (not isinstance(master, dict) or set(master) != {"key", "sha256", "bytes"}
            or master["key"] != "manifests/runs/" + record["run_id"] + "/manifest.json"
            or not isinstance(master["sha256"], str) or not HEX.fullmatch(master["sha256"])
            or type(master["bytes"]) is not int or not 0 < master["bytes"] <= MASTER_BYTES):
        raise InvalidArchive("Invalid backup history master reference")
    return record


class SnapshotHistory:
    def __init__(self, store, cache):
        self.store, self.cache = store, Path(cache)
        self.cache.mkdir(mode=0o700, parents=True, exist_ok=True)
        directory(self.cache, private=True)
        # Separate destinations cannot share either receipt or graph caches.
        scope = hashlib.sha256(json_bytes({"bucket": store.bucket, "prefix": store.prefix})).hexdigest()
        self.cache = self.cache / scope
        self.cache.mkdir(mode=0o700, exist_ok=True)
        directory(self.cache, private=True)

    def _read_cache(self, name, limit):
        try:
            with open_regular(self.cache / name) as incoming:
                body = incoming.read(limit + 1)
        except FileNotFoundError:
            return None
        if len(body) > limit:
            raise InvalidArchive("Backup history cache exceeds its size limit")
        cached = decode_json(body)
        if (not isinstance(cached, dict) or set(cached) != {"sha256", "value"}
                or cached["sha256"] != hashlib.sha256(json_bytes(cached["value"])).hexdigest()):
            raise InvalidArchive("Backup history cache is corrupt; rebuild it before cleanup")
        return cached["value"]

    def _save_cache(self, name, value):
        body = json_bytes({"sha256": hashlib.sha256(json_bytes(value)).hexdigest(), "value": value})
        fd, temporary = tempfile.mkstemp(prefix=".history-", dir=self.cache)
        try:
            with os.fdopen(fd, "wb") as output:
                output.write(body)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, self.cache / name)
            sync_directory(self.cache)
        finally:
            Path(temporary).unlink(missing_ok=True)

    def _record(self, key, listed, name):
        current = identity(listed, listed=True)
        if current is None or not 0 < current[0] <= 16384:
            raise InvalidArchive("Invalid backup history object listing")
        cached = self._read_cache(name, 32768)
        if cached is not None:
            if not isinstance(cached, dict) or set(cached) != {"identity", "record"}:
                raise InvalidArchive("Invalid backup history receipt cache")
            if cached["identity"] == list(current):
                return cached["record"]
        head = self.store.head(key)
        try:
            sha256 = base64.b64decode(head["ChecksumSHA256"], validate=True).hex()
        except (TypeError, KeyError, ValueError):
            raise InvalidArchive("Backup history lacks a full SHA-256 checksum") from None
        if not HEX.fullmatch(sha256) or identity(head) != current:
            raise InvalidArchive("Backup history changed during inspection")
        body = self.store.read({"key": key, "sha256": sha256, "bytes": current[0]})
        record = decode_json(body)
        if json_bytes(record) != body or cached is not None and cached["record"] != record:
            raise InvalidArchive("Immutable backup history bytes changed")
        self._save_cache(name, {"identity": list(current), "record": record})
        return record

    def inspect(self):
        listed = inventory(self.store.client, self.store.bucket, self.store.prefix + HISTORY)
        publications, retired = {}, {}
        for full_key, item in sorted(listed.items()):
            key = full_key[len(self.store.prefix):]
            suffix = key[len(HISTORY):]
            parts = suffix.split("/")
            if (len(parts) != 2 or parts[0] not in ("publications", "retirements")
                    or not parts[1].endswith(".json") or not canonical_uuid(parts[1][:-5])):
                raise InvalidArchive("Unknown backup history object; cleanup requires review")
            kind, archive_uuid = parts[0], parts[1][:-5]
            record = self._record(key, item, kind + "-" + archive_uuid + ".json")
            if kind == "publications":
                publications[archive_uuid] = validate_publication(record, archive_uuid)
            else:
                retired[archive_uuid] = record
        for archive_uuid, record in retired.items():
            expected = publications.get(archive_uuid)
            if (not isinstance(record, dict) or set(record) != {"format", "version", "archive_uuid", "publication", "retired_by"}
                    or record["format"] != FORMAT + ".retirement" or type(record["version"]) is not int
                    or record["version"] != 1 or record["archive_uuid"] != archive_uuid
                    or expected is None or record["publication"] != descriptor(publication_key(archive_uuid), json_bytes(expected))
                    or not canonical_uuid(record["retired_by"]) or record["retired_by"] == archive_uuid
                    or record["retired_by"] not in publications):
                raise InvalidArchive("Invalid or orphaned backup retirement receipt")
        return publications, retired

    def _media_graph(self, record):
        name = "media-" + record["archive_uuid"] + ".json"
        graph = self._read_cache(name, MASTER_BYTES)
        if graph is None:
            body = self.store.read(record["master"])
            if publication(body) != record:
                raise InvalidArchive("Backup history differs from its verified master")
            catalog = decode_json(body)
            if catalog["version"] != 4:
                raise InvalidArchive("Historical media needs an explicit immutable-store migration before cleanup")
            # publication() checked the complete v4 inventory and selection.
            graph = {"master": record["master"], "media_store": catalog["media_store"],
                     "keys": sorted(catalog["objects"])}
            self._save_cache(name, graph)
        if (not isinstance(graph, dict) or set(graph) != {"master", "media_store", "keys"}
                or graph["master"] != record["master"] or not isinstance(graph["keys"], list)
                or any(not isinstance(key, str) for key in graph["keys"])
                or sorted(set(graph["keys"])) != graph["keys"]):
            raise InvalidArchive("Invalid retained media graph")
        validate_store(graph["media_store"])
        for key in graph["keys"]:
            relative_path(key)
        return graph

    def retain(self, current_body, media_store, retention=None, *, available_media=None):
        """Retire old snapshot promises, then return every still-protected key.

        Caller holds the host backup lock. No cleanup is allowed if any required
        receipt, pin, selection or storage binding cannot be verified. Retirement
        is permanent: increasing keep_last later cannot resurrect expired media.
        """
        retention, media_store = policy(retention), validate_store(media_store)
        current = publication(current_body)
        records, retired = self.inspect()
        current_uuid = current["archive_uuid"]
        if records.get(current_uuid) != current or current_uuid in retired:
            raise InvalidArchive("Current backup is missing from active publication history")
        active = records.keys() - retired.keys()
        if set(retention["pins"]) - active:
            raise InvalidArchive("Pinned backup is missing or already retired; refusing cleanup")
        newest = sorted(active, key=lambda item: (item == current_uuid, records[item]["created_epoch"], item), reverse=True)
        keep = set(newest[:retention["keep_last"]]) | set(retention["pins"])
        protected = set()
        for archive_uuid in sorted(keep):
            graph = self._media_graph(records[archive_uuid])
            if graph["media_store"] != media_store:
                raise InvalidArchive("Retained backup uses a different cold store; refusing cleanup")
            protected.update(graph["keys"])
        if available_media is not None and protected - available_media.keys():
            raise InvalidArchive("Retained backup media is missing from the complete cold inventory; refusing cleanup")
        # The current pointer must still be our exact publication. A changed or
        # missing pointer cannot authorize older backup retirement.
        if not self.store.verified("current_manifest.json", current["master"]["sha256"], current["master"]["bytes"]):
            raise InvalidArchive("Current backup disappeared before retention")
        expired = active - keep
        for archive_uuid in sorted(expired):
            value = {"format": FORMAT + ".retirement", "version": 1, "archive_uuid": archive_uuid,
                     "publication": descriptor(publication_key(archive_uuid), json_bytes(records[archive_uuid])),
                     "retired_by": current_uuid}
            self.store.put_bytes(HISTORY + "retirements/" + archive_uuid + ".json", json_bytes(value))
        # Large derived media graphs only help retained snapshots. Permanent
        # small publication/retirement receipts keep their remote UUID history.
        for archive_uuid in sorted(set(retired) | expired):
            name = "media-" + archive_uuid + ".json"
            graph = self._read_cache(name, MASTER_BYTES)
            if graph is not None:
                if not isinstance(graph, dict) or graph.get("master") != records[archive_uuid]["master"]:
                    raise InvalidArchive("Retired media cache differs from its original publication")
                (self.cache / name).unlink()
        sync_directory(self.cache)
        return {"retained": sorted(keep), "retired": sorted(set(retired) | expired), "protected_media": protected}
