"""Exercise the installed importer contract against the real native Go API."""

from contextlib import redirect_stderr, redirect_stdout
import hashlib
import io
import json
from pathlib import Path
import sys

from stash_ingest.backfill_import import main
from test_backfill_import import journal_fixture


def run():
    setup = json.loads(sys.argv[1])
    path = Path(setup["directory"]) / "legacy-journal.sqlite"
    journal_fixture(path, count=51)
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    args = ["--journal", str(path), "--root", setup["root"], "--source", setup["source"], "--endpoint", setup["endpoint"], "--apply"]
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        assert main(args) == 1  # The server commits the first batch but drops its response.
    assert json.loads(errors.getvalue())["acknowledged"] == 0
    assert "private" not in errors.getvalue()
    for _ in range(2):
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors):
            assert main(args) == 0, errors.getvalue()
        result = json.loads(output.getvalue())
        assert result["state"] == "imported" and result["acknowledged"] == 52, result
    assert hashlib.sha256(path.read_bytes()).hexdigest() == before
    print(json.dumps({"state": "verified", "decisions": 52, "source_unchanged": True}))


if __name__ == "__main__":
    run()
