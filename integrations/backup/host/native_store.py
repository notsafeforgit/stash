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
from pathlib import Path
import tempfile

from object_receipts import ObjectReceipts, inventory as list_objects

from stash_archive.bundle import iter_artifacts, validate_manifest
from stash_archive.filesystem_boundary import canonical_uuid
from stash_archive.checkpoint_release import archived_boundary
from stash_archive.server_checkpoint import ServerCheckpoint
from stash_archive.storage import (HEX, InvalidArchive, decode_json, json_bytes, load_manifest,
                                   open_regular, regular, publish_bytes, require_space, write_artifact)

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
    media = decode_json(read_artifact(source, selected, 128 << 20))
    fields = {"format", "version", "checkpoint_uuid", "filesystem_boundary_sha256", "selection_sha256", "selection"}
    if (not isinstance(media, dict) or set(media) != fields or media["format"] != MEDIA_FORMAT
            or type(media["version"]) is not int or media["version"] != 1
            or media["checkpoint_uuid"] != boundary["uuid"]
            or media["filesystem_boundary_sha256"] != filesystem["sha256"]
            or media["selection_sha256"] != selection_sha256
            or not isinstance(media["selection"], dict)
            or set(media["selection"]) != {"format", "version", "videos", "units"}
            or media["selection"] != media_selection(media["selection"])
            or selection_digest(media["selection"]) != selection_sha256):
        raise InvalidArchive("Native backup and media selection do not share the same checkpoint")
    return media


def validate_proof(source, manifest, proof):
    manifest_hash = hashlib.sha256(json_bytes(manifest)).hexdigest()
    if (not isinstance(proof, dict) or proof.get("uuid") != manifest["uuid"]
            or proof.get("manifest_sha256") != manifest_hash or proof.get("contents_verified") is not True
            or not isinstance(proof.get("native_snapshot"), dict) or not isinstance(proof.get("ingestion_receipts"), dict)
            or proof["native_snapshot"].get("database_verified") is not True
            or any(proof[k].get("archive_uuid") != manifest["uuid"] or proof[k].get("manifest_sha256") != manifest_hash
                   for k in ("native_snapshot", "ingestion_receipts"))):
        raise InvalidArchive("Native publication requires matching native and producer validation proofs")
    entries = [entry for entry in iter_artifacts(source, manifest) if entry["role"] in ("library", "producer_outbox")]
    library, = [entry for entry in entries if entry["role"] == "library"]
    native, receipts = proof["native_snapshot"], proof["ingestion_receipts"]
    expected = [{"role": entry["role"], "name": entry["name"], "sha256": entry["sha256"]} for entry in entries]
    if (native.get("component") != {"role": "library", "name": "library"}
            or native.get("sha256") != library["sha256"] or native.get("bytes") != library["size"]
            or native.get("schema_version") != library["sqlite"]["schema"]
            or receipts.get("registered_producers_complete") is not True
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
        if (not isinstance(value, dict) or set(value) != {"key", "sha256", "bytes"}
                or value["key"] != base + filename or not isinstance(value["sha256"], str) or not HEX.fullmatch(value["sha256"])
                or type(value["bytes"]) is not int or not 0 < value["bytes"] <= 128 << 20):
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

    def verified(self, key, sha256, size, *, receipts=None, listed=None):
        if receipts is not None and listed is not None:
            full_key = self.prefix + key
            if full_key not in listed:
                return False
            if receipts.matches(full_key, sha256, size, listed[full_key]):
                return True
        head = self.head(key)
        if head is None:
            return False
        expected = base64.b64encode(bytes.fromhex(sha256)).decode("ascii")
        if (head.get("ContentLength") != size or head.get("ChecksumSHA256") != expected
                or head.get("ChecksumType", "FULL_OBJECT") != "FULL_OBJECT"
                or head.get("StorageClass", "STANDARD") != "STANDARD"):
            raise InvalidArchive("S3 native object lacks its exact SHA-256, size or Standard storage class")
        if receipts is not None:
            receipts.remember(self.prefix + key, sha256, head)
        return True

    def put(self, key, body, sha256, size, *, receipts=None, listed=None):
        if self.verified(key, sha256, size, receipts=receipts, listed=listed):
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
            return
        if not self.verified(key, sha256, size, receipts=receipts):
            raise InvalidArchive("Uploaded native object is unavailable for checksum verification")

    def put_file(self, key, path, expected_sha256, expected_size, *, receipts=None, listed=None):
        with open_regular(path) as incoming:
            if regular(path).st_size != expected_size or hashlib.file_digest(incoming, "sha256").hexdigest() != expected_sha256:
                raise InvalidArchive("Native publication input differs from its declared bytes")
            incoming.seek(0)
            self.put(key, incoming, expected_sha256, expected_size, receipts=receipts, listed=listed)

    def put_bytes(self, key, body):
        value = descriptor(key, body)
        self.put(key, io.BytesIO(body), value["sha256"], value["bytes"])
        return value

    def read(self, value):
        if not self.verified(value["key"], value["sha256"], value["bytes"]):
            raise InvalidArchive("Required native object is missing from S3")
        response = self.client.get_object(Bucket=self.bucket, Key=self.prefix + value["key"], ChecksumMode="ENABLED")
        with response["Body"] as incoming:
            body = incoming.read(value["bytes"] + 1)
        if len(body) != value["bytes"] or hashlib.sha256(body).hexdigest() != value["sha256"]:
            raise InvalidArchive("Downloaded native object differs from its publication digest")
        return body

    def fetch_metadata(self, reference, destination, *, reserve=50 << 30):
        reference = validate_reference(reference)
        destination = Path(destination)
        for name, filename in (("manifest", "manifest.json"), ("inventory", "artifacts.jsonl"), ("verification", "verification.json")):
            require_space(destination, reference[name]["bytes"], reserve)
            publish_bytes(destination / filename, self.read(reference[name]))
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
            require_space(destination, size, reserve)
            body = self.read({"key": PREFIX + "objects/" + name + ".gz", "sha256": name, "bytes": size})
            publish_bytes(destination / "objects" / (name + ".gz"), body)
        validate_selection_binding(destination, reference["checkpoint_uuid"], reference["selection_sha256"])
        return manifest

    def publish_archive(self, source, proof, checkpoint_uuid, selection_sha256):
        """The host must obtain proof from verify_archive_proofs on this bundle."""
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
