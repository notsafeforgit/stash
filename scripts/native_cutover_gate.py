"""Inspect completed backup receipts independently of a later restore drill.

This operator-side helper performs local reads only. It accepts the publisher's
durable completion record, which is written after checksum-verified cloud
publication and provider release. It does not claim that a cloud restore passed.
"""

import hashlib
import json
from pathlib import Path
import re
import stat


class GatePending(ValueError):
    """The selected publication or restore has not completed yet."""


def read_bytes(path, limit):
    path = Path(path)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_size > limit:
        raise ValueError("Expected a bounded regular receipt: " + str(path))
    body = path.read_bytes()
    if len(body) > limit:
        raise ValueError("Receipt exceeded its size bound")
    return body


def read_json(path, limit=65536):
    return json.loads(read_bytes(path, limit))


def digest(body):
    return hashlib.sha256(body).hexdigest()


def publication_evidence(run, expected):
    """Bind one completed run to the operator's selected immutable identity."""
    run = Path(run)
    if set(expected) != {"run_id", "checkpoint_uuid", "config_sha256"}:
        raise ValueError("Select an exact run, checkpoint and configuration")
    if run.name != expected["run_id"]:
        raise ValueError("The selected run directory changed")
    identity = read_json(run / "identity.json")
    if any(identity.get(key) != value for key, value in expected.items()):
        raise ValueError("The backup differs from the selected identity")
    if (run / "abandoned.json").exists():
        raise ValueError("An abandoned backup cannot authorize rollout")
    if not (run / "finished.json").exists():
        raise GatePending("The backup publisher has not completed")
    finished = read_json(run / "finished.json")
    released = read_json(run / "released.json")
    publication = read_json(run / "publication.json")
    master_body = read_bytes(run / "master.json", 512 << 20)
    master = json.loads(master_body)
    master_sha256 = digest(master_body)
    if (set(finished) != {"master_sha256", "publication"}
            or finished != released
            or finished["master_sha256"] != master_sha256
            or finished["publication"] != publication
            or master.get("native_archive") != publication
            or master.get("run_id") != expected["run_id"]
            or publication.get("checkpoint_uuid") != expected["checkpoint_uuid"]):
        raise ValueError("Backup completion and publication receipts disagree")
    archive_manifest = read_bytes(run / "archive/manifest.json", 16 << 20)
    manifest = json.loads(archive_manifest)
    if (manifest.get("uuid") != publication.get("archive_uuid")
            or publication.get("manifest", {}).get("sha256") != digest(archive_manifest)
            or publication.get("manifest", {}).get("bytes") != len(archive_manifest)
            or publication.get("inventory", {}).get("sha256") != manifest.get("inventory", {}).get("sha256")
            or publication.get("inventory", {}).get("bytes") != manifest.get("inventory", {}).get("size")):
        raise ValueError("The published archive identity differs from its manifest")
    inventory = run / "archive/artifacts.jsonl"
    if not stat.S_ISREG(inventory.lstat().st_mode):
        raise ValueError("The archive inventory must be a regular file")
    with inventory.open("rb") as incoming:
        inventory_sha256 = hashlib.file_digest(incoming, "sha256").hexdigest()
    if (inventory.stat().st_size != publication["inventory"]["bytes"]
            or inventory_sha256 != publication["inventory"]["sha256"]):
        raise ValueError("The retained archive inventory changed")
    for descriptor in (publication["manifest"], publication["inventory"], publication.get("verification", {})):
        if (not isinstance(descriptor.get("key"), str)
                or not re.fullmatch(r"[0-9a-f]{64}", str(descriptor.get("sha256")))
                or type(descriptor.get("bytes")) is not int or descriptor["bytes"] <= 0):
            raise ValueError("Incomplete cloud publication reference")
    return {"status": "passed", "gate": "backup_publication", **expected,
            "master_manifest": str(run / "master.json"), "master_sha256": master_sha256,
            "publication": publication, "coordinated_backup_complete": True,
            "restore_verification_required": True,
            "receipts": {name: digest(read_bytes(run / name, 65536))
                         for name in ("identity.json", "finished.json", "released.json", "publication.json")}}


def check_controller_publication(controller, evidence, restore_stage, completed_stage):
    """Wait until the old controller has passed its final pre-restore hold check.

    A subsequent restore failure cannot undo an already confirmed publication.
    No overall controller status is rewritten or treated as restore success.
    """
    if controller.get("coordinated_backup_complete") is not True:
        raise GatePending("The backup controller has not confirmed publication")
    for key in ("run_id", "checkpoint_uuid", "master_manifest", "master_sha256"):
        if controller.get(key) != evidence[key]:
            raise ValueError("Controller publication does not match the selected run")
    if controller.get("stage") not in (restore_stage, completed_stage):
        raise GatePending("The controller has not passed its pre-restore hold check")
    if controller["stage"] == restore_stage and controller.get("child") is None:
        if type(controller.get("child_exit_code")) is not int:
            raise GatePending("Restore startup has not been recorded yet")
    if controller["stage"] == completed_stage and controller.get("isolated_restore_complete") is not True:
        raise ValueError("Incomplete controller success record")


def check_restore_finished(controller, restore_stage, completed_stage):
    """Require an exited, successful restore child; stdout alone is insufficient."""
    if controller.get("stage") not in (restore_stage, completed_stage):
        raise GatePending("The isolated restore has not run")
    if controller.get("child") is not None or controller.get("status") == "running":
        raise GatePending("The isolated restore controller is still active")
    if type(controller.get("child_exit_code")) is not int or controller["child_exit_code"] != 0:
        raise ValueError("The isolated restore command failed or has no exit receipt")
