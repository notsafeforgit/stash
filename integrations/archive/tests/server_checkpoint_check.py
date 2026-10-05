"""Exercise the portable CLI against the real native checkpoint HTTP handler."""

from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import sys

from stash_archive.bundle import import_archive
from stash_archive.cli import main
from stash_archive.checkpoint_release import release_published_checkpoint
from stash_archive.server_checkpoint import ServerCheckpoint

args = json.load(sys.stdin)
root = Path(args["directory"])
output, restored = root / "bundle", root / "restored"
options = ["export", "--server", args["server"], "--api-key-file", args["key_file"],
           "--request-id", args["request_id"], "--recovery-roots", args["roots_file"],
           "--output", str(output), "--reserve-bytes", "0"]
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
client = ServerCheckpoint(args["server"], Path(args["key_file"]).read_text().strip(), args["request_id"])
release = release_published_checkpoint(output, client)
assert release_published_checkpoint(output, client) == release
print(json.dumps({"verified": True, "restored": str(restored), "checkpoint": checkpoint["uuid"]}))
