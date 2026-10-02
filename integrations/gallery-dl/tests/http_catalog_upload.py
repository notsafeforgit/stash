"""Exercise resumable catalog upload against the native application API."""

from contextlib import closing
import json
from pathlib import Path
import sqlite3
import shutil
import sys
from unittest.mock import patch
import uuid

from http_catalog_registry_import import execute
from stash_ingest.catalog_identity_import import main as identity_main
from stash_ingest.catalog_registry_import import main as registry_main
from stash_ingest.catalog_snapshot import prepare
from stash_ingest.catalog_upload import main
from stash_ingest.catalog_evidence_import import CatalogEvidenceClient, main as evidence_main
from stash_ingest.catalog_relations_import import CatalogRelationsClient, main as relations_main
from stash_ingest.catalog_publisher_import import CatalogPublisherClient, main as publisher_main
from stash_ingest.catalog_attachment_import import CatalogAttachmentClient, main as attachment_main
from stash_ingest.catalog_media_import import main as media_main
from stash_ingest.catalog_membership_import import CatalogMembershipClient, main as membership_main
from stash_ingest.encoding import digest, encode
from test_catalog_registry_import import registry_fixture
from test_catalog_snapshot import CATALOG_ID, CAPTURED, catalog_fixture


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    registry_path = directory / "registry.sqlite"
    identities, _ = registry_fixture(registry_path)
    with closing(sqlite3.connect(registry_path)) as db, db:
        db.execute("INSERT INTO catalogs VALUES(?,?,?,?,?,?)", (CATALOG_ID, "collection", "Historical album", "directory:Historical album", CAPTURED, None))
        db.execute("INSERT INTO catalogs VALUES(?,?,?,?,?,?)", ("c_" + "2" * 32, "collection", "Copied evidence", "directory:Copied evidence", CAPTURED, None))
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
    with closing(sqlite3.connect(source)) as db, db:
        shared = json.loads(db.execute("SELECT payload_json FROM observations WHERE observation_id='shared'").fetchone()[0])
        shared.update(author_fullname="t2_http_publisher", author="ActualHTTPPublisher")
        shared["gallery_data"] = {"items": [{"media_id": "photo"}, {"media_id": "video"}]}
        shared["media_metadata"] = {"photo": {"e": "Image"}, "video": {"e": "RedditVideo"}}
        db.execute("UPDATE observations SET payload_json=? WHERE observation_id='shared'", (json.dumps(shared),))
        for i in range(2, 55):
            db.execute("INSERT INTO observation_details VALUES(?,?,?,?,?,?)",
                       (f"capture-{i:03}", "shared", CAPTURED, "1.32", json.dumps({"filename": str(i), "num": i + 1}), "[]"))
        unused = {"id": "unused", "name": "Original unreferenced profile"}
        db.execute("INSERT INTO account_snapshots VALUES(?,'reddit',?)",
                   (digest(encode(["source-account-snapshot-v1", "reddit", unused])), json.dumps(unused)))
        db.execute("CREATE TABLE IF NOT EXISTS post_aliases(alias_key TEXT PRIMARY KEY,post_key TEXT NOT NULL REFERENCES posts(post_key))")
        db.execute("INSERT INTO post_aliases VALUES('reddit:post:local-alias','reddit:post:album')")
        db.execute("INSERT INTO handles VALUES('reddit:handle:juniper','Juniper',?)", (CAPTURED,))
        for index in range(55):
            db.execute("INSERT INTO post_urls VALUES('reddit:post:album',?)", (f"https://example.test/album?copy={index:02}",))
            db.execute("INSERT INTO memberships VALUES('reddit:post:album',?,'collection',?)", (f"directory:Group {index:02}", f"Group {index:02}"))
            asset, path = f"path:{index}", f"absent/{index}.mp4"
            db.execute("INSERT INTO assets(asset_id,created_at) VALUES(?,?)", (asset, CAPTURED))
            db.execute("INSERT INTO files(relpath,asset_id,state,first_observed,role) VALUES(?,?,'missing',?,'local')", (path, asset, CAPTURED))
            db.execute("INSERT INTO appearances(post_key,attachment_key,asset_id,source_relpath) VALUES('reddit:post:album',?,?,?)", (path, asset, path))
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
    execute(evidence_main, args, 1)  # Mapping transaction committed; response lost.
    mapped = execute(evidence_main, args)
    assert mapped == execute(evidence_main, args)
    assert mapped["state"] == "mapped" and mapped["imported"] is False
    assert mapped["capture_mappings"] == 56 and mapped["profile_mappings"] == 2
    assert mapped["processed_records"] == mapped["source_records"] == 60
    execute(relations_main, args, 1)  # The first relationship batch committed; response lost.
    relationships = execute(relations_main, args, 2)
    assert relationships == execute(relations_main, args, 2)
    assert relationships["state"] == "review" and relationships["imported"] is False
    assert relationships["processed_records"] == relationships["source_records"] == 59
    assert relationships["mapped_records"] == 56 and relationships["review_records"] == 3
    execute(publisher_main, args, 1)  # The first publisher batch committed; response lost.
    publishers = execute(publisher_main, args)
    assert publishers == execute(publisher_main, args)
    assert publishers["state"] == "mapped" and publishers["imported"] is False
    assert publishers["processed_records"] == publishers["source_records"] == 56
    assert publishers["linked_records"] == 55 and publishers["unavailable_records"] == 1
    assert publishers["created_accounts"] == 1
    execute(attachment_main, args, 1)  # A committed attachment batch response is lost.
    attachments = execute(attachment_main, args)
    assert attachments == execute(attachment_main, args)
    assert attachments["state"] == "mapped" and attachments["imported"] is False
    assert attachments["processed_records"] == attachments["source_records"] == 56
    assert attachments["mapped_records"] == 55 and attachments["unavailable_records"] == 1
    assert attachments["changed_selections"] == 1
    media_args = [*args, "--root-uuid", setup["root_uuid"], "--root-revision", str(setup["root_revision"]),
                  "--collection-revision", "1", "--library-root-path", "/media"]
    execute(media_main, media_args, 1)  # The root mapping commits, but its response is lost.
    execute(media_main, media_args, 1)  # The first asset batch commits, but its response is lost.
    media = execute(media_main, media_args)
    assert media == execute(media_main, media_args)
    assert media["state"] == "mapped" and media["imported"] is False
    assert media["processed_records"] == media["source_records"] == 165
    assert media["mapped_records"] == 55 and media["unavailable_records"] == 110
    assert media["matched_files"] == media["media_associations"] == 0
    execute(membership_main, args, 1)  # Its first bounded transaction commits before connection loss.
    membership = execute(membership_main, args)
    assert membership == execute(membership_main, args)
    assert membership["state"] == "mapped" and membership["mapped_records"] == 55 and membership["imported"] is False
    assert source.read_bytes() == original

    # Another physical catalog retains copied events and one differing payload
    # under the same old capture ID. Only the complete copied events may coalesce.
    copied_source = directory / "copied.sqlite"
    shutil.copyfile(source, copied_source)
    with closing(sqlite3.connect(copied_source)) as db, db:
        db.execute("UPDATE catalog_info SET value=? WHERE key='id'", ("c_" + "2" * 32,))
        db.execute("UPDATE observation_details SET payload_patch=? WHERE capture_id='capture-0'",
                   (json.dumps({"filename": "different", "num": 1}),))
    copied_snapshot = str(uuid.uuid4())
    copied = prepare(copied_source, directory / "copied", copied_snapshot, setup["source"], CAPTURED)
    copied_args = ["--snapshot", str(directory / "copied"), "--endpoint", setup["endpoint"], "--expected-sha256", copied["manifest_sha256"]]
    execute(main, copied_args)
    assert execute(evidence_main, copied_args)["state"] == "mapped"
    assert execute(relations_main, copied_args, 2)["state"] == "review"
    copied_publishers = execute(publisher_main, copied_args)
    assert copied_publishers["linked_records"] == 1 and copied_publishers["preserved_records"] == 54
    assert copied_publishers["unavailable_records"] == 1 and copied_publishers["created_accounts"] == 0
    copied_attachments = execute(attachment_main, copied_args)
    assert copied_attachments["mapped_records"] == 55 and copied_attachments["unavailable_records"] == 1
    assert copied_attachments["changed_selections"] == 0
    assert execute(membership_main, copied_args)["mapped_records"] == 55
    memberships = CatalogMembershipClient(setup["endpoint"])
    original_members = memberships.request("GET", f"/{setup['snapshot']}/membership-import/records", None, prepared["manifest_sha256"], "application/json")
    copied_members = memberships.request("GET", f"/{copied_snapshot}/membership-import/records", None, copied["manifest_sha256"], "application/json")
    assert len(original_members) == len(copied_members) == 55
    assert {row["collection_uuid"] for row in original_members} == {row["collection_uuid"] for row in copied_members}
    assert {row["post_uuid"] for row in original_members} == {row["post_uuid"] for row in copied_members}
    assert not ({row["membership_uuid"] for row in original_members} & {row["membership_uuid"] for row in copied_members})
    client = CatalogEvidenceClient(setup["endpoint"])
    def capture_ids(snapshot_uuid, manifest_sha):
        rows = client.request("GET", f"/{snapshot_uuid}/evidence-import/records", None, manifest_sha, "application/json")
        return {(row["table"], row["key"]): row["capture_uuid"] for row in rows if "capture_uuid" in row}
    old, new = capture_ids(setup["snapshot"], prepared["manifest_sha256"]), capture_ids(copied_snapshot, copied["manifest_sha256"])
    assert len(old) == len(new) == 56
    assert sum(old[key] != new[key] for key in old) == 1
    assert old["observation_details", '["capture-0"]'] != new["observation_details", '["capture-0"]']
    relations = CatalogRelationsClient(setup["endpoint"])
    rows = relations.request("GET", f"/{setup['snapshot']}/relations-import/records", None, prepared["manifest_sha256"], "application/json")
    other_rows = relations.request("GET", f"/{copied_snapshot}/relations-import/records", None, copied["manifest_sha256"], "application/json")
    original_url = next(row for row in rows if row["table"] == "post_urls")
    copied_url = next(row for row in other_rows if row["table"] == "post_urls" and row["key"] == original_url["key"])
    assert original_url["post_uuid"] == copied_url["post_uuid"]
    assert original_url["url_evidence_uuid"] != copied_url["url_evidence_uuid"]
    detail = relations.request("GET", f"/{setup['snapshot']}/relations-import/records/{original_url['ordinal']}", None, prepared["manifest_sha256"], "application/json")
    assert detail["source_values"]["url"] == json.loads(original_url["key"])[1]
    publisher_client = CatalogPublisherClient(setup["endpoint"])
    publisher_rows = publisher_client.request("GET", f"/{setup['snapshot']}/publisher-import/records", None, prepared["manifest_sha256"], "application/json")
    assert len(publisher_rows) == 56
    linked = next(row for row in publisher_rows if row["outcome"] == "linked")
    detail = publisher_client.request("GET", f"/{setup['snapshot']}/publisher-import/records/{linked['ordinal']}", None, prepared["manifest_sha256"], "application/json")
    assert detail["context"]["namespace"] == "native:reddit"
    attachment_client = CatalogAttachmentClient(setup["endpoint"])
    attachment_rows = attachment_client.request("GET", f"/{setup['snapshot']}/attachment-import/records", None, prepared["manifest_sha256"], "application/json")
    assert len(attachment_rows) == 56
    mapped = [row for row in attachment_rows if row["outcome"] == "mapped"]
    assert len({row["manifest_uuid"] for row in mapped}) == len({row["selection_uuid"] for row in mapped}) == 1
    detail = attachment_client.request("GET", f"/{setup['snapshot']}/attachment-import/records/{mapped[0]['ordinal']}", None, prepared["manifest_sha256"], "application/json")
    assert detail["context"]["evidence_path"] == "/gallery_data/items"
    print(json.dumps({"records": prepared["records"], "chunks": prepared["chunks"], "source_unchanged": True, "imported": False}))


if __name__ == "__main__":
    run()
