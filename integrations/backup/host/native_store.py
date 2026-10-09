"""Host-owned S3 Standard publication of content-addressed native archives.

S3 verifies the supplied SHA-256 on single-part PUT. New/unknown objects must
return that checksum on HEAD. Publication reuses prior verified upload receipts
when a fresh LIST reports the same object identity; audits bypass those receipts.
No credentials, cloud scheduling or cloud SDK enter the Stash application.
"""

import base64
from concurrent.futures import ThreadPoolExecutor
import hashlib
import io
import os
from pathlib import Path
import tempfile

from manifest_limits import MASTER_BYTES, MEDIA_BINDING_BYTES, INVENTORY_BYTES, TRANSFER_BYTES
from object_receipts import ObjectReceipts, inventory as list_objects
from media_objects import validate_inventory as validate_media_inventory
import native_tags

from stash_archive.bundle import SQLITE_ROLES, iter_artifacts, sqlite_evidence_binding, validate_manifest
from stash_archive.filesystem_boundary import canonical_uuid
from stash_archive.checkpoint_release import archived_boundary
from stash_archive.server_checkpoint import ServerCheckpoint
from stash_archive.storage import (HEX, MAX_MANIFEST, InvalidArchive, decode_json, json_bytes, load_manifest,
                                   open_regular, regular, require_space, sync_directory, write_artifact)

PREFIX = "native-archives/"
FORMAT = "org.notsafeforgit.stash.s3-native-archive"
SELECTION_FORMAT = "org.notsafeforgit.stash.s3-media-selection"
MEDIA_FORMAT = "org.notsafeforgit.stash.host-backup.media"


def read_artifact(source, entry, limit):
    if entry is None or entry["size"] > limit:
        raise InvalidArchive("Native publication is missing a bounded binding artifact")
    output = io.BytesIO()
    write_artifact(source, entry, output)
    return output.getvalue()


class ArchivedCheckpoint:
    """Validate archived server records without constructing any transport."""
    boundary = True
    validate = ServerCheckpoint.validate

    def __init__(self, request_id):
        self.request_id = request_id


def validate_selection_binding(source, checkpoint_uuid, selection_sha256):
    if (not canonical_uuid(checkpoint_uuid) or not isinstance(selection_sha256, str)
            or not HEX.fullmatch(selection_sha256)):
        raise InvalidArchive("Invalid native/media publication identity")
    boundary = archived_boundary(source, ArchivedCheckpoint(checkpoint_uuid))
    selected, filesystem = None, None
    for entry in iter_artifacts(source, load_manifest(source)):
        if (entry["role"], entry["name"]) == ("media_manifest", "s3-media.json"):
            selected = entry
        if (entry["role"], entry["name"]) == ("operating_state", "filesystem-boundary.json"):
            filesystem = entry
    media = decode_json(read_artifact(source, selected, MEDIA_BINDING_BYTES))
    fields = {"format", "version", "checkpoint_uuid", "filesystem_boundary_sha256", "selection_sha256", "selection"}
    if (not isinstance(media, dict) or set(media) != fields or media["format"] != MEDIA_FORMAT
            or type(media["version"]) is not int or media["version"] != 1
            or media["checkpoint_uuid"] != boundary["uuid"]
            or media["filesystem_boundary_sha256"] != filesystem["sha256"]
            or media["selection_sha256"] != selection_sha256
            or not isinstance(media["selection"], dict)
            or media["selection"].get("format") != SELECTION_FORMAT
            or type(media["selection"].get("version")) is not int
            or media["selection"]["version"] not in (1, 2)
            or media["selection"] != media_selection(media["selection"])
            or selection_digest(media["selection"]) != selection_sha256):
        raise InvalidArchive("Native backup and media selection do not share the same checkpoint")
    return media


