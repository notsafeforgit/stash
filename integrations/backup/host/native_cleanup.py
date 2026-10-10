"""Retire only objects belonging to durably retired native snapshots.

The backup lock excludes other publishers. Complete retained graphs protect
shared chunks. A small permanent completion receipt precedes retirement of the
old inventory, so cache loss never requires an already-expired inventory.
Unknown uploads and unsuccessful attempts are not garbage-collection roots.
"""

import hashlib
from pathlib import Path
import tempfile

from manifest_limits import MASTER_BYTES
from native_history import FORMAT, publication, publication_key
from native_store import PREFIX, descriptor
from native_tags import RETIRED_TAG, reconcile
from object_receipts import ObjectReceipts, inventory
from stash_archive.artwork_pins import directory
from stash_archive.filesystem_boundary import canonical_uuid
from stash_archive.storage import HEX, InvalidArchive, decode_json, json_bytes, open_regular, publish_bytes, require_space, sync_directory

COMPLETED = PREFIX + "cleanup/completed/"


def lifecycle_rules(prefix=""):
    """Reviewable configuration only; never install or alter cloud policies."""
    tag = {"Key": RETIRED_TAG, "Value": "true"}
    match = {"And": {"Prefix": prefix, "Tags": [tag]}} if prefix else {"Tag": tag}
    rules = [{"ID": "StashNativeRetired", "Status": "Enabled", "Filter": match, "Expiration": {"Days": 1}}]
    for name, path in (("Archive", PREFIX), ("Manifests", "manifests/runs/"),
                       ("Current", "current_manifest.json"), ("CurrentText", "current_manifest.txt")):
        rules.append({"ID": "StashNative" + name + "Versions", "Status": "Enabled",
                      "Filter": {"Prefix": prefix + path},
                      "NoncurrentVersionExpiration": {"NoncurrentDays": 1},
                      "Expiration": {"ExpiredObjectDeleteMarker": True}})
    return rules


def verify_policy(store):
    try:
        config = store.client.get_bucket_lifecycle_configuration(Bucket=store.bucket)
        versioning = store.client.get_bucket_versioning(Bucket=store.bucket)
    except Exception as error:
        raise InvalidArchive("Standard cleanup requires readable, reviewed bucket lifecycle and versioning settings") from error
    rules = config.get("Rules") if isinstance(config, dict) else None
    if (not isinstance(rules, list) or any(not isinstance(rule, dict) for rule in rules)
            or any(rule.get("Status") not in ("Enabled", "Disabled") for rule in rules)
            or sorted((rule for rule in rules if rule["Status"] == "Enabled"), key=lambda rule: str(rule.get("ID")))
            != sorted(lifecycle_rules(store.prefix), key=lambda rule: rule["ID"])):
        raise InvalidArchive("Standard cleanup lifecycle differs from the reviewed native policy")
    if (not isinstance(versioning, dict) or versioning.get("Status") not in (None, "Enabled", "Suspended")
            or versioning.get("MFADelete") not in (None, "Disabled")):
        raise InvalidArchive("Standard cleanup cannot verify the bucket versioning state")


