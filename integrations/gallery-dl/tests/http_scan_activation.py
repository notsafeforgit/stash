"""Real Go API and Python CLI activation with an interrupted acknowledgement."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sys
from urllib.request import Request, urlopen

from stash_ingest.scan_activation import main


setup = json.loads(sys.argv[1])
path = Path(setup["directory"]) / "activation.json"
path.write_text(json.dumps(setup["binding"]))
args = ["--binding", str(path), "--endpoint", setup["endpoint"]]


def call(arguments, expected):
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = main(arguments)
    assert code == expected, (out.getvalue(), err.getvalue())
    return json.loads(out.getvalue() if expected == 0 else err.getvalue())


preview = call(args, 0)
assert preview["action"] == "preview" and preview["state"] == "deferred"
assert len(preview["job_uuids"]) == 1
assert preview["progress"]["cursor"].startswith("gallery-dl-archive-v1:")
path.write_text(json.dumps(preview))
frozen = path.read_bytes()
apply = args + ["--apply", "--expected-sha256", preview["plan_sha256"]]
assert call(apply, 1)["acknowledged"] is False  # server committed, response lost
first = call(apply, 0)
assert first == call(apply, 0)
assert first["state"] == "deferred" and first["action"] == "activated"
assert path.read_bytes() == frozen
request = Request(setup["endpoint"] + "/api/v3/archive/scan-journal-activations/" + preview["binding"]["uuid"],
                  headers={"ApiKey": "fixture-application-key"})
with urlopen(request, timeout=10) as response:
    retained = json.load(response)
assert retained == {k: v for k, v in first.items() if k != "action"}
call(args + ["--apply"], 1)
call(args + ["--apply", "--expected-sha256", "0" * 64], 1)
print(json.dumps({"run_uuid": first["run_uuid"], "activation_uuid": first["binding"]["uuid"]}))
