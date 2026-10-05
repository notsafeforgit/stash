"""Bind optional native and producer checks to one isolated verified restore.

The executable is an explicit, trusted local input, never selected by an archive.
No server, migration, worker or filesystem recovery is started by this protocol.
This still does not establish a coordinated live backup/media boundary.
"""

import hashlib
import math
import os
from pathlib import Path
import selectors
import stat
import subprocess
import tempfile
import time

from .bundle import FORMAT, import_archive, iter_artifacts
from .storage import InvalidArchive, RESERVE_BYTES, decode_json, json_bytes

MAX_OUTPUT = 64 << 10
DEFAULT_TIMEOUT = 3600


def validator_output(executable, database, timeout):
    """Bound both output streams and runtime, including a child that stops talking."""
    outputs = {"stdout": bytearray(), "stderr": bytearray()}
    deadline = time.monotonic() + timeout
    with subprocess.Popen([str(executable), "--verify-native-snapshot", str(database)],
                          stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, cwd=database.parent) as process:
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ, "stdout")
                selector.register(process.stderr, selectors.EVENT_READ, "stderr")
                while selector.get_map():
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise InvalidArchive("Native snapshot validation timed out")
                    for key, _ in selector.select(min(remaining, 0.25)):
                        data = os.read(key.fileobj.fileno(), 8192)
                        if not data:
                            selector.unregister(key.fileobj)
                            continue
                        output = outputs[key.data]
                        if len(output) + len(data) > MAX_OUTPUT:
                            raise InvalidArchive("Native snapshot validator exceeded its output limit")
                        output.extend(data)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise InvalidArchive("Native snapshot validation timed out")
            try:
                process.wait(timeout=remaining)
            except subprocess.TimeoutExpired as error:
                raise InvalidArchive("Native snapshot validation timed out") from error
            if process.returncode != 0:
                detail = outputs["stderr"].decode("utf-8", errors="replace").strip()
                raise InvalidArchive(f"Native snapshot validation failed (exit {process.returncode}): {detail[:4096]}")
        except BaseException:
            if process.poll() is None:
                process.kill()
            process.wait()
            raise
    return bytes(outputs["stdout"])


def validator_path(executable, timeout):
    """Reject invalid local options before allocating a potentially large restore."""
    if not isinstance(executable, (str, os.PathLike)) or not os.fspath(executable):
        raise InvalidArchive("Native validation requires an explicit local executable path")
    if type(timeout) not in (int, float) or not math.isfinite(timeout) or timeout <= 0:
        raise InvalidArchive("Native validation timeout must be positive and finite")
    executable = Path(executable).absolute()
    if not stat.S_ISREG(executable.stat().st_mode) or not os.access(executable, os.X_OK):
        raise InvalidArchive("Native validator must be an executable regular file")
    return executable


def verify_native_snapshot(restored, entry, executable, *, timeout=DEFAULT_TIMEOUT):
    """Require the matching binary's exact v1 report for the packed library bytes."""
    executable = validator_path(executable, timeout)
    database = Path(restored).absolute() / "library.sqlite"
    body = validator_output(executable, database, timeout)
    report = decode_json(body)
    fields = {"format", "version", "lineage", "schema_version", "sha256", "bytes",
              "database_verified", "pending_file_deletions", "filesystem_recovery_verified"}
    if not isinstance(report, dict) or set(report) != fields:
        raise InvalidArchive("Unsupported native snapshot verification report")
    if (report["format"] != FORMAT + ".snapshot-verification"
            or type(report["version"]) is not int or report["version"] != 1
            or report["lineage"] != FORMAT
            or type(report["schema_version"]) is not int
            or report["schema_version"] != entry["sqlite"]["schema"]
            or report["sha256"] != entry["sha256"]
            or type(report["bytes"]) is not int or report["bytes"] != entry["size"]
            or report["database_verified"] is not True
            or report["filesystem_recovery_verified"] is not False
            or type(report["pending_file_deletions"]) is not int
            or report["pending_file_deletions"] < 0):
        raise InvalidArchive("Native snapshot verification does not match the archive library")
    return report


def verify_archive_proofs(source, *, native_validator=None, producer_origin=None,
                         timeout=DEFAULT_TIMEOUT, temp_parent=None, reserve=RESERVE_BYTES):
    """Return success only after every requested check passes on the same restore."""
    from .receipts import origin, verify_restored_receipts

    if producer_origin is not None:
        producer_origin = origin(producer_origin)
    if native_validator is not None:
        native_validator = validator_path(native_validator, timeout)
    with tempfile.TemporaryDirectory(prefix="stash-archive-verify-", dir=temp_parent) as temp:
        restored = Path(temp) / "restored"
        manifest = import_archive(source, restored, reserve=reserve)
        # Consume the complete iterator, so the inventory digest and unique
        # library requirement are checked before any additional proof is used.
        library, = [entry for entry in iter_artifacts(source, manifest) if entry["role"] == "library"]
        manifest_sha256 = hashlib.sha256(json_bytes(manifest)).hexdigest()
        result = {"uuid": manifest["uuid"], "manifest_sha256": manifest_sha256,
                  "coverage": manifest["coverage"], "contents_verified": True}
        if native_validator is not None:
            proof = verify_native_snapshot(restored, library, native_validator, timeout=timeout)
            result["native_snapshot"] = dict(proof, archive_uuid=manifest["uuid"],
                                             manifest_sha256=manifest_sha256,
                                             component={"role": "library", "name": library["name"]})
        if producer_origin is not None:
            result["ingestion_receipts"] = verify_restored_receipts(source, restored, manifest, producer_origin)
        return result
