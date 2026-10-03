"""Exercise the real migration CLI, including a committed response being lost."""

import json
from pathlib import Path
import sqlite3
import sys
import uuid

from http_catalog_registry_import import execute
from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_upload import main as upload_main
from stash_ingest.automation_enrichment_import import AutomationEnrichmentClient, main
from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main as registry_main
from test_automation_snapshot import automation_fixture
from test_catalog_registry_import import registry_fixture


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    registry_path = directory / "registry.sqlite"
    identities, _ = registry_fixture(registry_path)
    parent = execute(identity_main, ["--registry", str(registry_path), "--source", setup["source"], "--snapshot", str(uuid.uuid4()),
                                    "--captured-at", identities["captured_at"], "--namespace", "stash", "--endpoint", setup["endpoint"]])
    parent_file = directory / "parent.json"
    parent_file.write_text(json.dumps(parent))
    execute(identity_main, ["--binding", str(parent_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", parent["plan_sha256"]])
    plan = execute(registry_main, ["--registry", str(registry_path), "--identity-import", str(parent_file), "--snapshot", str(uuid.uuid4()), "--endpoint", setup["endpoint"]])
    binding = directory / "registry.json"
    binding.write_text(json.dumps(plan))
    execute(registry_main, ["--binding", str(binding), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", plan["plan_sha256"]])
    source = directory / "automation.sqlite"
    automation_fixture(source, empty=True)
    with sqlite3.connect(source) as db:
        for index in range(205):
            db.execute("INSERT INTO enrichment_source_progress VALUES (?,?)",
                       (f"service-{index:03}", 0 if index == 0 else 1577836700.125))
    before = source.read_bytes()
    snapshot = directory / "snapshot"
    prepared = prepare(source, snapshot, setup["snapshot"], setup["source"], "2020-01-01T00:00:00Z")
    args = ["--snapshot", str(snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", prepared["manifest_sha256"]]
    execute(upload_main, args)
    execute(main, args, 1)  # First 200 records committed, but the response was lost.
    result = execute(main, args, 2)
    assert result == execute(main, args, 2)
    assert result["processed_records"] == 205 and result["mapped_records"] == 204 and result["review_records"] == 1
    assert result["imported"] is False and result["state"] == "review"
    client = AutomationEnrichmentClient(setup["endpoint"], "STASH_API_KEY")
    prefix = f"/{setup['snapshot']}/enrichment-import/records"
    after, records = 0, []
    while True:
        page = client.request("GET", f"{prefix}?after={after}&limit=75", None, prepared["manifest_sha256"], "application/json")
        if not page:
            break
        records.extend(page)
        after = page[-1]["ordinal"]
    assert len(records) == 205
    normalized = next(row for row in records if row["disposition"] == "source_progress")
    details = client.request("GET", f"{prefix}/{normalized['ordinal']}", None, prepared["manifest_sha256"], "application/json")
    assert details["source_values"]["last_attempt"] == 1577836700.125
    assert normalized["source_last_attempt"] == "2019-12-31T23:58:20.125Z"
    assert source.read_bytes() == before
    print(json.dumps({"records": 205, "review": 1, "source_unchanged": True, "imported": False}))


if __name__ == "__main__":
    run()
