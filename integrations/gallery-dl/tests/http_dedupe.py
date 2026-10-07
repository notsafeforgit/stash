"""Run the actual dedupe CLI across a Go server/database and Python restart."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sys

from stash_ingest.dedupe import main


def run(setup):
    directory, media = Path(setup["directory"]), Path(setup["media"])
    worker = directory / "worker"
    worker.mkdir(exist_ok=True)
    report = directory / "candidates.json"
    if setup["phase"] == "lost-response":
        report.write_text(json.dumps({"groups": [{"file_len": len(b"same complete bytes"),
            "files": [str(media / name) for name in ("third.mp4", "duplicate.mp4", "keep.mp4")]}]}))
    arguments = ["--endpoint", setup["endpoint"], "--root", str(media), "--root-uuid", setup["root_uuid"],
                 "--state-dir", str(directory / "state"), "--library-lock", str(directory / "library.lock"),
                 "--lock-root", str(worker), "--report", str(report), "--all-content"]
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        code = main(arguments)
    if setup["phase"] == "lost-response":
        assert code == 1 and "network_unavailable" in errors.getvalue(), (code, output.getvalue(), errors.getvalue())
        assert (directory / "state" / "active.json").exists()
        assert not (media / "duplicate.mp4").exists()
        assert (media / "third.mp4").exists()
    else:
        assert code == 0, (code, output.getvalue(), errors.getvalue())
        result = json.loads(output.getvalue())
        assert result["all_removed"] and result["committed"] == 2 and result["pending"] == 0, result
        assert not (directory / "state" / "active.json").exists()
        assert sorted(p.name for p in media.iterdir()) == ["keep.mp4"]
    print(json.dumps({"phase": setup["phase"], "verified": True}))


if __name__ == "__main__":
    run(json.loads(sys.argv[1]))
