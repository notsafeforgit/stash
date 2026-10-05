"""Daily host publisher coordination; installed only at native cutover."""

from contextlib import closing
import base64
import hashlib
import os
from pathlib import Path
import re
import shutil
import sqlite3
import tempfile
import time
import uuid

from stash_archive.artwork_pins import ArtworkPins, directory, release_published_artwork
from stash_archive.bundle import export_archive
from stash_archive.checkpoint_abandon import abandon_host_capture, checkpoint_status, validate_abandonment
from stash_archive.component_stage import release_published_components
from stash_archive.filesystem_boundary import canonical_uuid
from stash_archive.host_boundary import HostFilesystemCapture
from stash_archive.storage import (HEX, InvalidArchive, decode_json, json_bytes, load_manifest, open_regular,
                                   publish_bytes, regular, require_space, sync_directory)
from stash_archive.server_checkpoint import ServerCheckpoint
from stash_archive.verification import verify_archive_proofs, validator_path
from stash_archive.zfs_media import ZFSMedia, mounts, release_published_media

from native_store import (MEDIA_FORMAT, NativeStore, archive_objects, media_selection, selection_digest,
                          validate_reference, validate_selection_binding)
import worker_inventory

CONFIG_FORMAT = "org.notsafeforgit.stash.host-backup"
RUN_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,127}\Z")


def private_directory(path):
    path = Path(path).absolute()
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory(path, private=True)
    return path


def same_or_publish(path, body):
    try:
        with open_regular(path) as incoming:
            if incoming.read(len(body) + 1) != body:
                raise InvalidArchive("A retained host backup record changed")
    except FileNotFoundError:
        publish_bytes(path, body)


def read_record(path, *, limit=128 << 20, optional=False):
    try:
        with open_regular(path) as incoming:
            body = incoming.read(limit + 1)
    except FileNotFoundError:
        if optional:
            return None
        raise
    if len(body) > limit:
        raise InvalidArchive("Retained host record exceeds its size limit")
    return decode_json(body)


class OwnedWorkspace:
    """Private scratch with durable inode ownership, never a source location."""
    def __init__(self, root, name):
        if name not in {"pack", "verify"}:
            raise InvalidArchive("Unknown native scratch workspace")
        self.path = Path(root) / ("scratch-" + name)
        record_path = Path(root) / ("scratch-" + name + ".json")
        saved = read_record(record_path, limit=4096, optional=True)
        if saved is None:
            self.path.mkdir(mode=0o700, exist_ok=True)
            identity = directory(self.path, private=True)
            if any(self.path.iterdir()):
                raise InvalidArchive("Unowned native scratch data requires inspection")
            saved = {"format": CONFIG_FORMAT + ".scratch", "version": 1, "name": name,
                     "device": identity[0], "inode": identity[1]}
            publish_bytes(record_path, json_bytes(saved))
        if (not isinstance(saved, dict) or set(saved) != {"format", "version", "name", "device", "inode"}
                or saved["format"] != CONFIG_FORMAT + ".scratch" or type(saved["version"]) is not int or saved["version"] != 1
                or saved["name"] != name or type(saved["device"]) is not int or type(saved["inode"]) is not int):
            raise InvalidArchive("Invalid native scratch ownership record")
        self.identity = saved["device"], saved["inode"]
        self.verify()

    def verify(self):
        if directory(self.path, private=True) != self.identity:
            raise InvalidArchive("Native scratch workspace was replaced")

    def clear(self):
        self.verify()
        # The enclosing run lock excludes the original packer and is inherited
        # by the validator, including if its Python parent died unexpectedly.
        # Never traverse a mounted filesystem inside the owned scratch tree.
        if any(path.is_relative_to(self.path) for path, _, _ in mounts()):
            raise InvalidArchive("Native scratch contains an unexpected mounted filesystem")
        for root, dirs, files in os.walk(self.path, followlinks=False):
            for name in dirs + files:
                info = (Path(root) / name).lstat()
                if info.st_dev != self.identity[0]:
                    raise InvalidArchive("Native scratch contains an unexpected mounted filesystem")
        for path in self.path.iterdir():
            if path.is_symlink() or not path.is_dir():
                path.unlink()
            else:
                shutil.rmtree(path)
        sync_directory(self.path)


