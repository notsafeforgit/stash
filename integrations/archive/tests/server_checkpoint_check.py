"""Exercise the portable CLI against the real native checkpoint HTTP handler."""

from contextlib import redirect_stdout
import io
import hashlib
import json
from pathlib import Path
import shutil
import sys
import uuid
from unittest.mock import patch

from stash_archive.bundle import export_archive, import_archive
from stash_archive.artwork_pins import ArtworkPins, release_published_artwork, publish_bytes
from stash_archive.cli import main
from stash_archive.checkpoint_release import release_published_checkpoint
from stash_archive.server_checkpoint import ServerCheckpoint

args = json.load(sys.stdin)
root = Path(args["directory"])
output, restored = root / "bundle", root / "restored"
options = ["export", "--server", args["server"], "--api-key-file", args["key_file"],
           "--request-id", args["request_id"], "--recovery-roots", args["roots_file"],
           "--output", str(output), "--reserve-bytes", "0"]
key = Path(args["key_file"]).read_text().strip()
if args.get("boundary"):
    captures = []
    cache = root / "artwork-pins"
    cache.mkdir(mode=0o700)
    pins = ArtworkPins(cache, [args["blobs"]], reserve=0)

    def capture(ready):
        captures.append(ready)
        record = pins.capture(ready)
        # Once the inodes are retained, a later live deletion cannot alter this
        # database's artwork. Remove the source to require the real pin provider.
        shutil.rmtree(args["blobs"])
        return {"artwork": record, "large_identifier": 18446744073709551615}

    client = ServerCheckpoint(args["server"], key, args["request_id"], json.loads(Path(args["roots_file"]).read_text()), boundary=capture)
    export_archive(None, output, reserve=0, server_checkpoint=client, artwork_pins=pins)
    first_receipt = client.boundary_receipt
    assert first_receipt["details"]["large_identifier"] == 18446744073709551615
    assert len(captures) == 1
    # A sealed server replay must return the same view without invoking capture.
    pins = ArtworkPins(cache, [args["blobs"]], reserve=0)
    export_archive(None, root / "replayed", reserve=0, server_checkpoint=client, artwork_pins=pins)
    assert client.boundary_receipt == first_receipt and len(captures) == 1
else:
    with redirect_stdout(io.StringIO()):
        main(options)
import_archive(output, restored, reserve=0)
if args.get("boundary"):
    data = b"original checkpoint artwork"
    checksum = hashlib.md5(data).hexdigest()
    assert (restored / "blobs" / checksum[:2] / checksum[2:4] / checksum).read_bytes() == data
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
print(json.dumps({"verified": True, "restored": str(restored), "checkpoint": checkpoint["uuid"], "failed_id": failed_id}))
