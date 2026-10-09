"""Real API fixture: a committed admission loses its response across restarts."""

import json
from pathlib import Path
import sys
import time

from stash_ingest.client import Unavailable
from stash_ingest.intake_folder import FORMAT, run
from stash_ingest.intake_journal import Journal


setup = json.loads(sys.argv[1])
directory = Path(setup["directory"])
key = directory / "application-key"
if setup["phase"] == "lost":
    key.write_text("fixture-private-key\n")
    key.chmod(0o600)
value = {"format": FORMAT, "endpoint": setup["endpoint"], "root_uuid": setup["root_uuid"],
         "collections": [{"uuid": setup["collection_uuid"], "path_prefix": "."}], "exclude": [],
         "state_dir": str(directory / "intake"), "library_lock": str(directory / "backup.lock"),
         "api_key_file": str(key), "max_pending": 1, "entries_per_run": 100,
         "settle_seconds": 0, "scan_interval_seconds": 3600}
# Intake uses integer seconds, while freshly created fixture files have
# fractional mtimes. Keep this network-recovery test past that file boundary.
settled_now = int(time.time()) + 2
if setup["phase"] == "lost":
    try:
        run(value, now=settled_now)
    except Unavailable as error:
        assert error.code == "network_unavailable", str(error)
    else:
        raise AssertionError("Expected a lost response")
    saved, = Journal(value["state_dir"], value).pending()
    assert saved["relative_path"] == "first.mp4"
    (directory / "saved-intake.json").write_text(json.dumps(saved))
elif setup["phase"] == "recovered":
    saved = json.loads((directory / "saved-intake.json").read_text())
    for _ in range(2):
        result = run(value, now=settled_now)
        assert result["pending"] == 1 and result["submitted"] == 0, result
        assert result["observed"] == {}, result
        assert Journal(value["state_dir"], value).pending() == [saved]
elif setup["phase"] == "next":
    result = run(value, now=settled_now)
    assert result["pending"] == 1 and result["submitted"] == 1, result
    assert result["observed"] == {"cancelled": 1}, result
    saved, = Journal(value["state_dir"], value).pending()
    assert saved["relative_path"] == "second.jpg"
    (directory / "saved-next-intake.json").write_text(json.dumps(saved))
else:
    value["max_pending"] = 5
    result = run(value, now=int(time.time()) + 4000)
    assert result["pending"] == 1 and result["submitted"] == 1, result
    assert result["scope_exclusions"] == {"source_folder": 1}, result
    saved, = Journal(value["state_dir"], value).pending()
    assert saved["relative_path"] == "purchases/file.jpg", saved
    assert saved["collection_uuid"] == setup["dynamic_collection_uuid"], saved
    assert saved["scan_collection_uuid"] == setup["collection_uuid"], saved
