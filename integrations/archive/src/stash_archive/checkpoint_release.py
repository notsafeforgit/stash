"""Bind release of temporary server components to an enclosing portable archive.

These checks establish the binding, not remote publication. Only the authorized
publisher may call release, after verifying its durable archive publication.
Export/download alone must never release the server's retryable checkpoint.
"""

import hashlib
import io
from pathlib import Path

from .bundle import iter_artifacts, validate_manifest
from .server_checkpoint import request_bytes
from .storage import InvalidArchive, decode_json, json_bytes, load_manifest, write_artifact

RELEASE_FORMAT = "org.notsafeforgit.stash.server-checkpoint-release"
CHECKPOINT_FIELDS = ("format", "version", "uuid", "coverage", "created_at", "request_sha256",
                     "source_database_path", "source_config_path", "source_working_directory",
                     "committed_deletion_ids", "components")
COMPONENT_FIELDS = ("role", "name", "bytes", "sha256", "source_path")


def checkpoint_digest(checkpoint):
    """Digest the exact Go checkpoint representation, independent of JSON order."""
    ordered = {k: checkpoint[k] for k in CHECKPOINT_FIELDS}
    ordered["components"] = [{k: entry[k] for k in COMPONENT_FIELDS if k in entry}
                             for entry in checkpoint["components"]]
    return hashlib.sha256(request_bytes(ordered)).hexdigest()


def archive_binding(source, client):
    """Require the complete inventory and every checkpoint component binding.

    Do not make another full library copy. The enclosing publisher owns content,
    native/producer validation, durability, and remote readback before release.
    """
    source = Path(source)
    archive = validate_manifest(load_manifest(source))
    entries, checkpoint_entry = {}, None
    for entry in iter_artifacts(source, archive):
        if entry["role"] in ("library", "config", "file_journal", "operating_state"):
            entries[(entry["role"], entry["name"])] = entry
        if (entry["role"], entry["name"]) == ("operating_state", "server-checkpoint.json"):
            checkpoint_entry = entry
    if checkpoint_entry is None or checkpoint_entry["size"] > 1 << 20:
        raise InvalidArchive("Archive has no bounded server checkpoint component")
    output = io.BytesIO()
    write_artifact(source, checkpoint_entry, output)
    checkpoint = decode_json(output.getvalue())
    # The original reserve was part of the server request, not a release option.
    # Validate the archived manifest against its captured request digest and the
    # explicitly selected checkpoint UUID without re-requesting a live capture.
    if not isinstance(checkpoint, dict) or "request_sha256" not in checkpoint:
        raise InvalidArchive("Invalid archived server checkpoint")
    client.validate(checkpoint, checkpoint["request_sha256"])
    for component in checkpoint["components"]:
        name = "library" if component["role"] == "library" else component["name"]
        entry = entries.get((component["role"], name))
        if entry is None or (entry["sha256"], entry["size"]) != (component["sha256"], component["bytes"]):
            raise InvalidArchive("Archive does not contain the exact server checkpoint components")
    return checkpoint, {"checkpoint_sha256": checkpoint_digest(checkpoint),
                        "archive_uuid": archive["uuid"],
                        "archive_manifest_sha256": hashlib.sha256(json_bytes(archive)).hexdigest()}


def release_published_checkpoint(source, client):
    """Called only after the publisher verifies durable enclosing publication.

    The server retains this archive binding permanently before unlinking its
    large temporary components. Retrying this call completes interrupted cleanup.
    It makes no remote-storage or complete-production-coverage claim itself.
    """
    checkpoint, binding = archive_binding(source, client)
    with client.open(f"/{client.request_id}/release", request_bytes(binding)) as response:
        if response.headers.get_content_type() != "application/json":
            raise InvalidArchive("Checkpoint release response is not JSON")
        body = response.read(4097)
    if len(body) > 4096:
        raise InvalidArchive("Checkpoint release receipt exceeds its size limit")
    receipt = decode_json(body)
    fields = {"format", "version", "uuid", "request_sha256", "archive", "released_at"}
    if (not isinstance(receipt, dict) or set(receipt) != fields or receipt["format"] != RELEASE_FORMAT
            or type(receipt["version"]) is not int or receipt["version"] != 1
            or receipt["uuid"] != client.request_id or receipt["request_sha256"] != checkpoint["request_sha256"]
            or receipt["archive"] != binding or not isinstance(receipt["released_at"], str)):
        raise InvalidArchive("Checkpoint release receipt does not match the published archive")
    return receipt
