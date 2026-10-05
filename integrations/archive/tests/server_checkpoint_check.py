"""Exercise the portable CLI against the real native checkpoint HTTP handler."""

from contextlib import redirect_stdout
from contextlib import closing
import fcntl
import io
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import sys
import time
import uuid
from unittest.mock import patch

from stash_archive.bundle import export_archive, import_archive
from stash_archive.artwork_pins import ArtworkPins, release_published_artwork, publish_bytes
from stash_archive.cli import main
from stash_archive.checkpoint_release import release_published_checkpoint
from stash_archive.checkpoint_abandon import checkpoint_status, abandon_checkpoint
from stash_archive.storage import InvalidArchive
from stash_archive.component_stage import ComponentStage, release_published_components
from stash_archive.server_checkpoint import ServerCheckpoint
from stash_ingest.publication_lock import ACTIVE, PublicationBarrier

args = json.load(sys.stdin)
root = Path(args["directory"])
output, restored = root / "bundle", root / "restored"
options = ["export", "--server", args["server"], "--api-key-file", args["key_file"],
           "--request-id", args["request_id"], "--recovery-roots", args["roots_file"],
           "--output", str(output), "--reserve-bytes", "0"]
key = Path(args["key_file"]).read_text().strip()
if args.get("boundary"):
    captures = []
    stages = []
    cache = root / "artwork-pins"
    cache.mkdir(mode=0o700)
    pins = ArtworkPins(cache, [args["blobs"]], reserve=0)
    worker_locks = root / "worker-locks"
    worker_locks.mkdir()
    stage_cache = root / "component-stages"
    stage_cache.mkdir(mode=0o700)
    downloads, profile = root / "download-archive.sqlite", root / "worker-profile.json"
    with closing(sqlite3.connect(downloads)) as db:
        db.execute("CREATE TABLE archive(entry TEXT PRIMARY KEY)")
        db.execute("INSERT INTO archive VALUES('original')")
        db.commit()
    profile.write_bytes(b'{"profile":"original"}\n')
    external = [{"role": "download_archive", "name": "downloads.sqlite", "path": downloads},
                {"role": "worker_profile", "name": "profile.json", "path": profile}]

    def locked():
        fd = os.open(worker_locks / ACTIVE, os.O_RDWR)
        try:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                return False
            except BlockingIOError:
                return True
        finally:
            os.close(fd)

    def capture(ready):
        assert locked()
        captures.append(ready)
        record = pins.capture(ready)
        # Once the inodes are retained, a later live deletion cannot alter this
        # database's artwork. Remove the source to require the real pin provider.
        shutil.rmtree(args["blobs"])
        return {"artwork": record, "large_identifier": 18446744073709551615, "components": stages[-1].binding()}

    def coordinated_export(destination):
        with PublicationBarrier([worker_locks]) as barrier:
            validated = []
            def validate_boundary(record):
                assert not locked()
                pins.open_bound(record)
                stages[-1].validate_boundary(record)
                validated.append(record)
            client = ServerCheckpoint(args["server"], key, args["request_id"],
                                      json.loads(Path(args["roots_file"]).read_text()),
                                      boundary=capture, boundary_release=barrier.release, boundary_validate=validate_boundary)
            stage = ComponentStage(stage_cache, client, external, barrier, reserve=0)
            stages.append(stage)
            was_fresh = stage.fresh
            transport = client.open
            requests = []
            def checked_transport(suffix, data=None):
                requests.append((suffix, data is None))
                if not was_fresh:
                    assert data is None, "reopened stage attempted a new server capture"
                if suffix.endswith("/boundary") or "/components/" in suffix:
                    assert not locked(), "workers remained locked during checkpoint copy/download"
                if suffix.endswith("/components/library.sqlite"):
                    assert validated, "large library download preceded retained-view validation"
                return transport(suffix, data)
            with patch.object(client, "open", side_effect=checked_transport):
                stage.seal()
                assert not any(suffix.endswith("/components/library.sqlite") for suffix, _ in requests)
                export_archive(None, destination, reserve=0, server_checkpoint=client, artwork_pins=pins,
                               component_stage=stage, producer_origin=args["server"])
            assert not locked()
            assert len(validated) == 2
            return client

    client = coordinated_export(output)
    first_receipt = client.boundary_receipt
    assert first_receipt["details"]["large_identifier"] == 18446744073709551615
    assert len(captures) == 1
    with closing(sqlite3.connect(downloads)) as db:
        db.execute("INSERT INTO archive VALUES('later')")
        db.commit()
    profile.unlink()
    # A sealed server replay must return the same view without invoking capture.
    pins = ArtworkPins(cache, [args["blobs"]], reserve=0)
    client = coordinated_export(root / "replayed")
    assert client.boundary_receipt == first_receipt and len(captures) == 1
else:
    with redirect_stdout(io.StringIO()):
        main(options)
import_archive(output, restored, reserve=0)
if args.get("boundary"):
    data = b"original checkpoint artwork"
    checksum = hashlib.md5(data).hexdigest()
    assert (restored / "blobs" / checksum[:2] / checksum[2:4] / checksum).read_bytes() == data
    assert (restored / "components/worker_profile/profile.json").read_bytes() == b'{"profile":"original"}\n'
    with closing(sqlite3.connect(restored / "components/download_archive/downloads.sqlite")) as db:
        assert db.execute("SELECT entry FROM archive").fetchall() == [("original",)]
