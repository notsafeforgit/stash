"""Persistent producer/config snapshots belonging to one native checkpoint.

Prepare under the original host worker barriers. Only that live invocation may
request a new server capture; a reopened or uncertain attempt can only GET an
already sealed checkpoint. Never merge fresh worker state into an older capture.
"""

import base64
from contextlib import closing
import hashlib
import os
from pathlib import Path

from .artwork_pins import directory
from .bundle import NAME, ROLES, SQLITE_ROLES, connect_readonly, database_metadata, snapshot_database
from .checkpoint_release import checkpoint_digest
from .server_checkpoint import RESERVED
from .storage import (HEX, InvalidArchive, decode_json, json_bytes, open_regular,
                      publish_bytes, require_space, sync_directory)

FORMAT = "org.notsafeforgit.stash.host-components"
MANIFEST = ("operating_state", "host-components.json")
MAX_MANIFEST = 16 << 20


def signature(info):
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns


def digest_file(path):
    with open_regular(path) as incoming:
        before = os.fstat(incoming.fileno())
        digest = hashlib.file_digest(incoming, "sha256").hexdigest()
        after = os.fstat(incoming.fileno())
    if signature(before) != signature(after) or signature(after) != signature(Path(path).lstat()):
        raise InvalidArchive("Staged component changed while checking its digest")
    return digest, after.st_size


def copy_file(source, target, reserve):
    with open_regular(source) as incoming:
        before = os.fstat(incoming.fileno())
        require_space(target.parent, before.st_size, reserve)
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as output:
            count = 0
            while chunk := incoming.read(1 << 20):
                count += len(chunk)
                if count > before.st_size:
                    raise InvalidArchive("External configuration grew during capture")
                require_space(target.parent, before.st_size - count, reserve)
                output.write(chunk)
            output.flush()
            os.fsync(output.fileno())
        if count != before.st_size or signature(before) != signature(os.fstat(incoming.fileno())) or signature(before) != signature(source.lstat()):
            raise InvalidArchive("External configuration changed during capture")


