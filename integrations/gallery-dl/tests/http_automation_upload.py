"""Run the actual automation upload client against the native application API."""

import json
from pathlib import Path
import sys
from unittest.mock import patch
import uuid

from http_catalog_registry_import import execute
from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_upload import main
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
    automation_fixture(source)
    before = source.read_bytes()
    snapshot = directory / "snapshot"
    with patch("stash_ingest.catalog_snapshot.MAX_CHUNK_ROWS", 2):
        prepared = prepare(source, snapshot, setup["snapshot"], setup["source"], "2020-01-01T00:00:00Z")
    args = ["--snapshot", str(snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", prepared["manifest_sha256"]]
    execute(main, args, 1)  # Manifest committed; its acknowledgement was lost.
    execute(main, args, 1)  # First chunk committed; its acknowledgement was lost.
    received = execute(main, args)
    assert received == execute(main, args)
    assert received["state"] == "received" and received["imported"] is False
    assert received["received_records"] == prepared["records"] == 14
    assert received["registry_import_uuid"] == plan["uuid"]
    assert received["pending_families"] == prepared["pending_families"]
    assert source.read_bytes() == before
    print(json.dumps({"records": received["received_records"], "source_unchanged": True, "imported": False}))


if __name__ == "__main__":
    run()