checkpoint = json.loads((restored / "components/operating_state/server-checkpoint.json").read_bytes())
assert checkpoint["uuid"] == args["request_id"]
assert "http-backup-fixture" in (restored / "components/config/config.yml").read_text()
assert "http-backup-fixture" not in json.dumps(checkpoint)
assert (restored / "components/file_journal/deletions.zip").stat().st_size > 0
# The private enclosing bundle is durably exported and has passed a complete
# real restore. Production publishers must additionally finish remote readback
# and master-manifest publication before using this release operation.
client = ServerCheckpoint(args["server"], key, args["request_id"])
release = release_published_checkpoint(output, client)
assert release_published_checkpoint(output, client) == release
if args.get("boundary"):
    view = pins.open_bound(first_receipt).verify()

    def fail_completion(path, body):
        if path.name == "released.json":
            raise RuntimeError("lost completion before durable marker")
        return publish_bytes(path, body)

    with patch("stash_archive.artwork_pins.publish_bytes", side_effect=fail_completion):
        try:
            release_published_artwork(output, client, pins)
            raise AssertionError("injected cleanup interruption did not fail")
        except RuntimeError as error:
            assert str(error) == "lost completion before durable marker"
    assert (view.path / "release.json").is_file() and not (view.path / "released.json").exists()
    assert release_published_artwork(output, client, pins) == release
    assert release_published_artwork(output, client, pins) == release
    assert (view.path / "manifest.json").is_file() and (view.path / "released.json").is_file()
    assert list((view.path / "0").iterdir()) == []
    component_path = stages[-1].path
    (component_path / "unrelated").write_bytes(b"keep unrelated files")
    # A structurally valid enclosing inventory that omits one staged artifact
    # cannot authorize even the server release request or local deletion.
    from stash_archive.storage import InvalidArchive, json_bytes
    original_manifest = (output / "manifest.json").read_bytes()
    original_inventory = (output / "artifacts.jsonl").read_bytes()
    entries = [json.loads(line) for line in original_inventory.splitlines()]
    entries = [entry for entry in entries if entry["role"] != "worker_profile"]
    changed_inventory = b"".join(json_bytes(entry) for entry in entries)
    changed_manifest = json.loads(original_manifest)
    changed_manifest["inventory"] = {"sha256": hashlib.sha256(changed_inventory).hexdigest(),
                                     "size": len(changed_inventory), "count": len(entries),
                                     "total_bytes": sum(entry["size"] for entry in entries)}
    try:
        (output / "manifest.json").write_bytes(json_bytes(changed_manifest))
        (output / "artifacts.jsonl").write_bytes(changed_inventory)
        with patch.object(client, "open", side_effect=AssertionError("incomplete archive must not request release")):
            try:
                release_published_components(output, client, stage_cache)
                raise AssertionError("incomplete external inventory authorized release")
            except InvalidArchive as error:
                assert "exact staged component" in str(error)
    finally:
        (output / "manifest.json").write_bytes(original_manifest)
        (output / "artifacts.jsonl").write_bytes(original_inventory)
    with patch("stash_archive.component_stage.publish_bytes", side_effect=fail_completion):
        try:
            release_published_components(output, client, stage_cache)
            raise AssertionError("injected external cleanup interruption did not fail")
        except RuntimeError as error:
            assert str(error) == "lost completion before durable marker"
    assert (component_path / "release.json").is_file() and not (component_path / "released.json").exists()
    assert release_published_components(output, client, stage_cache) == release
    assert release_published_components(output, client, stage_cache) == release
    assert (component_path / "unrelated").read_bytes() == b"keep unrelated files"
    assert (component_path / "manifest.json").is_file() and (component_path / "released.json").is_file()
    assert not list(component_path.glob("component-*"))
    with closing(sqlite3.connect(downloads)) as db:
        assert db.execute("SELECT entry FROM archive ORDER BY entry").fetchall() == [("later",), ("original",)]
failed_id = ""
if args.get("boundary"):
    failed_id = str(uuid.uuid4())

    def fail_provider(ready):
        raise RuntimeError("isolated provider failure")

    failed = ServerCheckpoint(args["server"], key, failed_id, boundary=fail_provider)
    try:
        export_archive(None, root / "failed", reserve=0, server_checkpoint=failed)
        raise AssertionError("provider failure reported a completed archive")
    except RuntimeError as error:
        assert str(error) == "isolated provider failure"
    assert not (root / "failed").exists()
    deadline = time.monotonic() + 5
    while True:
        try:
            state = checkpoint_status(failed, 0)
            assert state["state"] == "partial"
            break
        except InvalidArchive as error:
            if "HTTP 409" not in str(error) or time.monotonic() >= deadline:
                raise
            time.sleep(0.01)
    abandoned = abandon_checkpoint(failed, 0)
    assert abandon_checkpoint(failed, 0) == abandoned
    assert checkpoint_status(failed, 0)["state"] == "abandoned"
    try:
        failed.seal(reserve=0)
        raise AssertionError("abandoned UUID captured newer state")
    except InvalidArchive as error:
        assert "HTTP 410" in str(error)
missing = ServerCheckpoint(args["server"], key, str(uuid.uuid4()))
assert checkpoint_status(missing, 0)["state"] == "missing"
abandoned_missing = abandon_checkpoint(missing, 0)
assert abandon_checkpoint(missing, 0) == abandoned_missing
assert checkpoint_status(missing, 0)["state"] == "abandoned"
try:
    missing.seal(reserve=0)
    raise AssertionError("delayed capture was not fenced")
except InvalidArchive as error:
    assert "HTTP 410" in str(error)
print(json.dumps({"verified": True, "restored": str(restored), "checkpoint": checkpoint["uuid"], "failed_id": failed_id}))
