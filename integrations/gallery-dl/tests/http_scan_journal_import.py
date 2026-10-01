"""Lost-response replay and evidence inspection against the real native API."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import sys
from urllib.request import Request, urlopen

from stash_ingest.encoding import digest
from stash_ingest.scan_journal_import import main
from test_scan_journal_import import journal_fixture


def run():
    setup = json.loads(sys.argv[1])
    path = Path(setup["directory"]) / "legacy-scan-journal.sqlite"
    document = journal_fixture(path)
    before = path.read_bytes()
    args = ["--journal", str(path), "--root", setup["root"], "--source", setup["source"],
            "--snapshot", setup["snapshot"], "--captured-at", document["captured_at"]]
    output = io.StringIO()
    with redirect_stdout(output):
        assert main(args) == 0
    prepared = json.loads(output.getvalue())
    apply = [*args, "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", prepared["input_sha256"]]
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        assert main(apply) == 1
    for _ in range(2):
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors):
            assert main(apply) == 0, errors.getvalue()
        result = json.loads(output.getvalue())
        assert result["record_count"] == 8 and result["jobs_activated"] == 0
        assert result["input_sha256"] == prepared["input_sha256"]
    def fetch(route):
        request = Request(setup["endpoint"] + "/api/v3/archive/" + route, headers={"ApiKey": os.environ["STASH_API_KEY"]})
        with urlopen(request) as response:
            assert response.headers["Cache-Control"] == "no-store"
            return json.load(response)
    records = fetch("scan-journals/" + setup["snapshot"] + "/records")
    assert len(records) == 8 and all("evidence" not in item for item in records)
    observed = {}
    for item in records:
        row = fetch("scan-journal-records/" + item["uuid"])
        observed.setdefault(row["table"], []).append(row["evidence"])
        assert row["summary"]["activation"] == "not_activated"
    for table, rows in document["tables"].items():
        assert sorted(map(lambda row: json.dumps(row, sort_keys=True), observed[table])) == sorted(map(lambda row: json.dumps(row, sort_keys=True), rows))
    assert not fetch("scan-journals/" + setup["snapshot"] + "/records?after=" + str(records[-1]["sequence"]))
    assert len(fetch("scan-journals/" + setup["snapshot"] + "/records?table=extractor_jobs")) == 2
    assert path.read_bytes() == before
    print(json.dumps({"records": 8, "source_unchanged": True, "input_sha256": digest(before)}))


if __name__ == "__main__":
    run()
