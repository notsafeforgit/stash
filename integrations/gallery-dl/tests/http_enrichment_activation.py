"""Exercise saved bulk activation against real native imports and HTTP receipts."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sqlite3
import sys
import uuid

from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_upload import main as upload_main
from stash_ingest.automation_enrichment_import import main as import_main
from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main as registry_main
from stash_ingest.enrichment_activation import main
from stash_ingest.catalog_snapshot import prepare as prepare_catalog
from stash_ingest.catalog_upload import main as upload_catalog
from stash_ingest.catalog_evidence_import import main as import_evidence
from stash_ingest.catalog_enrichment_import import main as import_receipts
from stash_ingest.enrichment_activation_client import EnrichmentActivationClient
from test_catalog_snapshot import catalog_fixture, CAPTURED
from test_automation_snapshot import automation_fixture
from test_catalog_registry_import import registry_fixture


def execute(fn, args, wanted=0):
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        actual = fn(args)
    assert actual == wanted, (actual, output.getvalue(), errors.getvalue())
    return json.loads(errors.getvalue() if wanted == 1 else output.getvalue())


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    registry_path = directory / "registry.sqlite"
    identities, _ = registry_fixture(registry_path)
    with sqlite3.connect(registry_path) as db:
        db.execute("INSERT INTO catalogs VALUES (?,?,?,?,?,?)",
                   ("c_" + "1" * 32, "collection", "Enrichment source", "directory:Enrichment source", "2020-01-01T00:00:00Z", None))
    parent = execute(identity_main, ["--registry", str(registry_path), "--source", setup["source"], "--snapshot", str(uuid.uuid4()),
                                    "--captured-at", identities["captured_at"], "--namespace", "stash", "--endpoint", setup["endpoint"]])
    parent_file = directory / "parent.json"
    parent_file.write_text(json.dumps(parent))
    execute(identity_main, ["--binding", str(parent_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", parent["plan_sha256"]])
    binding = execute(registry_main, ["--registry", str(registry_path), "--identity-import", str(parent_file), "--snapshot", str(uuid.uuid4()), "--endpoint", setup["endpoint"]])
    binding_file = directory / "registry.json"
    binding_file.write_text(json.dumps(binding))
    execute(registry_main, ["--binding", str(binding_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", binding["plan_sha256"]])
    source = directory / "automation.sqlite"
    automation_fixture(source, empty=True)
    with sqlite3.connect(source) as db:
        for index in range(205):
            db.execute("INSERT INTO enrichment_jobs VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                       ("c_" + "1" * 32, f"reddit:post:activation{index:03d}", 1, "reddit", "reddit:handle:juniper",
                        f"https://www.reddit.com/comments/activation{index:03d}", "pending", 25, 3, 0, None, None,
                        "2026-09-29T00:00:00Z", "2026-09-29T00:00:00Z"))
    catalog = directory / "catalog.sqlite"
    catalog_fixture(catalog)
    snapshot_catalog = directory / "catalog-snapshot"
    catalog_prepared = prepare_catalog(catalog, snapshot_catalog, str(uuid.uuid4()), setup["source"], CAPTURED)
    catalog_args = ["--snapshot", str(snapshot_catalog), "--endpoint", setup["endpoint"], "--expected-sha256", catalog_prepared["manifest_sha256"]]
    uploaded = execute(upload_catalog, catalog_args)
    execute(import_evidence, catalog_args)
    execute(import_receipts, catalog_args)
    source_before = source.read_bytes()
    snapshot = directory / "snapshot"
    prepared = prepare(source, snapshot, setup["snapshot"], setup["source"], "2026-10-02T00:00:00Z")
    frozen = ["--snapshot", str(snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", prepared["manifest_sha256"]]
    execute(upload_main, frozen)
    imported = execute(import_main, frozen)
    assert imported["mapped_records"] == 205 and imported["review_records"] == 0
    disabled = execute(main, ["prepare", "--snapshot", setup["snapshot"], "--manifest-sha256", prepared["manifest_sha256"],
                              "--output", str(directory / "disabled-plan"), "--endpoint", setup["endpoint"]])
    assert disabled["counts"] == {"collection_disabled": 205} and disabled["batches"] == 0
    client = EnrichmentActivationClient(setup["endpoint"])
    collection = next(c for c in client.request("GET", "/collections?limit=100") if c["uuid"] == uploaded["collection_uuid"])
    definition = {k: v for k, v in collection.items() if k in {"label", "kind", "namespace", "target_url", "account_uuid", "root_uuid", "path_prefix"}}
    definition.update(state="active", expected_revision=collection["revision"], reason="Reviewed metadata-only fixture collection")
    revised = client.request("PUT", "/collections/" + collection["uuid"], definition)
    assert revised["revision"] == 2
    output = directory / "activation"
    plan = execute(main, ["prepare", "--snapshot", setup["snapshot"], "--manifest-sha256", prepared["manifest_sha256"],
                          "--output", str(output), "--endpoint", setup["endpoint"]])
    assert plan["candidates"] == 205 and plan["batches"] == 3 and plan["activated"] is False
    before = {path.name: path.read_bytes() for path in output.iterdir()}
    args = ["--plan", str(output), "--expected-sha256", plan["plan_sha256"], "--endpoint", setup["endpoint"]]
    assert execute(main, ["status", *args], 3)["counts"] == {"not_activated": 205}
    failed = execute(main, ["apply", *args], 1)
    assert failed["activation_complete"] is False
    assert failed["error"] == "network_unavailable", failed
    pending = execute(main, ["status", *args], 3)
    assert pending["counts"] == {"activated": 100, "not_activated": 105}, pending
    result = execute(main, ["apply", *args])
    assert result["activation_complete"] is True and result["counts"] == {"activated": 205}
    assert result["execution_status"] == "not_checked"
    assert result == execute(main, ["status", *args]) == execute(main, ["apply", *args])
    assert before == {path.name: path.read_bytes() for path in output.iterdir()}
    assert source.read_bytes() == source_before
    print(json.dumps(result))


if __name__ == "__main__":
    run()