class NativeRunJournal:
    """One durable attempt under the caller's existing backup/dedupe lock.

    Never replace the active attempt with a new capture after a restart. The
    pointer is retired after publication/release or fenced unsealed abandonment.
    """
    def __init__(self, state, proposed_id, binding, options):
        if not isinstance(proposed_id, str) or not RUN_NAME.fullmatch(proposed_id):
            raise InvalidArchive("Invalid host backup run identity")
        if (not isinstance(options, dict) or options.keys() - {"init_run", "compact", "delta_cutover_now", "defer_cleanup"}
                or any(type(value) is not bool for value in options.values())):
            raise InvalidArchive("Invalid retained host backup options")
        self.state = private_directory(state)
        self.runs = private_directory(self.state / "runs")
        self.active = self.state / "active.json"
        saved = read_record(self.active, limit=16384, optional=True)
        self.resumed = saved is not None
        if saved is None:
            # Recover the small window after an operator restored the journal
            # files, rather than quietly abandoning an older retained attempt.
            pending = []
            for path in self.runs.iterdir():
                if not RUN_NAME.fullmatch(path.name):
                    continue
                directory(path, private=True)
                record = read_record(path / "identity.json", limit=16384, optional=True)
                finished = read_record(path / "finished.json", limit=16384, optional=True)
                abandoned = read_record(path / "abandoned.json", limit=4096, optional=True)
                if abandoned is not None:
                    if finished is not None or not isinstance(record, dict):
                        raise InvalidArchive("Conflicting native backup terminal receipts")
                    validate_abandonment(abandoned, record.get("checkpoint_uuid"))
                if finished is not None:
                    if (not isinstance(finished, dict) or set(finished) != {"master_sha256", "publication"}
                            or not isinstance(finished["master_sha256"], str) or not HEX.fullmatch(finished["master_sha256"])
                            or not isinstance(record, dict)
                            or validate_reference(finished["publication"])["checkpoint_uuid"] != record.get("checkpoint_uuid")):
                        raise InvalidArchive("Invalid completed native run receipt")
                if record is not None and finished is None and abandoned is None:
                    pending.append(record)
            if len(pending) > 1:
                raise InvalidArchive("Multiple unfinished native runs require explicit recovery")
            if pending:
                saved, self.resumed = pending[0], True
            else:
                if (self.runs / proposed_id).exists():
                    proposed_id = proposed_id[:95] + "-" + uuid.uuid4().hex
                saved = {"format": CONFIG_FORMAT + ".run", "version": 1, **binding,
                         "run_id": proposed_id, "checkpoint_uuid": str(uuid.uuid4()),
                         "created_epoch": int(time.time()), "options": options}
        fields = {"format", "version", "run_id", "checkpoint_uuid", "created_epoch", "options", *binding}
        if (not isinstance(saved, dict) or set(saved) != fields or saved["format"] != CONFIG_FORMAT + ".run"
                or type(saved["version"]) is not int or saved["version"] != 1
                or not isinstance(saved["run_id"], str) or not RUN_NAME.fullmatch(saved["run_id"])
                or not canonical_uuid(saved["checkpoint_uuid"]) or type(saved["created_epoch"]) is not int
                or saved["created_epoch"] <= 0 or any(saved[k] != value for k, value in binding.items())
                or not isinstance(saved["options"], dict)
                or saved["options"].keys() - {"init_run", "compact", "delta_cutover_now", "defer_cleanup"}
                or any(type(value) is not bool for value in saved["options"].values())):
            raise InvalidArchive("Host backup retry changed its original identity, configuration or destination")
        self.record = saved
        # The pointer and full original identity precede every capture side effect.
        same_or_publish(self.active, json_bytes(saved))
        self.root = private_directory(self.runs / saved["run_id"])
        same_or_publish(self.root / "identity.json", json_bytes(saved))

    def complete(self, receipt):
        same_or_publish(self.root / "finished.json", json_bytes(receipt))
        self.clear_active()

    def abandon(self, receipt):
        validate_abandonment(receipt, self.record["checkpoint_uuid"])
        for name in ("finished.json", "publication.json", "master.json", "catalog.json", "prepared-master.json"):
            if (self.root / name).exists() or (self.root / name).is_symlink():
                raise InvalidArchive("Cannot abandon a run that reached publication")
        same_or_publish(self.root / "abandoned.json", json_bytes(receipt))
        self.clear_active()

    def clear_active(self):
        current = read_record(self.active, limit=16384, optional=True)
        if current is not None:
            if current != self.record:
                raise InvalidArchive("The active native backup changed before completion")
            self.active.unlink()
            sync_directory(self.state)


