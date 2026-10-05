"""Exercise the portable CLI against the real native checkpoint HTTP handler."""

from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import sys
import uuid

from stash_archive.bundle import export_archive, import_archive
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

    def capture(ready):
        captures.append(ready)
        # Exercise real HTTP coordination with a private stand-in view. Actual
        # filesystem providers must supply immutable views and producer barriers.
        (root / "isolated-view").mkdir()
        return {"snapshot": {"guid": 18446744073709551615, "name": "isolated-view"}}

    client = ServerCheckpoint(args["server"], key, args["request_id"], json.loads(Path(args["roots_file"]).read_text()), boundary=capture)
    export_archive(None, output, reserve=0, server_checkpoint=client)
    first_receipt = client.boundary_receipt
    assert first_receipt["details"]["snapshot"]["guid"] == 18446744073709551615
    assert len(captures) == 1
    # A sealed server replay must return the same view without invoking capture.
    export_archive(None, root / "replayed", reserve=0, server_checkpoint=client)
    assert client.boundary_receipt == first_receipt and len(captures) == 1
else:
    with redirect_stdout(io.StringIO()):
        main(options)
import_archive(output, restored, reserve=0)
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