def validate_proof(source, manifest, proof):
    manifest_hash = hashlib.sha256(json_bytes(manifest)).hexdigest()
    if (not isinstance(proof, dict) or proof.get("uuid") != manifest["uuid"]
            or proof.get("manifest_sha256") != manifest_hash or proof.get("contents_verified") is not True
            or proof.get("verification_method", "isolated-restore") not in ("isolated-restore", "streamed-contents", "captured-contents")
            or not isinstance(proof.get("ingestion_receipts"), dict)
            or proof["ingestion_receipts"].get("archive_uuid") != manifest["uuid"]
            or proof["ingestion_receipts"].get("manifest_sha256") != manifest_hash):
        raise InvalidArchive("Native publication requires matching content and producer validation proofs")
    databases = [entry for entry in iter_artifacts(source, manifest) if entry["role"] in SQLITE_ROLES]
    if "sqlite_snapshots" in proof:
        expected = sqlite_evidence_binding(manifest, databases)
        if proof["sqlite_snapshots"] != expected:
            raise InvalidArchive("SQLite verification does not match every archived database")
    elif proof.get("verification_method") == "captured-contents" or "native_snapshot" not in proof:
        raise InvalidArchive("Native publication requires SQLite verification")
    # Historical publications carry only the full native audit. Keep those
    # readable, and never ignore an invalid audit attached to a newer proof.
    entries = [entry for entry in databases if entry["role"] in ("library", "producer_outbox")]
    library, = [entry for entry in entries if entry["role"] == "library"]
    if "native_snapshot" in proof:
        native = proof["native_snapshot"]
        if (not isinstance(native, dict) or native.get("database_verified") is not True
                or native.get("archive_uuid") != manifest["uuid"] or native.get("manifest_sha256") != manifest_hash
                or native.get("component") != {"role": "library", "name": "library"}
                or native.get("sha256") != library["sha256"] or native.get("bytes") != library["size"]
                or native.get("schema_version") != library["sqlite"]["schema"]):
            raise InvalidArchive("Native audit does not verify the exact archived library")
    receipts = proof["ingestion_receipts"]
    expected = [{"role": entry["role"], "name": entry["name"], "sha256": entry["sha256"]} for entry in entries]
    if (receipts.get("registered_producers_complete") is not True
            or receipts.get("components") != expected):
        raise InvalidArchive("Publication proofs do not verify the exact archived library and producer components")


def archive_objects(source, manifest):
    objects = {}
    for entry in iter_artifacts(source, manifest):
        for chunk in entry["chunks"]:
            name, size = chunk["sha256"], chunk["encoded_size"]
            if name in objects and objects[name] != size:
                raise InvalidArchive("Native object inventory has inconsistent lengths")
            objects[name] = size
    return objects


def media_selection(catalog):
    """Exclude publication timestamps/references, avoiding a circular digest."""
    immutable = ((catalog.get("format") == "s3-log-backup" and catalog.get("version") == 4)
                 or (catalog.get("format") == SELECTION_FORMAT and catalog.get("version") == 2))
    if immutable:
        validate_media_inventory(catalog)
        return {"format": SELECTION_FORMAT, "version": 2,
                "videos": sorted(catalog["videos"], key=lambda item: item["path"]),
                "units": sorted(catalog["units"], key=lambda item: item["unit_id"]),
                "media_store": catalog["media_store"], "objects": catalog["objects"]}
    return {"format": SELECTION_FORMAT, "version": 1,
            "videos": sorted(catalog["videos"], key=lambda item: item["key"]),
            "units": sorted(catalog["units"], key=lambda item: item["unit_id"])}


def selection_digest(catalog):
    return hashlib.sha256(json_bytes(media_selection(catalog))).hexdigest()


def descriptor(key, body):
    return {"key": key, "sha256": hashlib.sha256(body).hexdigest(), "bytes": len(body)}


def validate_reference(reference):
    fields = {"format", "version", "checkpoint_uuid", "archive_uuid", "selection_sha256",
              "objects_prefix", "manifest", "inventory", "verification"}
    if (not isinstance(reference, dict) or set(reference) != fields or reference["format"] != FORMAT
            or type(reference["version"]) is not int or reference["version"] != 1
            or not canonical_uuid(reference["checkpoint_uuid"]) or not canonical_uuid(reference["archive_uuid"])
            or not isinstance(reference["selection_sha256"], str) or not HEX.fullmatch(reference["selection_sha256"])
            or reference["objects_prefix"] != PREFIX + "objects/"):
        raise InvalidArchive("Invalid native archive publication reference")
    base = PREFIX + "runs/" + reference["archive_uuid"] + "/"
    for name, filename in (("manifest", "manifest.json"), ("inventory", "artifacts.jsonl"), ("verification", "verification.json")):
        value = reference[name]
        limit = INVENTORY_BYTES if name == "inventory" else MAX_MANIFEST
        if (not isinstance(value, dict) or set(value) != {"key", "sha256", "bytes"}
                or value["key"] != base + filename or not isinstance(value["sha256"], str) or not HEX.fullmatch(value["sha256"])
                or type(value["bytes"]) is not int or not 0 < value["bytes"] <= limit):
            raise InvalidArchive("Invalid native archive object reference")
    return reference


