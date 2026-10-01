"""Exercise both registry phases against the real application API."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import sys
from urllib.request import Request, urlopen
import uuid

from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main
from test_catalog_registry_import import registry_fixture


def execute(fn, args, wanted=0):
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        assert fn(args) == wanted, errors.getvalue()
    return json.loads(output.getvalue()) if wanted == 0 else None


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    path = directory / "registry.sqlite"
    identities, registry = registry_fixture(path)
    original = path.read_bytes()
    parent = execute(identity_main, ["--registry", str(path), "--source", setup["source"], "--snapshot", str(uuid.uuid4()),
                                    "--captured-at", identities["captured_at"], "--namespace", "stash", "--endpoint", setup["endpoint"]])
    parent_file = directory / "parent.json"
    parent_file.write_text(json.dumps(parent))
    execute(identity_main, ["--binding", str(parent_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", parent["plan_sha256"]])
    plan = execute(main, ["--registry", str(path), "--identity-import", str(parent_file), "--snapshot", setup["snapshot"], "--endpoint", setup["endpoint"]])
    assert plan["record_count"] == 21 and len(plan["records"]) == 21
    assert all("evidence" not in record for record in plan["records"])
    assert {row["action"] for row in plan["ownership"]} == {"linked", "unlinked", "review"}
    frozen = directory / "plan.json"
    frozen.write_text(json.dumps(plan))
    args = ["--binding", str(frozen), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", plan["plan_sha256"]]
    execute(main, args, 1)
    first = execute(main, args)
    assert first == execute(main, args)
    request = Request(setup["endpoint"] + "/api/v3/archive/catalog-registry-imports/" + setup["snapshot"] + "/records",
                      headers={"ApiKey": os.environ["STASH_API_KEY"]})
    with urlopen(request) as response:
        assert response.headers["Cache-Control"] == "no-store"
        records = json.load(response)
    assert len(records) == 21
    for table, original_rows in registry["tables"].items():
        copied = [row["evidence"] for row in records if row["table"] == table]
        assert sorted(json.dumps(row, sort_keys=True) for row in copied) == sorted(json.dumps(row, sort_keys=True) for row in original_rows)
    assert path.read_bytes() == original
    print(json.dumps({"records": 21, "source_unchanged": True, "replayed": True}))


if __name__ == "__main__":
    run()
