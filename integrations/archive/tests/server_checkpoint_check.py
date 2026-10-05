"""Exercise the portable CLI against the real native checkpoint HTTP handler."""

from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import sys

from stash_archive.bundle import import_archive
from stash_archive.cli import main

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
print(json.dumps({"verified": True, "restored": str(restored), "checkpoint": checkpoint["uuid"]}))
