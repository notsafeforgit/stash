"""Exercise native discovery review using real frozen inputs and lost HTTP ACKs."""

import hashlib
import json
from pathlib import Path
import sqlite3
import sys
import uuid

from http_enrichment_activation import execute
from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_upload import main as upload_main
from stash_ingest.automation_enrichment_import import main as enrichment_main
from stash_ingest.automation_discovery_import import main as discovery_main
from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main as registry_main
from stash_ingest.catalog_snapshot import prepare as prepare_catalog
from stash_ingest.catalog_upload import main as upload_catalog
from stash_ingest.catalog_evidence_import import main as import_evidence
from stash_ingest.catalog_enrichment_import import main as import_receipts
from stash_ingest.client import Unavailable
from stash_ingest.enrichment_activation_client import EnrichmentActivationClient
from test_automation_snapshot import automation_fixture
from test_catalog_registry_import import registry_fixture
from test_catalog_snapshot import catalog_fixture, CAPTURED


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    registry_path = directory / "registry.sqlite"
    identities, _ = registry_fixture(registry_path)
    catalog_id = "c_" + "1" * 32
    with sqlite3.connect(registry_path) as db:
        db.execute("INSERT INTO catalogs VALUES (?,?,?,?,?,?)",
                   (catalog_id, "collection", "Discovery source", "directory:Discovery source", "2020-01-01T00:00:00Z", None))
    parent = execute(identity_main, ["--registry", str(registry_path), "--source", setup["source"], "--snapshot", str(uuid.uuid4()),
                                    "--captured-at", identities["captured_at"], "--namespace", "stash", "--endpoint", setup["endpoint"]])
    parent_file = directory / "parent.json"
    parent_file.write_text(json.dumps(parent))
    execute(identity_main, ["--binding", str(parent_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", parent["plan_sha256"]])
    binding = execute(registry_main, ["--registry", str(registry_path), "--identity-import", str(parent_file), "--snapshot", str(uuid.uuid4()), "--endpoint", setup["endpoint"]])
    binding_file = directory / "registry.json"
    binding_file.write_text(json.dumps(binding))
    execute(registry_main, ["--binding", str(binding_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", binding["plan_sha256"]])
    catalog = directory / "catalog.sqlite"
    catalog_fixture(catalog)
    snapshot_catalog = directory / "catalog-snapshot"
    catalog_prepared = prepare_catalog(catalog, snapshot_catalog, str(uuid.uuid4()), setup["source"], CAPTURED)
    catalog_args = ["--snapshot", str(snapshot_catalog), "--endpoint", setup["endpoint"], "--expected-sha256", catalog_prepared["manifest_sha256"]]
    uploaded = execute(upload_catalog, catalog_args)
    execute(import_evidence, catalog_args)
    execute(import_receipts, catalog_args)

    source = directory / "automation.sqlite"
    automation_fixture(source, empty=True)
    account_key = "reddit:handle:deliberately-unlinked"
    profile = "https://www.reddit.com/user/deliberately-unlinked/submitted/?sort=new"
    job_key = hashlib.sha256(json.dumps(["reddit", account_key, profile], sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    evidence = json.dumps({"titles": ["album with source positions"], "texts": [], "dates": ["2026-10-03"],
                           "paths": ["Historical album/photo.jpg"], "urls": [], "platform": "reddit", "account_key": account_key,
                           "candidate_url": None, "strict_filename_id": False})
    with sqlite3.connect(source) as db:
        db.execute("INSERT INTO discovery_accounts VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
                   (job_key, "reddit", account_key, profile, "retry", '{"after":"t3_prior"}', None, 67, 5, 0,
                    "network unavailable", "2026-09-29T00:00:00Z", "2026-10-01T12:00:00Z"))
        for i in range(2):
            db.execute("INSERT INTO discovery_targets VALUES(?,?,?,?,?)", (catalog_id, f"reddit:post:activation{i:03d}", job_key, evidence, "pending"))
    original = source.read_bytes()
    snapshot = directory / "snapshot"
    prepared = prepare(source, snapshot, setup["snapshot"], setup["source"], "2026-10-02T00:00:00Z")
    args = ["--snapshot", str(snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", prepared["manifest_sha256"]]
    execute(upload_main, args)
    execute(enrichment_main, args)
    imported = execute(discovery_main, args)
    assert imported["mapped_records"] == 3 and imported["review_records"] == 0, imported

    client = EnrichmentActivationClient(setup["endpoint"])
    collection = next(c for c in client.request("GET", "/collections?limit=100") if c["uuid"] == uploaded["collection_uuid"])
    definition = {k: v for k, v in collection.items() if k in {"label", "kind", "namespace", "target_url", "account_uuid", "root_uuid", "path_prefix"}}
    definition.update(state="active", expected_revision=collection["revision"], reason="Reviewed discovery fixture collection")
    collection = client.request("PUT", "/collections/" + collection["uuid"], definition)
    prefix = f"/automation-snapshots/{setup['snapshot']}/discovery-import/records"
    rows = client.request("GET", prefix)
    account = next(row for row in rows if row["table"] == "discovery_accounts")
    targets = [row for row in rows if row["table"] == "discovery_targets"]
    detail = client.request("GET", prefix + "/" + str(account["ordinal"]))
    listing = {"uuid": str(uuid.uuid4()), "account_uuid": account["account_uuid"], "collection_uuid": collection["uuid"],
               "collection_revision": collection["revision"], "root_uuid": collection.get("root_uuid"), "profile_url": account["profile_url"],
               "policy_sha256": "a" * 64, "extractor_version": "1.32.15-dev", "initial_cursor": json.loads(detail["source_values"]["cursor_json"]),
               "historical_pages": account["historical_pages"], "legacy": {"snapshot_uuid": setup["snapshot"], "account_ordinal": account["ordinal"]},
               "not_before": account["not_before"]}
    request = {"uuid": str(uuid.uuid4()), "manifest_sha256": prepared["manifest_sha256"], "listing": listing,
               "targets": [{"source_ordinal": row["ordinal"], "source_sha256": row["sha256"]} for row in targets]}
    preview = client.request("POST", "/discovery-activations/preview", request)
    assert preview["input"] == request and len(preview["entries"]) == 2
    assert preview["input"]["listing"]["initial_cursor"] == {"after": "t3_prior"}
    apply = {"input": request, "expected_plan_sha256": preview["plan_sha256"]}
    try:
        client.request("POST", "/discovery-activations", apply)
        raise AssertionError("expected a lost committed response")
    except Unavailable as error:
        assert error.code == "network_unavailable", error
    receipt = client.request("GET", "/discovery-activations/" + request["uuid"])
    assert {key: receipt[key] for key in preview} == preview
    assert client.request("POST", "/discovery-activations", apply) == receipt
    assert client.request("GET", "/discovery-listings/" + listing["uuid"])["historical_pages"] == 67
    assert client.request("GET", "/discovery-listings/" + listing["uuid"] + "/pages") == []
    for entry in receipt["entries"]:
        target = client.request("GET", "/discovery-match-targets/" + entry["target_uuid"])
        assert target["last_page"] == 0 and target["enumeration_complete"] is False
        assert client.request("GET", "/discovery-match-targets/" + entry["target_uuid"] + "/candidates") == []
    assert source.read_bytes() == original
    print(json.dumps(receipt))


if __name__ == "__main__":
    run()
