from contextlib import closing, redirect_stderr, redirect_stdout
import hashlib
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_snapshot import main, prepare, verify
from stash_ingest.catalog_source import CatalogSource
from stash_ingest.encoding import InvalidData, encode


CATALOG_ID = "c_" + "1" * 32
CAPTURED = "2026-09-29T12:00:00.123456+00:00"
PROFILE = {"id": "9007199254740993", "name": "Juniper 🌿"}
PROFILE_ID = hashlib.sha256(encode(["source-account-snapshot-v1", "reddit", PROFILE])).hexdigest()
SIDECAR = b"original\x00\xffdocument"


def catalog_fixture(path, version=3, normalized=True):
    """Independent SQL fixture covering the historical sidecar/profile layouts."""
    refs = ",account_refs_json TEXT NOT NULL DEFAULT '[]'" if version == 3 else ""
    with closing(sqlite3.connect(path)) as db, db:
        db.executescript("""
            PRAGMA application_id=1396920387;
            CREATE TABLE catalog_info(key TEXT PRIMARY KEY,value TEXT NOT NULL);
            CREATE TABLE accounts(account_key TEXT PRIMARY KEY,platform TEXT NOT NULL,source_id TEXT,identity_basis TEXT NOT NULL,created_at TEXT NOT NULL);
            CREATE TABLE handles(account_key TEXT NOT NULL REFERENCES accounts(account_key),handle TEXT NOT NULL,first_observed TEXT NOT NULL,PRIMARY KEY(account_key,handle));
            CREATE TABLE posts(post_key TEXT PRIMARY KEY,platform TEXT NOT NULL,source_id TEXT,account_key TEXT REFERENCES accounts(account_key),identity_basis TEXT NOT NULL,created_at TEXT NOT NULL);
            CREATE TABLE post_urls(post_key TEXT NOT NULL REFERENCES posts(post_key),url TEXT NOT NULL,PRIMARY KEY(post_key,url));
            CREATE TABLE assets(asset_id TEXT PRIMARY KEY,digest_algorithm TEXT,digest TEXT,byte_size INTEGER,created_at TEXT NOT NULL);
            CREATE TABLE files(relpath TEXT PRIMARY KEY,asset_id TEXT NOT NULL REFERENCES assets(asset_id),state TEXT NOT NULL,byte_size INTEGER,mtime_ns INTEGER,first_observed TEXT NOT NULL,survivor_relpath TEXT,role TEXT NOT NULL DEFAULT 'local');
            CREATE TABLE appearances(post_key TEXT NOT NULL REFERENCES posts(post_key),attachment_key TEXT NOT NULL,asset_id TEXT NOT NULL REFERENCES assets(asset_id),source_media_id TEXT,position INTEGER,source_relpath TEXT,PRIMARY KEY(post_key,attachment_key,asset_id));
            CREATE TABLE memberships(post_key TEXT NOT NULL REFERENCES posts(post_key),collection_key TEXT NOT NULL,kind TEXT NOT NULL,label TEXT NOT NULL,PRIMARY KEY(post_key,collection_key));
            CREATE TABLE translations(translation_id TEXT PRIMARY KEY,post_key TEXT NOT NULL REFERENCES posts(post_key),input_hash TEXT,original_text TEXT,translated_text TEXT NOT NULL,source_language TEXT,target_language TEXT,provider TEXT,provenance TEXT NOT NULL,captured_at TEXT NOT NULL);
            CREATE TABLE dedupe_events(event_id TEXT PRIMARY KEY,asset_id TEXT NOT NULL REFERENCES assets(asset_id),paths_json TEXT NOT NULL,survivor_relpath TEXT,stage TEXT NOT NULL,created_at TEXT NOT NULL);
        """)
        db.execute(f"PRAGMA user_version={version}")
        db.execute("CREATE TABLE observations(observation_id TEXT PRIMARY KEY,post_key TEXT NOT NULL REFERENCES posts(post_key),origin TEXT NOT NULL,captured_at TEXT NOT NULL,payload_json TEXT NOT NULL,title TEXT,original_text TEXT,published_at TEXT,date_basis TEXT,language TEXT,extractor_version TEXT" + refs + ")")
        db.execute("CREATE TABLE observation_details(capture_id TEXT PRIMARY KEY,observation_id TEXT NOT NULL REFERENCES observations(observation_id) ON DELETE CASCADE,captured_at TEXT NOT NULL,extractor_version TEXT,payload_patch TEXT NOT NULL" + refs + ")")
        db.execute("CREATE INDEX observation_details_parent ON observation_details(observation_id)")
        if normalized:
            db.executescript("""
                CREATE TABLE sidecar_documents(document_id INTEGER PRIMARY KEY,content_sha256 TEXT NOT NULL,raw_content BLOB NOT NULL,encoding TEXT NOT NULL,parse_status TEXT NOT NULL,warnings_json TEXT NOT NULL,parsed_json TEXT NOT NULL,UNIQUE(content_sha256,encoding,parse_status,warnings_json,parsed_json));
                CREATE TABLE sidecar_sources(relpath TEXT NOT NULL,content_sha256 TEXT NOT NULL,document_id INTEGER NOT NULL REFERENCES sidecar_documents(document_id),post_key TEXT REFERENCES posts(post_key),captured_at TEXT NOT NULL,PRIMARY KEY(relpath,content_sha256));
                CREATE VIEW sidecars AS SELECT s.relpath,s.content_sha256,d.raw_content,d.encoding,d.parse_status,d.warnings_json,d.parsed_json,s.post_key,s.captured_at FROM sidecar_sources s JOIN sidecar_documents d USING(document_id);
            """)
            db.execute("INSERT INTO sidecar_documents VALUES(1,?,?,?,?,?,?)", (hashlib.sha256(SIDECAR).hexdigest(), SIDECAR, "unknown", "invalid", '["invalid encoding"]', '{}'))
            for name in ("one.nfo", "two.nfo"):
                db.execute("INSERT INTO sidecar_sources VALUES(?,?,1,NULL,?)", (name, hashlib.sha256(SIDECAR).hexdigest(), CAPTURED))
        else:
            db.execute("CREATE TABLE sidecars(relpath TEXT NOT NULL,content_sha256 TEXT NOT NULL,raw_content BLOB NOT NULL,encoding TEXT NOT NULL,parse_status TEXT NOT NULL,warnings_json TEXT NOT NULL,parsed_json TEXT NOT NULL,post_key TEXT REFERENCES posts(post_key),captured_at TEXT NOT NULL,PRIMARY KEY(relpath,content_sha256))")
            db.execute("INSERT INTO sidecars VALUES(?,?,?,?,?,?,?,?,?)", ("one.nfo", hashlib.sha256(SIDECAR).hexdigest(), SIDECAR, "unknown", "invalid", '["invalid encoding"]', '{}', None, CAPTURED))
        db.executemany("INSERT INTO catalog_info VALUES(?,?)", [("id", CATALOG_ID), ("schema_version", str(version)), ("path_base", "media-root-relative"), ("kind", "creator"), ("label", "Juniper")])
        db.execute("INSERT INTO accounts VALUES('reddit:handle:juniper','reddit',NULL,'handle',?)", (CAPTURED,))
        db.execute("INSERT INTO posts VALUES('reddit:post:album','reddit','album','reddit:handle:juniper','source',?)", (CAPTURED,))
        payload = {"category": "reddit", "id": "album", "title": "A day outside", "user": None if version == 3 else PROFILE}
        row = ["shared", "reddit:post:album", "gallery-dl", CAPTURED, json.dumps(payload), "A day outside", None, "2026-09-01", "source", None, "1.32"]
        if version == 3:
            db.execute("CREATE TABLE account_snapshots(snapshot_id TEXT PRIMARY KEY,platform TEXT NOT NULL,payload_json TEXT NOT NULL) WITHOUT ROWID")
            db.execute("INSERT INTO account_snapshots VALUES(?,'reddit',?)", (PROFILE_ID, json.dumps(PROFILE)))
            row.append(json.dumps([[["user"], PROFILE_ID]]))
        db.execute("INSERT INTO observations VALUES(" + ",".join("?" for _ in row) + ")", row)
        row[0], row[2], row[4] = "flat", "legacy-nfo", '{"nfo_fields":{"title":["Original title"]}}'
        if version == 3: row[-1] = "[]"
        db.execute("INSERT INTO observations VALUES(" + ",".join("?" for _ in row) + ")", row)
        for i in range(2):
            row = ["capture-" + str(i), "shared", CAPTURED, "1.32", json.dumps({"filename": str(i), "num": i+1})]
            if version == 3: row.append("[]")
            db.execute("INSERT INTO observation_details VALUES(" + ",".join("?" for _ in row) + ")", row)