def copy_ledger(source, target, reserve):
    """Publish only a flushed, complete snapshot; partial files are never reused."""
    if target.exists():
        regular(target)
        return
    before = regular(source)
    require_space(target.parent, before.st_size, reserve)
    fd, temporary = tempfile.mkstemp(prefix=".ledger-", dir=target.parent)
    os.close(fd)
    temporary = Path(temporary)
    try:
        if source.suffix in (".sqlite", ".sqlite3"):
            with closing(sqlite3.connect(source.resolve().as_uri() + "?mode=ro", uri=True)) as db, closing(sqlite3.connect(temporary)) as output:
                db.backup(output, pages=1024, progress=lambda *_: require_space(target.parent, 0, reserve))
                if output.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
                    raise InvalidArchive("Host ledger snapshot failed SQLite integrity verification")
        else:
            with open_regular(source) as incoming, temporary.open("wb") as output:
                opened = os.fstat(incoming.fileno())
                if (opened.st_dev, opened.st_ino, opened.st_size, opened.st_mtime_ns, opened.st_ctime_ns) != (
                        before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns):
                    raise InvalidArchive("Host ledger changed before its snapshot")
                while chunk := incoming.read(1 << 20):
                    require_space(target.parent, len(chunk), reserve)
                    output.write(chunk)
                output.flush()
                os.fsync(output.fileno())
            after = regular(source)
            if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                    after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
                raise InvalidArchive("Host ledger changed during its snapshot")
        with open_regular(temporary) as incoming:
            os.fsync(incoming.fileno())
        os.link(temporary, target, follow_symlinks=False)
        sync_directory(target.parent)
    finally:
        temporary.unlink(missing_ok=True)


