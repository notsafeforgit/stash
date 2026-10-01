"""A real Go API must acknowledge the frozen Python input after lost response."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import sys
from urllib.request import Request, urlopen

from stash_ingest.catalog_identity_import import main
from test_catalog_identity_import import registry_fixture


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    path = directory / "registry.sqlite"
    document = registry_fixture(path)
    before = path.read_bytes()
    associations = directory / "accounts.json"
    associations.write_text(json.dumps(setup["accounts"]))
    args = ["--registry", str(path), "--source", setup["source"], "--snapshot", setup["snapshot"],
            "--namespace", "stash", "--captured-at", document["captured_at"], "--account-bindings", str(associations),
            "--endpoint", setup["endpoint"]]
    output = io.StringIO()
    with redirect_stdout(output):
        assert main(args) == 0
    plan = json.loads(output.getvalue())
    assert [row["action"] for row in plan["identities"]] == ["adopt", "redirect", "review"]
    assert len(plan["records"]) == 13 and all("evidence" not in row for row in plan["records"])
    frozen = directory / "plan.json"
    frozen.write_text(output.getvalue())
    apply = ["--binding", str(frozen), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", plan["plan_sha256"]]
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        assert main(apply) == 1
    first = None
    for _ in range(2):
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors):
            assert main(apply) == 0, errors.getvalue()
        receipt = json.loads(output.getvalue())
        assert receipt["input_sha256"] == plan["input_sha256"]
        if first is not None:
            assert first == receipt
        first = receipt
    request = Request(setup["endpoint"] + "/api/v3/archive/catalog-identity-imports/" + setup["snapshot"] + "/records",
                      headers={"ApiKey": os.environ["STASH_API_KEY"]})
    with urlopen(request) as response:
        assert response.headers["Cache-Control"] == "no-store"
        records = json.load(response)
    assert len(records) == 13
    for table, rows in document["tables"].items():
        retained = [record["evidence"] for record in records if record["table"] == table]
        assert sorted(json.dumps(row, sort_keys=True) for row in retained) == sorted(json.dumps(row, sort_keys=True) for row in rows)
    assert path.read_bytes() == before
    print(json.dumps({"records": 13, "source_unchanged": True, "replayed": True}))


if __name__ == "__main__":
    run()