class ComponentStage:
    def __init__(self, cache, client, components, barrier, *, reserve=50 << 30):
        if not barrier.acquired or client.boundary is None:
            raise InvalidArchive("Component staging requires the native filesystem capture barrier")
        self.cache = Path(cache).resolve(strict=True)
        self.cache_identity = directory(self.cache, private=True)
        self.path = self.cache / client.request_id
        self.client, self.barrier, self.reserve = client, barrier, reserve
        self.fresh = False
        plan = []
        selected = set()
        for component in components:
            if not isinstance(component, dict) or set(component) != {"role", "name", "path"}:
                raise InvalidArchive("Staged components require role/name/path objects")
            role, name = component["role"], component["name"]
            if (not isinstance(role, str) or role not in ROLES - {"media_manifest"}
                    or not isinstance(name, str) or not NAME.fullmatch(name)
                    or (role, name) in selected | RESERVED | {MANIFEST}):
                raise InvalidArchive("Unsupported or duplicate staged component")
            selected.add((role, name))
            plan.append({"role": role, "name": name,
                         "source_path": base64.b64encode(os.fsencode(Path(component["path"]).absolute())).decode("ascii")})
        if len(plan) > 16384:
            raise InvalidArchive("Too many external backup components")
        plan.sort(key=lambda c: (c["role"], c["name"]))
        self.intent = {"format": FORMAT, "version": 1, "uuid": client.request_id, "server": client.server,
                       "request_sha256": client.request_hash(reserve), "components": plan}
        if len(json_bytes(self.intent)) > MAX_MANIFEST:
            raise InvalidArchive("External backup inventory exceeds its size limit")
        require_space(self.cache, 0, reserve)
        try:
            self.path.mkdir(mode=0o700)
        except FileExistsError:
            directory(self.path, private=True)
            if self.read("intent.json") != self.intent:
                raise InvalidArchive("Checkpoint retry changed its external inventory or server request")
            self.record = self.read("manifest.json")
            self.validate_manifest()
        else:
            publish_bytes(self.path / "intent.json", json_bytes(self.intent))
            sync_directory(self.cache)
            entries = []
            # Preserve the producer causal prefix: all download archives, then
            # all outboxes, then the eventual native server view.
            ordered = sorted(enumerate(plan), key=lambda c: (0 if c[1]["role"] == "download_archive" else
                                                            1 if c[1]["role"] == "producer_outbox" else 2, c[0]))
            for index, spec in ordered:
                source = Path(os.fsdecode(base64.b64decode(spec["source_path"], validate=True)))
                target = self.component_path(index)
                metadata = None
                if spec["role"] in SQLITE_ROLES:
                    metadata = snapshot_database(source, target, spec["role"], reserve)
                else:
                    copy_file(source, target, reserve)
                digest, size = digest_file(target)
                entry = {"index": index, **spec, "sha256": digest, "bytes": size}
                if metadata is not None:
                    entry["sqlite"] = metadata
                entries.append(entry)
            self.record = {**self.intent, "components": sorted(entries, key=lambda c: c["index"])}
            if len(json_bytes(self.record)) > MAX_MANIFEST:
                raise InvalidArchive("External backup manifest exceeds its size limit")
            publish_bytes(self.path / "manifest.json", json_bytes(self.record))
            self.fresh = True
        self.manifest_sha256, self.manifest_size = digest_file(self.path / "manifest.json")
        self.verify()
        self.sealed = self.read("checkpoint.json", optional=True)
        if self.sealed is not None:
            client.validate(self.sealed, self.intent["request_sha256"])
            client.expected_checkpoint_sha256 = checkpoint_digest(self.sealed)
        if not self.fresh or (self.path / "requested.json").exists():
            client.existing_only = True

    def read(self, name, *, optional=False):
        try:
            with open_regular(self.path / name) as incoming:
                data = incoming.read(MAX_MANIFEST + 1)
        except FileNotFoundError:
            if optional:
                return None
            raise InvalidArchive("Incomplete external backup stage; abandon this attempt before using a new UUID") from None
        if len(data) > MAX_MANIFEST:
            raise InvalidArchive("Oversized external backup stage record")
        return decode_json(data)

    def component_path(self, index):
        return self.path / f"component-{index:05d}"

    def validate_manifest(self):
        if (not isinstance(self.record, dict) or set(self.record) != set(self.intent)
                or any(self.record[k] != self.intent[k] for k in self.intent if k != "components")
                or not isinstance(self.record["components"], list)
                or len(self.record["components"]) != len(self.intent["components"])):
            raise InvalidArchive("External backup stage identity changed")
        for index, (entry, spec) in enumerate(zip(self.record["components"], self.intent["components"])):
            fields = {"index", "role", "name", "source_path", "sha256", "bytes"}
            if spec["role"] in SQLITE_ROLES:
                fields.add("sqlite")
            if (not isinstance(entry, dict) or set(entry) != fields or entry["index"] != index
                    or type(entry["index"]) is not int or any(entry[k] != spec[k] for k in spec)
                    or type(entry["bytes"]) is not int or entry["bytes"] < 0
                    or not isinstance(entry["sha256"], str) or not HEX.fullmatch(entry["sha256"])):
                raise InvalidArchive("Invalid external backup component")

    def verify(self):
        if directory(self.cache, private=True) != self.cache_identity:
            raise InvalidArchive("External backup cache was replaced")
        directory(self.path, private=True)
        if any((self.path / name).exists() or (self.path / name).is_symlink() for name in ("release.json", "released.json", "abandoned.json")):
            raise InvalidArchive("External backup components have been released")
        if digest_file(self.path / "manifest.json") != (self.manifest_sha256, self.manifest_size):
            raise InvalidArchive("External backup manifest changed")
        for entry in self.record["components"]:
            path = self.component_path(entry["index"])
            if digest_file(path) != (entry["sha256"], entry["bytes"]):
                raise InvalidArchive("Retained external component changed")
            if entry["role"] in SQLITE_ROLES:
                with closing(connect_readonly(path)) as db:
                    if database_metadata(db, entry["role"]) != entry["sqlite"]:
                        raise InvalidArchive("Retained external database identity changed")
        return self

    def binding(self):
        return {"format": FORMAT, "version": 1, "uuid": self.intent["uuid"],
                "sha256": self.manifest_sha256, "bytes": self.manifest_size,
                "count": len(self.record["components"])}

    def validate_boundary(self, boundary):
        if (boundary.get("uuid") != self.intent["uuid"] or boundary.get("request_sha256") != self.intent["request_sha256"]
                or boundary.get("details", {}).get("components") != self.binding()):
            raise InvalidArchive("External component stage differs from the sealed filesystem boundary")

    def seal(self):
        if self.fresh and not self.client.existing_only:
            if not self.barrier.acquired:
                raise InvalidArchive("The original staging barrier must remain held through native capture")
            publish_bytes(self.path / "requested.json", json_bytes(self.binding()))
        elif self.sealed is None:
            self.client.existing_only = True
        # Once a request might reach the server, no retry can create another
        # view, even if its response or the local completion marker was lost.
        self.fresh = False
        try:
            body = self.client.seal(reserve=self.reserve)
        finally:
            self.client.existing_only = True
        manifest = decode_json(body)
        self.validate_boundary(self.client.boundary_receipt)
        if self.sealed is None:
            publish_bytes(self.path / "checkpoint.json", json_bytes(manifest))
            self.sealed = manifest
        elif checkpoint_digest(manifest) != checkpoint_digest(self.sealed):
            raise InvalidArchive("The retained native checkpoint changed")
        self.client.expected_checkpoint_sha256 = checkpoint_digest(manifest)
        return manifest

    def export_components(self, client, reserve):
        if (client is not self.client or client.request_hash(reserve) != self.intent["request_sha256"]
                or self.sealed is None or not client.existing_only):
            raise InvalidArchive("Export requires the original sealed external component stage")
        self.verify()
        return [{"role": e["role"], "name": e["name"], "path": self.component_path(e["index"])}
                for e in self.record["components"]] + [{"role": MANIFEST[0], "name": MANIFEST[1], "path": self.path / "manifest.json"}]

    def expected(self):
        return {(e["role"], e["name"]): (e["sha256"], e["bytes"]) for e in self.record["components"]} | {
            MANIFEST: (self.manifest_sha256, self.manifest_size)}

    def databases(self):
        return {(e["role"], e["name"]): e["sqlite"] for e in self.record["components"] if "sqlite" in e}


