"""Run the actual dedupe CLI across a Go server/database and Python restart."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import sys

from stash_ingest.dedupe import Journal
from stash_ingest.dedupe_host import FORMAT, main


def run(setup):
    directory, media = Path(setup["directory"]), Path(setup["media"])
    worker = directory / "worker"
    worker.mkdir(exist_ok=True)
    report = directory / "candidates.json"
    if setup["phase"] == "lost-response":
        report.write_text(json.dumps({"groups": [{"file_len": len(b"same complete bytes"),
            "files": [str(media / name) for name in ("third.mp4", "duplicate.mp4", "keep.mp4")]}]}))
        finder = directory / "finder"
        finder.write_text(f"#!{sys.executable}\nprint({report.read_text()!r})\n")
        finder.chmod(0o700)
        key = directory / "application-key"
        key.write_text("fixture-application-key\n")
        key.chmod(0o600)
        (directory / "host.json").write_text(json.dumps({"format": FORMAT, "endpoint": setup["endpoint"],
            "root": str(media), "root_uuid": setup["root_uuid"], "state_dir": str(directory / "state"),
            "library_lock": str(directory / "library.lock"), "lock_roots": [str(worker)],
            "api_key_file": str(key), "fclones": str(finder), "stamp_file": str(directory / "stamp")}))
    # The host path reads its private file and never exports the application key
    # into the candidate finder environment.
    os.environ["STASH_API_KEY"] = "must-not-be-used"
    arguments = ["--config", str(directory / "host.json"), "--all-content"]
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        code = main(arguments)
    if setup["phase"] == "lost-response":
        assert code == 1 and "network_unavailable" in errors.getvalue(), (code, output.getvalue(), errors.getvalue())
        assert Journal.has_active(directory / "state")
        assert not (media / "duplicate.mp4").exists()
        assert (media / "third.mp4").exists()
        assert not (directory / "stamp").exists()
    else:
        assert code == 0, (code, output.getvalue(), errors.getvalue())
        result = json.loads(output.getvalue())
        assert result["all_removed"] and result["committed"] == 2 and result["pending"] == 0, result
        assert not Journal.has_active(directory / "state")
        assert sorted(p.name for p in media.iterdir()) == ["keep.mp4"]
        assert (directory / "stamp").is_file()
    print(json.dumps({"phase": setup["phase"], "verified": True}))


if __name__ == "__main__":
    run(json.loads(sys.argv[1]))
