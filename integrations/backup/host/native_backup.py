"""Daily host publisher coordination; installed only at native cutover."""

from contextlib import closing
import base64
import hashlib
import os
from pathlib import Path
import re
import sqlite3
import tempfile
import time
import uuid

from stash_archive.artwork_pins import ArtworkPins, directory, release_published_artwork
from stash_archive.bundle import export_archive
from stash_archive.component_stage import release_published_components
from stash_archive.filesystem_boundary import canonical_uuid
from stash_archive.host_boundary import HostFilesystemCapture
from stash_archive.storage import (InvalidArchive, decode_json, json_bytes, load_manifest, open_regular,
                                   publish_bytes, regular, require_space, sync_directory)
from stash_archive.verification import verify_archive_proofs, validator_path
from stash_archive.zfs_media import ZFSMedia, release_published_media

from native_store import (MEDIA_FORMAT, NativeStore, archive_objects, media_selection, selection_digest,
                          validate_selection_binding)

CONFIG_FORMAT = "org.notsafeforgit.stash.host-backup"


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
    def __init__(self, filename, run_id, live_media, bucket, prefix, s3_client):
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,127}", run_id):
            raise InvalidArchive("Invalid host backup run identity")
        filename = Path(filename).resolve(strict=True)
        with open_regular(filename) as incoming:
            body = incoming.read((4 << 20) + 1)
        if len(body) > 4 << 20:
            raise InvalidArchive("Host backup configuration exceeds its size limit")
        config = decode_json(body)
        required = {"format", "version", "server", "api_key_file", "state_directory", "artwork_sources", "media",
                    "worker_lock_roots", "components", "recovery_roots", "producer_origin", "native_validator"}
        optional = {"reserve_bytes", "boundary_timeout", "validator_timeout", "zfs_command"}
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
        self.store = NativeStore(s3_client, bucket, prefix)
        state = private_directory(config["state_directory"])
        self.run_id, self.root = run_id, private_directory(state / "runs" / run_id)
        identity_path = self.root / "identity.json"
        identity = {"format": CONFIG_FORMAT + ".run", "version": 1, "run_id": run_id,
                    "config_sha256": hashlib.sha256(body).hexdigest(), "live_media": str(self.live_media),
                    "bucket": bucket, "prefix": prefix}
        if identity_path.exists():
            with open_regular(identity_path) as incoming:
                saved = decode_json(incoming.read(16385))
            if (not isinstance(saved, dict) or set(saved) != set(identity) | {"checkpoint_uuid", "created_epoch"}
                    or any(saved[k] != v for k, v in identity.items())):
                raise InvalidArchive("Host backup retry changed its original configuration or destination")
        else:
            saved = {**identity, "checkpoint_uuid": str(uuid.uuid4()), "created_epoch": int(time.time())}
            publish_bytes(identity_path, json_bytes(saved))
        if (not canonical_uuid(saved["checkpoint_uuid"]) or type(saved["created_epoch"]) is not int
                or saved["created_epoch"] <= 0):
            raise InvalidArchive("Invalid retained native backup identity")
        self.created_epoch = saved["created_epoch"]
        pins_cache, media_cache, component_cache = [private_directory(state / name) for name in ("artwork", "media", "components")]
        self.pins = ArtworkPins(pins_cache, config["artwork_sources"], reserve=self.reserve)
        self.media = ZFSMedia(media_cache, media["dataset"], media["guid"], media["mountpoint"],
                             command=config.get("zfs_command", ["/usr/sbin/zfs"]), reserve=self.reserve)
        self.component_cache = component_cache
        api_file = Path(config["api_key_file"]).resolve(strict=True)
        with open_regular(api_file) as incoming:
            key = incoming.read(8194).decode("ascii").strip()
        components = list(config["components"]) + [
            {"role": "config", "name": "host-backup.json", "path": filename},
            {"role": "config", "name": "host-backup-api-key", "path": api_file}]
        with HostFilesystemCapture(config["worker_lock_roots"], self.pins, self.media) as capture:
            self.client = capture.client(config["server"], key, saved["checkpoint_uuid"], config["recovery_roots"],
                                         boundary_timeout=config.get("boundary_timeout", 120))
            self.stage = capture.prepare(component_cache, self.client, components, reserve=self.reserve)
            self.stage.seal()
        self.view = self.media.open_bound(self.client.boundary_receipt).verify()
        self.media_path = self.view.resolve(relative)
        self.view_identity = (self.view.root.stat().st_dev, self.view.root.stat().st_ino)
        self.publication = None
        self.committed = False

    def check_source(self):
        info = self.view.root.stat()
        if ((info.st_dev, info.st_ino) != self.view_identity
                or not os.statvfs(self.view.root).f_flag & os.ST_RDONLY
                or not self.media_path.is_dir() or self.media_path.stat().st_dev != info.st_dev):
            raise InvalidArchive("Retained native media view became unavailable or changed")

    def publish(self, catalog, ledger_paths):
        self.view.verify()
        self.check_source()
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
            export_archive(None, self.archive, components=generated, producer_origin=self.producer_origin,
                           server_checkpoint=self.client, artwork_pins=self.pins, media_snapshot=self.media,
                           component_stage=self.stage, reserve=self.reserve)
        media = validate_selection_binding(self.archive, self.client.request_id, selection)
        if media != media_document:
            raise InvalidArchive("Retained archive differs from the original native/media selection")
        proof = verify_archive_proofs(self.archive, native_validator=self.validator, producer_origin=self.producer_origin,
                                      timeout=self.validator_timeout, temp_parent=self.root, reserve=self.reserve)
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
        try:
            self.store.client.put_object(Bucket=self.store.bucket, Key=self.store.prefix + "current_manifest.json",
                                         Body=body, ContentLength=len(body), StorageClass="STANDARD",
                                         ChecksumSHA256=base64.b64encode(bytes.fromhex(digest)).decode("ascii"))
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