class NativeStore:
    def __init__(self, client, bucket, prefix="", *, workers=16, receipts_path=None):
        if (not isinstance(bucket, str) or not bucket or "/" in bucket or ":" in bucket
                or not isinstance(prefix, str) or prefix.startswith("/")
                or any(part in (".", "..") for part in prefix.split("/"))
                or any(c in bucket + prefix for c in "\0\r\n") or type(workers) is not int or not 1 <= workers <= 64):
            raise InvalidArchive("Invalid host metadata bucket, prefix or upload concurrency")
        self.client, self.bucket = client, bucket
        self.prefix, self.workers = prefix.rstrip("/") + "/" if prefix else "", workers
        self.receipts_path = receipts_path

    def head(self, key):
        try:
            return self.client.head_object(Bucket=self.bucket, Key=self.prefix + key, ChecksumMode="ENABLED")
        except Exception as error:
            code = str(getattr(error, "response", {}).get("Error", {}).get("Code"))
            if code in {"404", "NoSuchKey", "NotFound"}:
                return None
            raise

    @staticmethod
    def check_head(head, sha256, size):
        expected = base64.b64encode(bytes.fromhex(sha256)).decode("ascii")
        if (head.get("ContentLength") != size or head.get("ChecksumSHA256") != expected
                or head.get("ChecksumType", "FULL_OBJECT") != "FULL_OBJECT"
                or head.get("StorageClass", "STANDARD") != "STANDARD"):
            raise InvalidArchive("S3 native object lacks its exact SHA-256, size or Standard storage class")

    def verified(self, key, sha256, size, *, receipts=None, listed=None, tag_state=None):
        if receipts is not None and listed is not None:
            full_key = self.prefix + key
            if full_key not in listed:
                return False
            if receipts.matches(full_key, sha256, size, listed[full_key]):
                return True
        head = self.head(key)
        if head is None:
            return False
        self.check_head(head, sha256, size)
        if receipts is not None:
            receipts.remember(self.prefix + key, sha256, head, tag_state=tag_state)
        return True

    def put(self, key, body, sha256, size, *, receipts=None, listed=None):
        managed = native_tags.MANAGED.fullmatch(key) is not None
        if self.verified(key, sha256, size, receipts=receipts, listed=listed):
            if not managed or native_tags.reconcile(self, key, sha256, size, "live", receipts=receipts, listed=listed):
                return
        try:
            self.client.put_object(Bucket=self.bucket, Key=self.prefix + key, Body=body,
                                   ContentLength=size, StorageClass="STANDARD", IfNoneMatch="*",
                                   ChecksumSHA256=base64.b64encode(bytes.fromhex(sha256)).decode("ascii"))
        except Exception:
            # A lost reply or a competing identical immutable upload may already
            # have completed. Only independently checked durable bytes suffice.
            if not self.verified(key, sha256, size, receipts=receipts):
                raise
            if managed and not native_tags.reconcile(self, key, sha256, size, "live", receipts=receipts):
                raise InvalidArchive("Native object expired while recovering its publication")
            return
        # A successful new PUT has no retirement tag. It still needs the full
        # checksum HEAD; only then may future unchanged LISTs reuse that fact.
        if not self.verified(key, sha256, size, receipts=receipts, tag_state="live" if managed else None):
            raise InvalidArchive("Uploaded native object is unavailable for checksum verification")

    def put_file(self, key, path, expected_sha256, expected_size, *, receipts=None, listed=None):
        # The retained remote object already supplies these exact bytes. Reuse
        # its checksum evidence before opening an unnecessary local upload copy.
        # Unknown/changed objects still follow the complete input validation path.
        if (receipts is not None and listed is not None
                and receipts.matches(self.prefix + key, expected_sha256, expected_size, listed.get(self.prefix + key))):
            if (not native_tags.MANAGED.fullmatch(key)
                    or native_tags.reconcile(self, key, expected_sha256, expected_size, "live",
                                             receipts=receipts, listed=listed)):
                return
        with open_regular(path) as incoming:
            if regular(path).st_size != expected_size or hashlib.file_digest(incoming, "sha256").hexdigest() != expected_sha256:
                raise InvalidArchive("Native publication input differs from its declared bytes")
            incoming.seek(0)
            self.put(key, incoming, expected_sha256, expected_size, receipts=receipts, listed=listed)

    def put_bytes(self, key, body):
        value = descriptor(key, body)
        self.put(key, io.BytesIO(body), value["sha256"], value["bytes"])
        return value

    def validate_read(self, value, limit):
        if (not isinstance(value, dict) or set(value) != {"key", "sha256", "bytes"}
                or not isinstance(value["key"], str) or not value["key"]
                or any(part in ("", ".", "..") for part in value["key"].split("/"))
                or any(c in value["key"] for c in "\0\r\n")
                or not isinstance(value["sha256"], str) or not HEX.fullmatch(value["sha256"])
                or type(value["bytes"]) is not int or not 0 <= value["bytes"] <= limit):
            raise InvalidArchive("Invalid or oversized native object read descriptor")

    def read(self, value):
        self.validate_read(value, MASTER_BYTES)
        if not self.verified(value["key"], value["sha256"], value["bytes"]):
            raise InvalidArchive("Required native object is missing from S3")
        response = self.client.get_object(Bucket=self.bucket, Key=self.prefix + value["key"], ChecksumMode="ENABLED")
        with response["Body"] as incoming:
            body = incoming.read(value["bytes"] + 1)
        if len(body) != value["bytes"] or hashlib.sha256(body).hexdigest() != value["sha256"]:
            raise InvalidArchive("Downloaded native object differs from its publication digest")
        return body

    def download_file(self, value, destination, *, reserve=50 << 30, limit=INVENTORY_BYTES):
        """Verify large metadata/encoded objects without loading them in RAM.

        Publish a new local file only after its complete digest matches. Failed
        and truncated transfers remove their temporary file; existing outputs
        and unrelated files are never replaced.
        """
        self.validate_read(value, limit)
        destination = Path(destination)
        require_space(destination.parent, value["bytes"], reserve)
        if destination.exists() or destination.is_symlink():
            raise InvalidArchive("Native download destination already exists")
        if not self.verified(value["key"], value["sha256"], value["bytes"]):
            raise InvalidArchive("Required native object is missing from S3")
        fd, temporary = tempfile.mkstemp(prefix=".native-download-", dir=destination.parent)
        try:
            digest, remaining = hashlib.sha256(), value["bytes"]
            with os.fdopen(fd, "wb") as output:
                response = self.client.get_object(Bucket=self.bucket, Key=self.prefix + value["key"], ChecksumMode="ENABLED")
                with response["Body"] as incoming:
                    while remaining:
                        block = incoming.read(min(TRANSFER_BYTES, remaining))
                        if not block or len(block) > remaining:
                            raise InvalidArchive("Downloaded native object has an incorrect length")
                        require_space(destination.parent, len(block), reserve)
                        digest.update(block)
                        output.write(block)
                        remaining -= len(block)
                    if incoming.read(1) or digest.hexdigest() != value["sha256"]:
                        raise InvalidArchive("Downloaded native object differs from its publication digest")
                output.flush()
                os.fsync(output.fileno())
            os.link(temporary, destination, follow_symlinks=False)
            sync_directory(destination.parent)
        finally:
            Path(temporary).unlink(missing_ok=True)

    def fetch_metadata(self, reference, destination, *, reserve=50 << 30):
        reference = validate_reference(reference)
        destination = Path(destination)
        for name, filename in (("manifest", "manifest.json"), ("inventory", "artifacts.jsonl"), ("verification", "verification.json")):
            self.download_file(reference[name], destination / filename, reserve=reserve,
                               limit=INVENTORY_BYTES if name == "inventory" else MAX_MANIFEST)
        manifest = validate_manifest(load_manifest(destination))
        if (manifest["uuid"] != reference["archive_uuid"]
                or (manifest["inventory"]["sha256"], manifest["inventory"]["size"])
                != (reference["inventory"]["sha256"], reference["inventory"]["bytes"])):
            raise InvalidArchive("Remote native inventory differs from its publication reference")
        validate_proof(destination, manifest, decode_json((destination / "verification.json").read_bytes()))
        return manifest, archive_objects(destination, manifest)

    def audit(self, reference, *, reserve=50 << 30):
        """Check metadata and HEAD every native object; no cold-media requests."""
        with tempfile.TemporaryDirectory(prefix="stash-native-audit-") as temp:
            manifest, objects = self.fetch_metadata(reference, temp, reserve=reserve)
            def check(item):
                name, size = item
                if not self.verified(PREFIX + "objects/" + name + ".gz", name, size):
                    raise InvalidArchive("Published native object is missing")
            with ThreadPoolExecutor(max_workers=self.workers) as pool:
                for _ in pool.map(check, objects.items()):
                    pass
            return {"archive_uuid": manifest["uuid"], "objects_verified": len(objects),
                    "encoded_bytes": sum(objects.values()), "coverage": "remote-object-checksums"}

    def download(self, reference, destination, *, reserve=50 << 30):
        """Fetch into a new, unactivated archive directory, checking every byte."""
        validate_reference(reference)
        destination = Path(destination)
        require_space(destination.parent, 0, reserve)
        destination.mkdir(mode=0o700)
        manifest, objects = self.fetch_metadata(reference, destination, reserve=reserve)
        (destination / "objects").mkdir(mode=0o700)
        # Sequential writes keep disk-space checks meaningful at the reserve.
        for name, size in objects.items():
            self.download_file({"key": PREFIX + "objects/" + name + ".gz", "sha256": name, "bytes": size},
                               destination / "objects" / (name + ".gz"), reserve=reserve, limit=MASTER_BYTES)
        validate_selection_binding(destination, reference["checkpoint_uuid"], reference["selection_sha256"])
        return manifest

    def publish_archive(self, source, proof, checkpoint_uuid, selection_sha256):
        """Require content, SQLite and producer proofs bound to this bundle."""
        source = Path(source)
        manifest = validate_manifest(load_manifest(source))
        manifest_body = (source / "manifest.json").read_bytes()
        # Portable manifests use this canonical representation. A different
        # representation cannot inherit the proof's digest by JSON equivalence.
        if manifest_body != json_bytes(manifest):
            raise InvalidArchive("Native manifest bytes differ from the verified canonical representation")
        validate_proof(source, manifest, proof)
        validate_selection_binding(source, checkpoint_uuid, selection_sha256)
        objects = archive_objects(source, manifest)
        base = PREFIX + "runs/" + manifest["uuid"] + "/"
        inventory, proof_body = manifest["inventory"], json_bytes(proof)
        reference = validate_reference({
            "format": FORMAT, "version": 1, "archive_uuid": manifest["uuid"],
            "checkpoint_uuid": checkpoint_uuid, "selection_sha256": selection_sha256,
            "objects_prefix": PREFIX + "objects/", "manifest": descriptor(base + "manifest.json", manifest_body),
            "inventory": {"key": base + "artifacts.jsonl", "sha256": inventory["sha256"], "bytes": inventory["size"]},
            "verification": descriptor(base + "verification.json", proof_body)})
        receipts = ObjectReceipts(self.receipts_path, self.bucket) if self.receipts_path is not None else None
        try:
            listed = list_objects(self.client, self.bucket, self.prefix + PREFIX + "objects/") if receipts is not None else None
            def upload(item):
                name, size = item
                self.put_file(PREFIX + "objects/" + name + ".gz", source / "objects" / (name + ".gz"), name, size,
                              receipts=receipts, listed=listed)
            with ThreadPoolExecutor(max_workers=self.workers) as pool:
                for _ in pool.map(upload, objects.items()):
                    pass
        finally:
            if receipts is not None:
                receipts.close()
        self.put_file(base + "artifacts.jsonl", source / "artifacts.jsonl", inventory["sha256"], inventory["size"])
        self.put_bytes(base + "verification.json", proof_body)
        self.put_bytes(base + "manifest.json", manifest_body)
        return reference