class CatalogSourceTests(unittest.TestCase):
    def test_historical_versions_share_profiles_without_extra_parent_captures(self):
        for version, normalized in ((1, False), (2, True), (3, True)):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "catalog.sqlite"
                catalog_fixture(path, version, normalized)
                before = path.read_bytes()
                with CatalogSource(path) as source:
                    source.check_integrity()
                    captures = list(source.captures())
                    inventory = source.capture_inventory()
                    for table in source.tables:
                        for row in source.rows(table): source.validate_row(table, row)
                self.assertEqual(3, inventory["count"])
                self.assertEqual(1, inventory["flat_observations"])
                self.assertEqual({"flat", "capture-0", "capture-1"}, {c["capture_id"] for c in captures})
                shared = [c for c in captures if not c["flat_observation"]]
                self.assertEqual(PROFILE, shared[0]["payload"]["user"])
                self.assertEqual(PROFILE, shared[1]["payload"]["user"])
                self.assertEqual(["0", "1"], [c["payload"]["filename"] for c in shared])
                shared[0]["payload"]["user"]["name"] = "Changed"
                self.assertEqual(PROFILE, shared[1]["payload"]["user"])
                self.assertEqual(before, path.read_bytes())

    def test_corruption_and_unknown_shapes_fail_without_source_changes(self):
        changes = [
            "PRAGMA application_id=0", "PRAGMA user_version=4", "CREATE TABLE unexpected(id TEXT)",
            "ALTER TABLE posts ADD COLUMN unknown TEXT", "CREATE VIEW extra AS SELECT * FROM posts",
            "DROP TABLE sidecar_sources", "UPDATE sidecar_documents SET raw_content=X'00'",
            "UPDATE account_snapshots SET payload_json='{}'", "DELETE FROM account_snapshots",
            "UPDATE observations SET payload_json='{\"x\":1,\"x\":2}' WHERE observation_id='shared'",
            "UPDATE observations SET captured_at='2026-01-01 01:02:03' WHERE observation_id='flat'",
            "UPDATE observation_details SET observation_id='missing'",
        ]
        for change in changes:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "catalog.sqlite"
                catalog_fixture(path)
                with closing(sqlite3.connect(path)) as db, db: db.execute(change)
                before = path.read_bytes()
                with self.assertRaises(InvalidData):
                    with CatalogSource(path) as source:
                        source.check_integrity()
                        for table in source.tables:
                            for row in source.rows(table): source.validate_row(table, row)
                        source.capture_inventory()
                self.assertEqual(before, path.read_bytes())

    def test_nested_profile_references_and_reddit_parent_patch_are_lossless(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.sqlite"
            catalog_fixture(path)
            shared = {"_reddit": {"category": "reddit", "id": "album", "items": [{"user": None}]}}
            patch_payload = {"_reddit": {"num": 2}, "filename": "nested"}
            with closing(sqlite3.connect(path)) as db, db:
                db.execute("UPDATE observations SET payload_json=?,account_refs_json=? WHERE observation_id='shared'", (json.dumps(shared), json.dumps([[["_reddit", "items", 0, "user"], PROFILE_ID]])))
                db.execute("UPDATE observation_details SET payload_patch=?", (json.dumps(patch_payload),))
            with CatalogSource(path) as source:
                values = [row for row in source.captures() if not row["flat_observation"]]
            self.assertEqual({"_reddit": {"category": "reddit", "id": "album", "items": [{"user": PROFILE}], "num": 2}, "filename": "nested"}, values[0]["payload"])
            for bad in ([[["_reddit", "items", -1, "user"], PROFILE_ID]], [[["_reddit", "id"], PROFILE_ID]], [[["_reddit", "items", 0, "user"], PROFILE_ID]] * 2):
                with closing(sqlite3.connect(path)) as db, db:
                    db.execute("UPDATE observations SET account_refs_json=? WHERE observation_id='shared'", (json.dumps(bad),))
                with CatalogSource(path) as source, self.assertRaises(InvalidData): source.capture_inventory()


class CatalogSnapshotTests(unittest.TestCase):
    def test_bounded_deterministic_snapshot_preserves_binary_and_physical_rows(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "catalog.sqlite"
            catalog_fixture(path)
            before = path.read_bytes()
            snapshot, source = str(uuid.uuid4()), str(uuid.uuid4())
            with patch("stash_ingest.catalog_snapshot.MAX_CHUNK_ROWS", 2):
                first = prepare(path, root / "first", snapshot, source, CAPTURED)
                again = prepare(path, root / "again", snapshot, source, CAPTURED)
                self.assertEqual(first, again)
                self.assertEqual(first, verify(root / "first", first["manifest_sha256"]))
            self.assertGreater(first["chunks"], 1)
            self.assertFalse(first["imported"])
            self.assertEqual(3, first["captures"])
            self.assertEqual(before, path.read_bytes())
            manifest = json.loads((root / "first/manifest.json").read_text())
            self.assertNotIn("sidecars", manifest["tables"])
            self.assertEqual(1, manifest["tables"]["account_snapshots"]["rows"])
            self.assertEqual(2, manifest["tables"]["observations"]["rows"])
            rows = [json.loads(line) for chunk in manifest["chunks"] for line in (root / "first" / chunk["file"]).read_bytes().splitlines()]
            blob = next(row["values"]["raw_content"] for row in rows if row["table"] == "sidecar_documents")
            self.assertEqual({"sqlite_blob_base64": "b3JpZ2luYWwA/2RvY3VtZW50"}, blob)
            self.assertEqual(0o700, (root / "first").stat().st_mode & 0o777)
            self.assertTrue(all(file.stat().st_mode & 0o777 == 0o600 for file in (root / "first").iterdir()))
            with self.assertRaises(InvalidData): prepare(path, root / "first", snapshot, source, CAPTURED)
            self.assertEqual(first, verify(root / "first"))

    def test_failed_preparation_does_not_publish_and_verification_detects_tampering(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "catalog.sqlite"
            catalog_fixture(path)
            args = (path, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
            with patch.object(CatalogSource, "capture_inventory", side_effect=InvalidData("fixture error")), self.assertRaises(InvalidData):
                prepare(*args)
            self.assertFalse((root / "snapshot").exists())
            self.assertEqual(["catalog.sqlite"], sorted(child.name for child in root.iterdir()))
            result = prepare(*args)
            with self.assertRaises(InvalidData): verify(root / "snapshot", "0" * 64)
            chunk = root / "snapshot/records-000000.jsonl"
            original = chunk.read_bytes()
            chunk.write_bytes(original.replace(b"Juniper", b"Junipxx"))
            with self.assertRaises(InvalidData): verify(root / "snapshot", result["manifest_sha256"])
            chunk.write_bytes(original)
            (root / "snapshot/extra").write_text("unexpected")
            with self.assertRaises(InvalidData): verify(root / "snapshot")

    def test_cli_requires_frozen_identity_and_never_reports_import_success(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "catalog.sqlite"
            catalog_fixture(path)
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(0, main(["--catalog", str(path), "--output", str(root / "snapshot"), "--snapshot", str(uuid.uuid4()), "--source", str(uuid.uuid4()), "--captured-at", CAPTURED]))
            result = json.loads(output.getvalue())
            self.assertTrue(result["prepared"])
            self.assertFalse(result["imported"])
            with redirect_stdout(io.StringIO()): self.assertEqual(0, main(["--verify", str(root / "snapshot"), "--expected-sha256", result["manifest_sha256"]]))
            with redirect_stderr(io.StringIO()): self.assertEqual(1, main(["--verify", str(root / "snapshot"), "--snapshot", str(uuid.uuid4())]))

    def test_lost_publication_acknowledgement_keeps_a_verifiable_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "catalog.sqlite"
            catalog_fixture(path)
            with patch("stash_ingest.catalog_snapshot.sync_directory", side_effect=[None, OSError("parent fsync failed")]), self.assertRaises(OSError):
                prepare(path, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
            result = verify(root / "snapshot")
            self.assertTrue(result["prepared"])
            self.assertFalse(result["imported"])
            self.assertFalse(list(root.glob(".stash-catalog-snapshot-*")))

    def test_manifest_and_chunk_boundaries_are_validated(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "catalog.sqlite"
            catalog_fixture(path)
            prepare(path, root / "snapshot", str(uuid.uuid4()), str(uuid.uuid4()), CAPTURED)
            file = root / "snapshot/manifest.json"
            original = json.loads(file.read_text())
            changes = [
                lambda value: value["tables"].update({"unknown": value["tables"]["post_urls"]}),
                lambda value: value["chunks"][0].update({"file": "../records.jsonl"}),
                lambda value: value["chunks"][0].update({"rows": 1001}),
                lambda value: value["tables"]["observations"].update({"rows": 0}),
                lambda value: value["captures"].update({"count": 999}),
            ]
            for change in changes:
                value = json.loads(json.dumps(original))
                change(value)
                file.write_bytes(encode(value))
                with self.assertRaises(InvalidData): verify(root / "snapshot")
            file.write_bytes(encode(original))
            chunk = root / "snapshot/records-000000.jsonl"
            outside = root / "outside.jsonl"
            chunk.rename(outside)
            chunk.symlink_to(outside)
            with self.assertRaises(InvalidData): verify(root / "snapshot")


if __name__ == "__main__":
    unittest.main()