def release_published_components(source, client, cache):
    """Retire staged copies only after the publisher verifies durable publication.

    Derive every deletion from the exact archived stage inventory. Original live
    source paths are never opened, and worker barriers are unnecessary for these
    private immutable copies. Small identities and receipts remain permanently.
    """
    import io
    from .bundle import iter_artifacts
    from .checkpoint_release import archived_boundary, release_published_checkpoint
    from .storage import load_manifest, write_artifact
    boundary = archived_boundary(source, client)
    entries = {(e["role"], e["name"]): e for e in iter_artifacts(source, load_manifest(source))}
    descriptor = entries.get(MANIFEST)
    if descriptor is None or descriptor["size"] > MAX_MANIFEST:
        raise InvalidArchive("Published archive has no bounded external component inventory")
    output = io.BytesIO()
    write_artifact(Path(source), descriptor, output)
    # Reopen from the archived identity, including after an interrupted cleanup;
    # never prepare from live sources or require their current configuration.
    stage = ComponentStage.__new__(ComponentStage)
    stage.cache = Path(cache).resolve(strict=True)
    stage.cache_identity = directory(stage.cache, private=True)
    stage.path = stage.cache / client.request_id
    directory(stage.path, private=True)
    stage.intent, stage.record = stage.read("intent.json"), stage.read("manifest.json")
    if (not isinstance(stage.intent, dict) or set(stage.intent) != {"format", "version", "uuid", "server", "request_sha256", "components"}
            or stage.intent["format"] != FORMAT or type(stage.intent["version"]) is not int or stage.intent["version"] != 1
            or stage.intent["uuid"] != client.request_id or not isinstance(stage.intent["components"], list)
            or len(stage.intent["components"]) > 16384 or stage.record != decode_json(output.getvalue())):
        raise InvalidArchive("Released external inventory differs from the published archive")
    stage.validate_manifest()
    stage.manifest_sha256, stage.manifest_size = digest_file(stage.path / "manifest.json")
    if (stage.manifest_sha256, stage.manifest_size) != (descriptor["sha256"], descriptor["size"]):
        raise InvalidArchive("Released external inventory bytes changed")
    stage.validate_boundary(boundary)
    for entry in stage.record["components"]:
        artifact = entries.get((entry["role"], entry["name"]))
        if (artifact is None or (artifact["sha256"], artifact["size"]) != (entry["sha256"], entry["bytes"])
                or artifact.get("sqlite") != entry.get("sqlite")):
            raise InvalidArchive("Published archive is missing an exact staged component")
    release = release_published_checkpoint(source, client)
    body = json_bytes({"format": FORMAT + "-release", "version": 1,
                       "components_sha256": stage.manifest_sha256, "server_release": release})
    intent, complete = stage.path / "release.json", stage.path / "released.json"

    def matching(path):
        try:
            with open_regular(path) as incoming:
                stored = incoming.read(len(body) + 1)
        except FileNotFoundError:
            return False
        if stored != body:
            raise InvalidArchive("External component release binding changed")
        return True

    if matching(complete):
        return release
    started, owned = matching(intent), []
    for entry in stage.record["components"]:
        path = stage.component_path(entry["index"])
        try:
            digest, size = digest_file(path)
        except FileNotFoundError:
            if started:
                continue
            raise InvalidArchive("Unreleased external component is missing") from None
        if (digest, size) != (entry["sha256"], entry["bytes"]):
            raise InvalidArchive("Released external component changed")
        owned.append((path, signature(path.lstat())))
    if not started:
        publish_bytes(intent, body)
    for path, identity in owned:
        if signature(path.lstat()) != identity:
            raise InvalidArchive("External component was replaced before release")
        path.unlink()
    sync_directory(stage.path)
    publish_bytes(complete, body)
    return release
