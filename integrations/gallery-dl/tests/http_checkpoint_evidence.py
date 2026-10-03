"""Review retained checkpoint evidence through the real application API."""

import hashlib
import json
from pathlib import Path
import sqlite3
import sys
import uuid

from http_enrichment_activation import execute
from stash_ingest.activation_client import ActivationClient
from stash_ingest.automation_snapshot import prepare
from stash_ingest.automation_upload import main as upload_main
from stash_ingest.automation_enrichment_import import main as enrichment_main
from stash_ingest.automation_checkpoint_import import main as checkpoint_main
from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main as registry_main
from stash_ingest.catalog_snapshot import prepare as prepare_catalog
from stash_ingest.catalog_upload import main as upload_catalog
from stash_ingest.catalog_evidence_import import main as evidence_main
from stash_ingest.catalog_enrichment_import import main as receipts_main
from stash_ingest.client import Unavailable
from stash_ingest.metadata_bundle import Bundle
from stash_ingest.encoding import encode
from test_automation_snapshot import automation_fixture
from test_catalog_registry_import import registry_fixture
from test_catalog_snapshot import catalog_fixture, CAPTURED


class ReviewClient(ActivationClient):
    kind = "checkpoint_evidence"
    max_http_bytes = 1 << 20


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    registry_path = directory / "registry.sqlite"
    identities, _ = registry_fixture(registry_path)
    catalog = "c_" + "1" * 32
    with sqlite3.connect(registry_path) as db:
        db.execute("INSERT INTO catalogs VALUES (?,?,?,?,?,?)",
                   (catalog, "collection", "Checkpoint source", "directory:Checkpoint source", "2020-01-01T00:00:00Z", None))
    parent = execute(identity_main, ["--registry", str(registry_path), "--source", setup["source"], "--snapshot", str(uuid.uuid4()),
                                    "--captured-at", identities["captured_at"], "--namespace", "stash", "--endpoint", setup["endpoint"]])
    parent_file = directory / "parent.json"
    parent_file.write_text(json.dumps(parent))
    execute(identity_main, ["--binding", str(parent_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", parent["plan_sha256"]])
    registry = execute(registry_main, ["--registry", str(registry_path), "--identity-import", str(parent_file), "--snapshot", str(uuid.uuid4()), "--endpoint", setup["endpoint"]])
    registry_file = directory / "registry.json"
    registry_file.write_text(json.dumps(registry))
    execute(registry_main, ["--binding", str(registry_file), "--endpoint", setup["endpoint"], "--apply", "--expected-sha256", registry["plan_sha256"]])
    catalog_path = directory / "catalog.sqlite"
    catalog_fixture(catalog_path)
    catalog_snapshot = directory / "catalog-snapshot"
    catalog_prepared = prepare_catalog(catalog_path, catalog_snapshot, str(uuid.uuid4()), setup["source"], CAPTURED)
    catalog_args = ["--snapshot", str(catalog_snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", catalog_prepared["manifest_sha256"]]
    execute(upload_catalog, catalog_args)
    execute(evidence_main, catalog_args)
    execute(receipts_main, catalog_args)
    source = directory / "automation.sqlite"
    automation_fixture(source, empty=True)
    metadata = {"category": "reddit", "id": "saved", "title": "Retained source caption", "author": {"id": 9007199254740993}}
    staged = json.dumps({"records": [{"kind": "post", "metadata": metadata}, {"kind": "media", "metadata": metadata}],
                         "pending_children": [{"url": "https://redgifs.com/watch/pending", "parent": metadata, "depth": 1}],
                         "unresolved": [{"url": "https://outside.invalid/unknown", "reason": "external_reference_only"}]})
    with sqlite3.connect(source) as db:
        db.execute("INSERT INTO enrichment_jobs VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                   (catalog, "reddit:post:saved", 1, "reddit", "reddit:handle:juniper", "https://www.reddit.com/comments/saved",
                    "retry", 25, 3, 0, None, staged, "2019-12-30T00:00:00Z", "2019-12-31T00:00:00Z"))
    before = source.read_bytes()
    snapshot = directory / "snapshot"
    prepared = prepare(source, snapshot, setup["snapshot"], setup["source"], "2020-01-01T00:00:00Z")
    args = ["--snapshot", str(snapshot), "--endpoint", setup["endpoint"], "--expected-sha256", prepared["manifest_sha256"]]
    execute(upload_main, args)
    execute(enrichment_main, args, 2)
    execute(checkpoint_main, args)
    client = ReviewClient(setup["endpoint"], "STASH_API_KEY")
    prefix = f"/automation-snapshots/{setup['snapshot']}"
    record = client.request("GET", prefix + "/enrichment-checkpoints/records?after=0&limit=1")[0]
    original = client.request("GET", prefix + "/enrichment-import/records?after=0&limit=1")[0]
    assert original["disposition"] == "staged_review", original
    request = {"uuid": str(uuid.uuid4()), "snapshot_uuid": setup["snapshot"], "manifest_sha256": prepared["manifest_sha256"],
               "ordinal": record["ordinal"], "target_revision": original["target_revision"]}
    plan = client.request("POST", "/checkpoint-evidence/preview", request)
    assert plan["assign_post_identifier"] is True
    assert plan["record_count"] == 2 and len(plan["captures"]) == 1 and plan["pending_count"] == plan["unscoped_reference_count"] == 1
    apply = {"input": request, "expected_plan_sha256": plan["plan_sha256"]}
    try:
        client.request("POST", "/checkpoint-evidence", apply)
        raise AssertionError("first committed response should be lost")
    except Unavailable as error:
        assert str(error) == "network_unavailable", error
    receipt = client.request("GET", "/checkpoint-evidence/" + request["uuid"])
    assert receipt == client.request("POST", "/checkpoint-evidence", apply)
    assert {key: receipt[key] for key in plan} == plan
    target = client.request("GET", "/enrichment-targets/" + plan["target"]["uuid"])
    assert target == plan["target"], (target, plan)
    collection = next(c for c in client.request("GET", "/collections") if c["uuid"] == target["collection_uuid"])
    definition = {k: v for k, v in collection.items() if k in {"label", "kind", "namespace", "target_url", "account_uuid", "root_uuid", "path_prefix"}}
    definition.update(state="active", expected_revision=collection["revision"], reason="Reviewed child retry destination")
    revised = client.request("PUT", "/collections/" + collection["uuid"], definition)
    handoff_input = {"uuid": str(uuid.uuid4()), "evidence_uuid": request["uuid"], "evidence_plan_sha256": plan["plan_sha256"],
                     "target_revision": target["revision"],
                     "collection_revision": revised["revision"], "policy_sha256": "a" * 64, "extractor_version": "1.32.15-dev"}
    handoff_plan = client.request("POST", "/checkpoint-handoffs/preview", handoff_input)
    assert handoff_plan["retained_capture_count"] == handoff_plan["pending_count"] == handoff_plan["unscoped_reference_count"] == 1
    assert handoff_plan["released_target_uuid"] != target["uuid"] and handoff_plan["released_revision"] == 1
    handoff_apply = {"input": handoff_input, "expected_plan_sha256": handoff_plan["plan_sha256"]}
    try:
        client.request("POST", "/checkpoint-handoffs", handoff_apply)
        raise AssertionError("committed handoff response should be lost")
    except Unavailable as error:
        assert str(error) == "network_unavailable", error
    handoff_path = "/checkpoint-handoffs/" + handoff_input["uuid"]
    handoff = client.request("GET", handoff_path)
    assert handoff == client.request("POST", "/checkpoint-handoffs", handoff_apply)
    assert {key: handoff[key] for key in handoff_plan} == handoff_plan
    seed = client.request("GET", handoff_path + "/seed")
    bundle = Bundle(seed["body"]["url"], handoff_input["extractor_version"], seed["body"])
    assert hashlib.sha256(encode(bundle.checkpoint(), 32 << 20)).hexdigest() == seed["sha256"] == handoff_plan["seed_sha256"]
    assert seed["body"]["records"][0]["patch"]["author"]["id"] == 9007199254740993
    assert seed["body"]["records"][0]["observed_at"] is None and seed["body"]["unresolved"] == []
    assert client.request("GET", "/enrichment-targets/" + target["uuid"]) == target
    assert source.read_bytes() == before
    print(json.dumps({"accepted": True, "handoff_reviewed": True, "held_for_review": True, "receipt_uuid": request["uuid"]}))


if __name__ == "__main__":
    run()