class NativeBackupSession:
    def __init__(self, filename, run_id, live_media, bucket, prefix, s3_client, *, options=None, lock_fd=None):
        self.lock_fd = lock_fd
        filename = Path(filename).resolve(strict=True)
        with open_regular(filename) as incoming:
            body = incoming.read((4 << 20) + 1)
        if len(body) > 4 << 20:
            raise InvalidArchive("Host backup configuration exceeds its size limit")
        config = decode_json(body)
        required = {"format", "version", "server", "api_key_file", "state_directory", "artwork_sources", "media",
                    "worker_lock_roots", "components", "recovery_roots", "producer_origin", "native_validator"}
        optional = {"reserve_bytes", "boundary_timeout", "validator_timeout", "zfs_command", "worker_inventory"}
        if (not isinstance(config, dict) or not required <= config.keys() or config.keys() - required - optional
                or config["format"] != CONFIG_FORMAT or type(config["version"]) is not int or config["version"] != 1):
            raise InvalidArchive("Invalid native host backup configuration")
        media = config["media"]
        if not isinstance(media, dict) or set(media) != {"dataset", "guid", "mountpoint", "relative_path"}:
            raise InvalidArchive("Native backup requires an explicit media dataset binding")
        self.reserve = config.get("reserve_bytes", 50 << 30)
        if type(self.reserve) is not int or self.reserve < 0:
            raise InvalidArchive("Invalid native backup space reserve")
        self.validator_timeout = config.get("validator_timeout", 3600)
        self.validator = validator_path(config["native_validator"], self.validator_timeout)
        self.producer_origin = config["producer_origin"]
        self.live_media = Path(live_media).resolve(strict=True)
        relative = Path(media["relative_path"])
        if relative.is_absolute() or ".." in relative.parts or not relative.parts or relative.parts[0] == ".zfs":
            raise InvalidArchive("Invalid native media path inside the dataset")
        if (Path(media["mountpoint"]) / relative).resolve(strict=True) != self.live_media:
            raise InvalidArchive("Native backup media binding differs from the existing host source")
        self.journal = NativeRunJournal(config["state_directory"], run_id,
                                        {"config_sha256": hashlib.sha256(body).hexdigest(), "live_media": str(self.live_media),
                                         "bucket": bucket, "prefix": prefix}, {} if options is None else options)
        saved, state = self.journal.record, self.journal.state
        self.store = NativeStore(s3_client, bucket, prefix, receipts_path=state / "object-receipts.sqlite3")
        self.run_id, self.root, self.created_epoch = saved["run_id"], self.journal.root, saved["created_epoch"]
        self.options, self.resumed = saved["options"], self.journal.resumed
        self.archive = self.root / "archive"
        abandoned = read_record(self.root / "abandoned.json", limit=4096, optional=True)
        if abandoned is not None:
            self.journal.abandon(abandoned)
            raise InvalidArchive("Unsealed backup attempt was abandoned; the next run will use a new identity")
        self.catalog = read_record(self.root / "catalog.json", optional=True)
        self.publication = read_record(self.root / "publication.json", optional=True)
        if self.publication is not None:
            validate_reference(self.publication)
        master = read_record(self.root / "master.json", optional=True)
        self.committed = master is not None
        if self.committed:
            if not isinstance(master, dict) or master.get("native_archive") != self.publication or self.publication is None:
                raise InvalidArchive("Retained master is missing its native publication")
            self.generated = self.read_generated()
            if (self.root / "released.json").exists():
                return
        pins_cache, media_cache, component_cache = [private_directory(state / name) for name in ("artwork", "media", "components")]
        self.pins = ArtworkPins(pins_cache, config["artwork_sources"], reserve=self.reserve)
        self.media = ZFSMedia(media_cache, media["dataset"], media["guid"], media["mountpoint"],
                             command=config.get("zfs_command", ["/usr/sbin/zfs"]), reserve=self.reserve, lock_fd=lock_fd)
        self.component_cache = component_cache
        api_file = Path(config["api_key_file"]).resolve(strict=True)
        with open_regular(api_file) as incoming:
            key = incoming.read(8194).decode("ascii").strip()
        if self.committed:
            # Release may already have removed server components, pins or the
            # media snapshot. Do not reopen or request any capture for cleanup.
            self.client = ServerCheckpoint(config["server"], key, saved["checkpoint_uuid"],
                                           config["recovery_roots"], existing_only=True)
            return
        components = list(config["components"]) + [
            {"role": "config", "name": "host-backup.json", "path": filename},
            {"role": "config", "name": "host-backup-api-key", "path": api_file}]
        with HostFilesystemCapture(config["worker_lock_roots"], self.pins, self.media) as capture:
            self.client = capture.client(config["server"], key, saved["checkpoint_uuid"], config["recovery_roots"],
                                         boundary_timeout=config.get("boundary_timeout", 120))
            if self.resumed:
                status = checkpoint_status(self.client, self.reserve)
                if status["state"] in {"missing", "partial", "abandoned"}:
                    if self.catalog is not None or self.publication is not None or self.archive.exists() or self.archive.is_symlink():
                        raise InvalidArchive("Unsealed server attempt contradicts retained publication data")
                    if lock_fd is None:
                        raise InvalidArchive("Abandoning a host capture requires backup/dedupe exclusion")
                    from stash_archive.locked_command import validate_lock
                    validate_lock(lock_fd)
                    receipt = abandon_host_capture(self.client, self.reserve, component_cache, self.pins, self.media)
                    self.journal.abandon(receipt)
                    raise InvalidArchive("Unsealed backup attempt was abandoned; the next run will use a new identity")
                self.client.existing_only = True
                if not (component_cache / saved["checkpoint_uuid"] / "manifest.json").is_file():
                    raise InvalidArchive("Sealed backup is missing its original external component stage")
            inventory = None
            if "worker_inventory" in config:
                report_path = self.root / "worker-inventory.json"
                if self.resumed:
                    # Recover the original closure even if profiles, mounts or
                    # archive membership have since changed or disappeared.
                    inventory = worker_inventory.validate_report(read_record(report_path))
                else:
                    inventory = worker_inventory.collect(config["worker_inventory"])
                    same_or_publish(report_path, json_bytes(inventory))
                components = worker_inventory.components_for_capture(inventory, components, config["worker_lock_roots"])
                components.append({"role": "operating_state", "name": "worker-inventory.json", "path": report_path})
            self.stage = capture.prepare(component_cache, self.client, components, reserve=self.reserve)
            if inventory is not None:
                worker_inventory.verify_stage(inventory, self.stage)
            self.stage.seal()
        self.view = self.media.open_bound(self.client.boundary_receipt).verify()
        self.media_path = self.view.resolve(relative)
        self.view_identity = (self.view.root.stat().st_dev, self.view.root.stat().st_ino)

    def read_generated(self):
        result = read_record(self.root / "generated.json", limit=1 << 20)
        if not isinstance(result, list) or not result or len(result) > 128:
            raise InvalidArchive("Invalid retained host ledger inventory")
        names = set()
        for entry in result:
            if (not isinstance(entry, dict) or set(entry) != {"name", "sha256", "bytes"}
                    or not isinstance(entry["name"], str) or Path(entry["name"]).name != entry["name"]
                    or (entry["name"] != "media.json" and not re.fullmatch(r"ledger-[0-9]+-[^/\\\x00]+", entry["name"]))
                    or entry["name"] in names or not isinstance(entry["sha256"], str) or not HEX.fullmatch(entry["sha256"])
                    or type(entry["bytes"]) is not int or entry["bytes"] < 0):
                raise InvalidArchive("Unsafe or changed host ledger inventory")
            names.add(entry["name"])
        return result

    def check_source(self):
        info = self.view.root.stat()
        if ((info.st_dev, info.st_ino) != self.view_identity
                or not os.statvfs(self.view.root).f_flag & os.ST_RDONLY
                or not self.media_path.is_dir() or self.media_path.stat().st_dev != info.st_dev):
            raise InvalidArchive("Retained native media view became unavailable or changed")

    def publish(self, catalog, ledger_paths):
        self.view.verify()
        self.check_source()
        retained = {key: value for key, value in catalog.items() if key != "native_archive"}
        same_or_publish(self.root / "catalog.json", json_bytes(retained))
        self.catalog = retained
        selection = selection_digest(catalog)
        media_document = {"format": MEDIA_FORMAT, "version": 1,
                          "checkpoint_uuid": self.client.request_id,
                          "filesystem_boundary_sha256": hashlib.sha256(self.client.boundary_bytes).hexdigest(),
                          "selection_sha256": selection, "selection": media_selection(catalog)}
        media_file = self.root / "media.json"
        same_or_publish(media_file, json_bytes(media_document))
        # Capture the post-upload host ledger under the existing global run
        # lock. It describes the same media selection as the master manifest.
        generated = [{"role": "media_manifest", "name": "s3-media.json", "path": media_file}]
        for index, source in enumerate(ledger_paths):
            source = Path(source)
            if not source.exists():
                continue
            target = self.root / ("ledger-" + str(index) + "-" + source.name)
            copy_ledger(source, target, self.reserve)
            generated.append({"role": "operating_state", "name": target.name, "path": target})
        self.generated = []
        for entry in generated:
            with open_regular(entry["path"]) as incoming:
                self.generated.append({"name": Path(entry["path"]).name, "bytes": os.fstat(incoming.fileno()).st_size,
                                       "sha256": hashlib.file_digest(incoming, "sha256").hexdigest()})
        same_or_publish(self.root / "generated.json", json_bytes(self.generated))
        self.archive = self.root / "archive"
        if not self.archive.exists():
            workspace = OwnedWorkspace(self.root, "pack")
            pending = workspace.path / "archive"
            if not (pending / "manifest.json").exists():
                workspace.clear()
                export_archive(None, pending, components=generated, producer_origin=self.producer_origin,
                               server_checkpoint=self.client, artwork_pins=self.pins, media_snapshot=self.media,
                               component_stage=self.stage, reserve=self.reserve)
            # A crash after sealing but before promotion must reuse those bytes.
            directory(pending, private=True)
            validate_selection_binding(pending, self.client.request_id, selection)
            pending.rename(self.archive)
            sync_directory(workspace.path)
            sync_directory(self.root)
        directory(self.archive, private=True)
        media = validate_selection_binding(self.archive, self.client.request_id, selection)
        if media != media_document:
            raise InvalidArchive("Retained archive differs from the original native/media selection")
        workspace = OwnedWorkspace(self.root, "verify")
        workspace.clear()
        proof = verify_archive_proofs(self.archive, native_validator=self.validator, producer_origin=self.producer_origin,
                                      timeout=self.validator_timeout, temp_parent=workspace.path, reserve=self.reserve,
                                      lock_fd=getattr(self, "lock_fd", None))
        self.publication = self.store.publish_archive(self.archive, proof, self.client.request_id, selection)
        same_or_publish(self.root / "publication.json", json_bytes(self.publication))
        return self.publication

    def prepare_master(self, body):
        if len(body) > 128 << 20:
            raise InvalidArchive("Master manifest exceeds its publication size limit")
        catalog = decode_json(body)
        if (self.publication is None or catalog.get("native_archive") != self.publication
                or selection_digest(catalog) != self.publication["selection_sha256"]):
            raise InvalidArchive("Master manifest does not match the verified native/media publication")
        self.view.verify()
        intent_path = self.root / "commit-intent.json"
        intent = read_record(intent_path, limit=4096, optional=True)
        digest = hashlib.sha256(body).hexdigest()
        if intent is None:
            head = self.store.head("current_manifest.json")
            etag = head.get("ETag") if head is not None else None
            if head is not None and (not isinstance(etag, str) or not 1 <= len(etag) <= 1024
                                     or any(ord(c) < 32 or ord(c) > 126 for c in etag)):
                raise InvalidArchive("Current S3 manifest has no usable conditional-write identity")
            intent = {"master_sha256": digest, "bytes": len(body), "expected_etag": etag}
            publish_bytes(intent_path, json_bytes(intent))
        if (not isinstance(intent, dict) or set(intent) != {"master_sha256", "bytes", "expected_etag"}
                or intent["master_sha256"] != digest or intent["bytes"] != len(body)
                or intent["expected_etag"] is not None and not isinstance(intent["expected_etag"], str)):
            raise InvalidArchive("The retained manifest publication attempt changed")
        self.store.put_bytes("manifests/runs/" + self.run_id + "/manifest.json", body)
        same_or_publish(self.root / "prepared-master.json", body)

    def commit_master(self, body):
        with open_regular(self.root / "prepared-master.json") as incoming:
            if incoming.read(len(body) + 1) != body:
                raise InvalidArchive("Current manifest differs from the prepared publication")
        self.view.verify()
        # This mutable pointer is the final publication commit. Its checksum is
        # supplied to S3 and checked again before any cleanup becomes eligible.
        digest = hashlib.sha256(body).hexdigest()
        intent = read_record(self.root / "commit-intent.json", limit=4096)
        if intent.get("master_sha256") != digest or intent.get("bytes") != len(body):
            raise InvalidArchive("Current manifest differs from the original commit attempt")
        head = self.store.head("current_manifest.json")
        checksum = base64.b64encode(bytes.fromhex(digest)).decode("ascii")
        already_committed = head is not None and head.get("ChecksumSHA256") == checksum and head.get("ContentLength") == len(body)
        if already_committed:
            # An interrupted caller can adopt only its exact confirmed bytes.
            self.store.verified("current_manifest.json", digest, len(body))
            same_or_publish(self.root / "master.json", body)
            self.committed = True
            return
        expected = intent["expected_etag"]
        if (head.get("ETag") if head is not None else None) != expected:
            raise InvalidArchive("The current S3 manifest changed; refusing to replace a newer publication")
        condition = {"IfNoneMatch": "*"} if expected is None else {"IfMatch": expected}
        try:
            self.store.client.put_object(Bucket=self.store.bucket, Key=self.store.prefix + "current_manifest.json",
                                         Body=body, ContentLength=len(body), StorageClass="STANDARD",
                                         ChecksumSHA256=checksum, **condition)
        except Exception:
            if not self.store.verified("current_manifest.json", digest, len(body)):
                raise
        if not self.store.verified("current_manifest.json", digest, len(body)):
            raise InvalidArchive("Current native manifest could not be verified")
        same_or_publish(self.root / "master.json", body)
        self.committed = True

    def finish(self):
        with open_regular(self.root / "master.json") as incoming:
            body = incoming.read((128 << 20) + 1)
        if len(body) > 128 << 20 or decode_json(body).get("native_archive") != self.publication:
            raise InvalidArchive("Cannot release an unrelated native backup")
        key = "manifests/runs/" + self.run_id + "/manifest.json"
        if not self.store.verified(key, hashlib.sha256(body).hexdigest(), len(body)):
            raise InvalidArchive("Cannot release an unpublished native backup")
        released = self.root / "released.json"
        release_body = json_bytes({"master_sha256": hashlib.sha256(body).hexdigest(), "publication": self.publication})
        if not released.exists():
            release_published_artwork(self.archive, self.client, self.pins)
            release_published_media(self.archive, self.client, self.media)
            release_published_components(self.archive, self.client, self.component_cache)
        same_or_publish(released, release_body)
        # All providers have durable release receipts. Reclaim only this run's
        # inventoried private copies, preserving manifests and unknown files.
        directory(self.archive, private=True)
        directory(self.archive / "objects", private=True)
        for name, size in archive_objects(self.archive, load_manifest(self.archive)).items():
            path = self.archive / "objects" / (name + ".gz")
            try:
                info = regular(path)
            except FileNotFoundError:
                continue
            if info.st_size != size:
                raise InvalidArchive("Released archive object changed before cleanup")
            path.unlink()
        sync_directory(self.archive / "objects")
        for entry in self.generated:
            path = self.root / entry["name"]
            try:
                with open_regular(path) as incoming:
                    if (os.fstat(incoming.fileno()).st_size != entry["bytes"]
                            or hashlib.file_digest(incoming, "sha256").hexdigest() != entry["sha256"]):
                        raise InvalidArchive("Released host ledger changed before cleanup")
            except FileNotFoundError:
                continue
            path.unlink()
        sync_directory(self.root)
        if hasattr(self, "journal"):
            self.journal.complete(decode_json(release_body))
