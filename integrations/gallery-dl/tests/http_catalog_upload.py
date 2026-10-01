"""Exercise resumable catalog upload against the native application API."""

from contextlib import closing
import json
from pathlib import Path
import sqlite3
import sys
from unittest.mock import patch
import uuid

from http_catalog_registry_import import execute
from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main as registry_main
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.catalog_upload import main
from test_catalog_registry_import import registry_fixture
from test_catalog_snapshot import CATALOG_ID, CAPTURED, catalog_fixture


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    registry_path = directory / "registry.sqlite"
    identities, _ = registry_fixture(registry_path)
    with closing(sqlite3.connect(registry_path)) as db, db:
        db.execute("INSERT INTO catalogs VALUES(?,?,?,?,?,?)", (CATALOG_ID, "collection", "Historical album", "directory:Historical album", CAPTURED, None))
    parent = execute(identity_main, ["--registry", str(registry_path), "--source", setup["source"], "--snapshot", str(uuid.uuid4()),
                                    "--captured-at", identities["captured_at"], "--namespace", "stash", "--endpoint", setup["endpoint"]])
    parent_file = directory / "parent.json"
    parent_file.write_text(json.dumps(parent))
    execute(identity_main, ["--binding", str(parent_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", parent["plan_sha256"]])
    plan = execute(registry_main, ["--registry", str(registry_path), "--identity-import", str(parent_file), "--snapshot", str(uuid.uuid4()), "--endpoint", setup["endpoint"]])
    binding = directory / "registry.json"
    binding.write_text(json.dumps(plan))
    execute(registry_main, ["--binding", str(binding), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", plan["plan_sha256"]])
    source = directory / "catalog.sqlite"
    catalog_fixture(source)
    original = source.read_bytes()
    snapshot = directory / "snapshot"
    with patch("stash_ingest.catalog_snapshot.MAX_CHUNK_ROWS", 2):
        prepared = prepare(source, snapshot, setup["snapshot"], setup["source"], CAPTURED)
    args = ["--snapshot", str(snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", prepared["manifest_sha256"]]
    execute(main, args, 1)  # Begin committed; response lost.
    execute(main, args, 1)  # First chunk committed; response lost.
    first = execute(main, args)
    assert first == execute(main, args)
    assert first["state"] == "received" and first["imported"] is False
    assert first["received_records"] == prepared["records"] and first["next_chunk"] == prepared["chunks"]
    assert set(first["pending_families"]) == set(json.loads((snapshot / "manifest.json").read_bytes())["tables"])
    assert source.read_bytes() == original
    print(json.dumps({"records": prepared["records"], "chunks": prepared["chunks"], "source_unchanged": True, "imported": False}))


if __name__ == "__main__":
    run()