class NativeCleanup:
    def __init__(self, history, *, reserve=50 << 30):
        self.history, self.store, self.reserve = history, history.store, reserve

    def graph(self, record):
        name = "native-" + record["archive_uuid"] + ".json"
        graph = self.history._read_cache(name, MASTER_BYTES)
        if graph is None:
            # The configured cache filesystem has the reserved backup headroom;
            # the system temp directory may be a much smaller memory filesystem.
            with tempfile.TemporaryDirectory(prefix="stash-native-graph-", dir=self.history.cache) as temp:
                _, objects = self.store.fetch_metadata(record["native_archive"], temp, reserve=self.reserve)
            graph = {"reference": record["native_archive"], "objects": objects}
            require_space(self.history.cache, len(json_bytes(graph)) + 128, self.reserve)
            self.history._save_cache(name, graph)
        if (not isinstance(graph, dict) or set(graph) != {"reference", "objects"}
                or graph["reference"] != record["native_archive"] or not isinstance(graph["objects"], dict)
                or any(not HEX.fullmatch(key) or type(size) is not int or size < 0
                       for key, size in graph["objects"].items())):
            raise InvalidArchive("Invalid retained native object graph")
        return graph

    @staticmethod
    def metadata(record):
        return [record["master"], *(record["native_archive"][kind] for kind in ("manifest", "inventory", "verification"))]

    @staticmethod
    def chunks(graph):
        return [{"key": PREFIX + "objects/" + key + ".gz", "sha256": key, "bytes": size}
                for key, size in graph["objects"].items()]

    def completed(self, records, retired):
        result = {}
        for full_key, item in inventory(self.store.client, self.store.bucket, self.store.prefix + COMPLETED).items():
            key = full_key[len(self.store.prefix):]
            name = key[len(COMPLETED):]
            if not name.endswith(".json") or not canonical_uuid(name[:-5]) or name[:-5] not in retired:
                raise InvalidArchive("Unknown native cleanup receipt; review required")
            archive_uuid = name[:-5]
            value = self.history._record(key, item, "cleanup-" + name)
            if (not isinstance(value, dict) or set(value) != {"format", "version", "publication", "graph_sha256"}
                    or value["format"] != FORMAT + ".chunk-retirement" or type(value["version"]) is not int
                    or value["version"] != 1
                    or value["publication"] != descriptor(publication_key(archive_uuid), json_bytes(records[archive_uuid]))
                    or not isinstance(value["graph_sha256"], str) or not HEX.fullmatch(value["graph_sha256"])):
                raise InvalidArchive("Invalid native chunk retirement receipt")
            result[archive_uuid] = value
        return result

    def prune_local(self, runs, record):
        """Remove four known large files after release, retaining UUID receipts.

        The small retirement intent precedes the first unlink. It makes a
        partial cleanup resumable even after the master itself has been removed.
        Unknown files and unfinished attempts are never removed.
        """
        runs = Path(runs)
        directory(runs, private=True)
        root = runs / record["run_id"]
        if not root.exists():
            return 0
        directory(root, private=True)

        def read(path, limit=32768):
            try:
                with open_regular(path) as incoming:
                    body = incoming.read(limit + 1)
            except FileNotFoundError:
                return None
            if len(body) > limit:
                raise InvalidArchive("Local backup retirement record exceeds its bound")
            return decode_json(body)

        active = read(runs.parent / "active.json")
        if active is not None and active.get("run_id") == record["run_id"]:
            return 0
        finished = read(root / "finished.json")
        if finished is None:
            return 0
        expected = {"master_sha256": record["master"]["sha256"], "publication": record["native_archive"]}
        saved = read(root / "identity.json")
        if (finished != expected or read(root / "released.json") != expected
                or not isinstance(saved, dict) or saved.get("run_id") != record["run_id"]
                or saved.get("checkpoint_uuid") != record["native_archive"]["checkpoint_uuid"]
                or saved.get("bucket") != self.store.bucket or not isinstance(saved.get("prefix"), str)
                or saved["prefix"].rstrip("/") != self.store.prefix.rstrip("/")):
            raise InvalidArchive("Local retired run differs from its released publication")
        directory(root / "archive", private=True)
        intent_path = root / "local-retirement.json"
        intent = read(intent_path)
        binding = descriptor(publication_key(record["archive_uuid"]), json_bytes(record))
        fixed = {"master.json": record["master"], "prepared-master.json": record["master"],
                 "archive/artifacts.jsonl": record["native_archive"]["inventory"]}
        fixed = {name: {"sha256": value["sha256"], "bytes": value["bytes"]} for name, value in fixed.items()}
        if intent is None:
            with open_regular(root / "master.json") as incoming:
                body = incoming.read(MASTER_BYTES + 1)
            if publication(body) != record:
                raise InvalidArchive("Local master differs from the retired publication")
            catalog = decode_json(body)
            del catalog["native_archive"]
            value = descriptor("catalog.json", json_bytes(catalog))
            intent = {"publication": binding, "files": fixed | {"catalog.json": {"sha256": value["sha256"], "bytes": value["bytes"]}}}
            publish_bytes(intent_path, json_bytes(intent))
        if (not isinstance(intent, dict) or set(intent) != {"publication", "files"} or intent["publication"] != binding
                or not isinstance(intent["files"], dict) or set(intent["files"]) != set(fixed) | {"catalog.json"}
                or any(intent["files"][name] != value for name, value in fixed.items())):
            raise InvalidArchive("Invalid local backup retirement intent")
        self.store.validate_read({"key": "catalog.json", **intent["files"]["catalog.json"]}, MASTER_BYTES)
        removed = 0
        for name, value in intent["files"].items():
            path = root / name
            try:
                with open_regular(path) as incoming:
                    if incoming.seek(0, 2) != value["bytes"]:
                        raise InvalidArchive("Local retired backup file changed before cleanup")
                    incoming.seek(0)
                    if hashlib.file_digest(incoming, "sha256").hexdigest() != value["sha256"]:
                        raise InvalidArchive("Local retired backup file changed before cleanup")
            except FileNotFoundError:
                continue
            path.unlink()
            removed += value["bytes"]
        sync_directory(root / "archive")
        sync_directory(root)
        return removed

    def run(self, current_body, *, runs=None):
        verify_policy(self.store)
        current = publication(current_body)
        records, retired = self.history.inspect()
        if records.get(current["archive_uuid"]) != current or current["archive_uuid"] in retired:
            raise InvalidArchive("Standard cleanup requires an active successful current backup")
        completed = self.completed(records, retired)
        protected = {}
        for archive_uuid in sorted(records.keys() - retired.keys()):
            record = records[archive_uuid]
            for value in [*self.chunks(self.graph(record)), *self.metadata(record)]:
                if value["key"] in protected and protected[value["key"]] != value:
                    raise InvalidArchive("Retained snapshots disagree about a shared native object")
                protected[value["key"]] = value
        listed = {}
        for prefix in (PREFIX + "objects/", PREFIX + "runs/", "manifests/runs/"):
            listed.update(inventory(self.store.client, self.store.bucket, self.store.prefix + prefix))
        if any(self.store.prefix + key not in listed for key in protected):
            raise InvalidArchive("Retained native data is missing from the complete inventory; refusing cleanup")
        if not self.store.verified("current_manifest.json", current["master"]["sha256"], current["master"]["bytes"]):
            raise InvalidArchive("Current backup disappeared before Standard cleanup")
        receipts = ObjectReceipts(self.store.receipts_path, self.store.bucket) if self.store.receipts_path is not None else None
        try:
            for value in protected.values():
                if not reconcile(self.store, value["key"], value["sha256"], value["bytes"], "live",
                                 receipts=receipts, listed=listed):
                    raise InvalidArchive("Retained native object expired before Standard cleanup")
            scheduled = set()
            # Process one retired graph at a time; a long pre-activation history
            # must not retain every large graph in memory or scratch storage.
            # All active roots have already been verified before any retirement.
            for archive_uuid in sorted(retired.keys() - completed.keys()):
                graph = self.graph(records[archive_uuid])
                for value in self.chunks(graph):
                    if value["key"] not in protected:
                        reconcile(self.store, value["key"], value["sha256"], value["bytes"], "retired",
                                  receipts=receipts, listed=listed)
                        scheduled.add(value["key"])
                value = {"format": FORMAT + ".chunk-retirement", "version": 1,
                         "publication": descriptor(publication_key(archive_uuid), json_bytes(records[archive_uuid])),
                         "graph_sha256": hashlib.sha256(json_bytes(graph)).hexdigest()}
                self.store.put_bytes(COMPLETED + archive_uuid + ".json", json_bytes(value))
                (self.history.cache / ("native-" + archive_uuid + ".json")).unlink(missing_ok=True)
            for archive_uuid in sorted(retired):
                for value in self.metadata(records[archive_uuid]):
                    reconcile(self.store, value["key"], value["sha256"], value["bytes"], "retired",
                              receipts=receipts, listed=listed)
                # All chunk work has a durable remote receipt. Preserve small
                # identity records while bounding large derived local graphs.
                (self.history.cache / ("native-" + archive_uuid + ".json")).unlink(missing_ok=True)
            sync_directory(self.history.cache)
            local_bytes = sum(self.prune_local(runs, records[archive_uuid]) for archive_uuid in sorted(retired)) if runs is not None else 0
            return {"retired_snapshots": len(retired), "protected_objects": len(protected),
                    "newly_processed_chunks": len(scheduled), "local_bytes_reclaimed": local_bytes}
        finally:
            if receipts is not None:
                receipts.close()
