import contextlib
import hashlib
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

from stash_archive.bundle import (FORMAT, export_archive, import_archive, summary,
                                  iter_artifacts, validate_manifest, verify_archive)
from stash_archive.cli import list_records, main
from stash_archive.storage import (CHUNK_SIZE, InvalidArchive, json_bytes,
                                   read_chunk, store_file)


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.database = self.root / "native ? library.sqlite"
        self.db = sqlite3.connect(self.database)
        self.addCleanup(self.db.close)
        self.db.executescript(f"""
            PRAGMA journal_mode=WAL;
            CREATE TABLE native_schema(singleton INTEGER PRIMARY KEY, lineage TEXT NOT NULL);
            INSERT INTO native_schema VALUES(1,'{FORMAT}');
            CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, dirty INTEGER NOT NULL);
            INSERT INTO schema_migrations VALUES(1000077,0);
            CREATE TABLE performers(id INTEGER PRIMARY KEY,name TEXT,uuid TEXT UNIQUE);
            CREATE TABLE redirects(old_uuid TEXT PRIMARY KEY,canonical_uuid TEXT REFERENCES performers(uuid));
            CREATE TABLE source_posts(uuid TEXT PRIMARY KEY,title TEXT);
            CREATE TABLE attachments(post TEXT REFERENCES source_posts(uuid),slot INTEGER,body BLOB, PRIMARY KEY(post,slot));
            CREATE TABLE blobs(checksum TEXT PRIMARY KEY,blob BLOB);
            CREATE TABLE pending_file_operations(uuid TEXT PRIMARY KEY,body BLOB);
            INSERT INTO performers VALUES(7,'Canonical','person-survivor');
            INSERT INTO performers VALUES(8,'Manual purchase','person-manual');
            INSERT INTO redirects VALUES('person-merged','person-survivor');
            INSERT INTO source_posts VALUES('post-1','An album');
        """)
        self.payload = bytes(range(256)) * 5000
        self.db.executemany("INSERT INTO attachments VALUES('post-1',?,?)", [(i, self.payload) for i in range(3)])
        self.db.execute("INSERT INTO pending_file_operations VALUES('interrupted',?)", (b'\x00original\xffjournal',))
        self.embedded = b"embedded original artwork"
        self.external = b"filesystem original artwork"
        self.external_md5 = hashlib.md5(self.external).hexdigest()
        self.db.executemany("INSERT INTO blobs VALUES(?,?)", [
            (hashlib.md5(self.embedded).hexdigest(), self.embedded), (self.external_md5, None)])
        self.db.commit()
        self.blobs = self.root / "originals"
        blob = self.blobs / self.external_md5[:2] / self.external_md5[2:4] / self.external_md5
        blob.parent.mkdir(parents=True)
        blob.write_bytes(self.external)
        self.outbox = self.root / "outbox.sqlite"
        with contextlib.closing(sqlite3.connect(self.outbox)) as outbox:
            outbox.executescript("""
                PRAGMA application_id=1398032719;
                PRAGMA user_version=15;
                CREATE TABLE binding(id INTEGER PRIMARY KEY,endpoint TEXT,producer TEXT);
                INSERT INTO binding VALUES(1,'https://stash.example','11111111-1111-4111-8111-111111111111');
                CREATE TABLE events(uuid TEXT PRIMARY KEY,body BLOB,state TEXT);
                INSERT INTO events VALUES('unacknowledged',X'00FF01','pending');
            """)
        self.config = self.root / "config.yml"
        self.config.write_bytes(b"database: native.sqlite\nprivate_setting: exact original bytes\n")
        self.output = self.root / "export"

    def export(self, output=None):
        return export_archive(self.database, output or self.output, blob_paths=[self.blobs], reserve=0,
                              components=[{"role": "producer_outbox", "name": "worker.sqlite", "path": self.outbox},
                                          {"role": "config", "name": "config.yml", "path": self.config}])

    def rewrite_inventory(self, manifest, entries):
        changed = dict(manifest)
        body = b"".join(json_bytes(entry) for entry in entries)
        changed["inventory"] = {"sha256": hashlib.sha256(body).hexdigest(), "size": len(body),
                                "count": len(entries), "total_bytes": sum(e["size"] for e in entries)}
        (self.output / "artifacts.jsonl").write_bytes(body)
        (self.output / "manifest.json").write_bytes(json_bytes(changed))
        return changed

    def test_wal_snapshot_empty_restore_and_offline_access(self):
        manifest = self.export()
        self.db.execute("UPDATE performers SET name='Later live edit' WHERE id=7")
        self.db.commit()
        restored = self.root / "new-installation"
        import_archive(self.output, restored, reserve=0)
        with contextlib.closing(sqlite3.connect(restored / "library.sqlite")) as db:
            self.assertEqual(db.execute("SELECT name FROM performers WHERE id=7").fetchone(), ("Canonical",))
            self.assertEqual(db.execute("SELECT * FROM redirects").fetchall(), [("person-merged", "person-survivor")])
            self.assertEqual(db.execute("SELECT body FROM attachments ORDER BY slot").fetchall(), [(self.payload,)] * 3)
            self.assertEqual(db.execute("SELECT body FROM pending_file_operations").fetchone()[0], b'\x00original\xffjournal')
        with contextlib.closing(sqlite3.connect(restored / "components/producer_outbox/worker.sqlite")) as outbox:
            self.assertEqual(outbox.execute("SELECT body,state FROM events").fetchall(), [(b'\x00\xff\x01', "pending")])
        self.assertEqual((restored / "components/config/config.yml").read_bytes(), self.config.read_bytes())
        self.assertEqual((restored / "blobs" / self.external_md5[:2] / self.external_md5[2:4] / self.external_md5).read_bytes(), self.external)
        self.assertEqual(json.loads((restored / "restore.json").read_bytes())["archive_uuid"], manifest["uuid"])
        page = list_records(restored, "performers", limit=1)
        self.assertEqual(page["next"], [7])
        self.assertEqual(list_records(restored, "performers", after=page["next"])["rows"][0]["name"], "Manual purchase")
        self.assertEqual(list_records(restored, "attachments", limit=1)["next"], ["post-1", 0])
        self.assertFalse(summary(self.output)["contents_verified"])
        self.assertEqual(verify_archive(self.output, temp_parent=self.root, reserve=0)["uuid"], manifest["uuid"])

    def test_deterministic_chunks_reuse_unchanged_contents(self):
        first = self.export()
        second = self.export(self.root / "second")
        a = next(e for e in iter_artifacts(self.output, first) if e["role"] == "library")
        b = next(e for e in iter_artifacts(self.root / "second", second) if e["role"] == "library")
        self.assertEqual(a, b)
        self.assertGreater(len(a["chunks"]), 1)
        self.assertLess(summary(self.output)["compressed_bytes"], summary(self.output)["uncompressed_bytes"])

    def test_corruption_missing_objects_and_decompression_limits_fail_closed(self):
        manifest = self.export()
        chunk = next(iter_artifacts(self.output, manifest))["chunks"][0]
        path = self.output / "objects" / (chunk["sha256"] + ".gz")
        original = path.read_bytes()
        for changed in (original[:-1], b"bad gzip", original + b"extra"):
            path.write_bytes(changed)
            with self.assertRaises(InvalidArchive): import_archive(self.output, self.root / "failed", reserve=0)
            self.assertFalse((self.root / "failed").exists())
        path.write_bytes(original)
        with self.assertRaises(InvalidArchive): read_chunk(self.output, dict(chunk, size=1))
        with self.assertRaises(InvalidArchive): read_chunk(self.output, dict(chunk, size=CHUNK_SIZE + 1))
        path.unlink()
        with self.assertRaises(FileNotFoundError): verify_archive(self.output, temp_parent=self.root, reserve=0)

    def test_missing_original_foreign_dirty_and_inconsistent_databases_refused(self):
        self.config.unlink()
        with self.assertRaises(FileNotFoundError): self.export()
        self.assertFalse(self.output.exists())
        self.config.write_bytes(b"restored")
        for statement in ("UPDATE schema_migrations SET dirty=1", "UPDATE native_schema SET lineage='foreign'"):
            self.db.execute("BEGIN")
            self.db.execute(statement)
            self.db.commit()
            with self.assertRaises(InvalidArchive): self.export()
            self.assertFalse(self.output.exists())
            self.db.execute("UPDATE schema_migrations SET dirty=0")
            self.db.execute("UPDATE native_schema SET lineage=?", (FORMAT,))
            self.db.commit()
        blob = self.blobs / self.external_md5[:2] / self.external_md5[2:4] / self.external_md5
        blob.unlink()
        with self.assertRaisesRegex(InvalidArchive, "unavailable"): self.export()
        self.assertFalse(self.output.exists())

    def test_existing_destinations_and_symlinks_are_never_overwritten(self):
        manifest = self.export()
        before = (self.output / "manifest.json").read_bytes()
        with self.assertRaises(FileExistsError): self.export()
        self.assertEqual(before, (self.output / "manifest.json").read_bytes())
        destination = self.root / "existing"
        destination.mkdir()
        (destination / "keep").write_text("keep")
        with self.assertRaises(FileExistsError): import_archive(self.output, destination, reserve=0)
        self.assertEqual((destination / "keep").read_text(), "keep")
        chunk = next(iter_artifacts(self.output, manifest))["chunks"][0]
        target = self.output / "objects" / (chunk["sha256"] + ".gz")
        saved = self.root / "saved"
        target.rename(saved)
        target.symlink_to(saved)
        with self.assertRaises(OSError): import_archive(self.output, self.root / "failed", reserve=0)
        self.assertTrue(saved.exists())

    def test_publication_failure_has_no_success_manifest(self):
        from stash_archive import bundle
        original = bundle.publish_bytes
        def fail_seal(path, body):
            if path.name == "manifest.json": raise OSError("simulated fsync/publication failure")
            return original(path, body)
        with patch.object(bundle, "publish_bytes", fail_seal), self.assertRaises(OSError): self.export()
        self.assertFalse(self.output.exists())

    def test_artifact_inventory_and_manifest_validation(self):
        manifest = self.export()
        entries = list(iter_artifacts(self.output, manifest))
        for value in (2, True, "1"):
            with self.assertRaises(InvalidArchive): validate_manifest(dict(manifest, version=value))
        for name in ("../escape", "/absolute", "..", "folder/name"):
            changed = json.loads(json_bytes(entries))
            changed[-1]["name"] = name
            with self.assertRaises(InvalidArchive):
                list(iter_artifacts(self.output, self.rewrite_inventory(manifest, changed)))
        self.rewrite_inventory(manifest, [e for e in entries if e["role"] != "blob"])
        with self.assertRaisesRegex(InvalidArchive, "inventory"): import_archive(self.output, self.root / "failed", reserve=0)

    def test_inventory_is_checked_before_destination_creation(self):
        manifest = self.export()
        inventory = (self.output / "artifacts.jsonl").read_bytes()
        entries = list(iter_artifacts(self.output, manifest))
        destination = self.root / "failed"
        for body in (inventory[:-1], inventory + inventory, inventory.replace(b'"role":', b'"role":"config","role":', 1)):
            with self.subTest(body_length=len(body)):
                (self.output / "artifacts.jsonl").write_bytes(body)
                with self.assertRaises(InvalidArchive): import_archive(self.output, destination, reserve=0)
                self.assertFalse(destination.exists())
        for changed in (entries + [entries[-1]], [e for e in entries if e["role"] != "library"]):
            self.rewrite_inventory(manifest, changed)
            with self.assertRaises(InvalidArchive): import_archive(self.output, destination, reserve=0)
            self.assertFalse(destination.exists())

    def test_chunk_order_and_database_identity_are_verified(self):
        manifest = self.export()
        entries = list(iter_artifacts(self.output, manifest))
        changed = json.loads(json_bytes(entries))
        library = next(e for e in changed if e["role"] == "library")
        library["chunks"].reverse()
        self.rewrite_inventory(manifest, changed)
        with self.assertRaisesRegex(InvalidArchive, "chunk order"):
            import_archive(self.output, self.root / "failed", reserve=0)
        self.assertFalse((self.root / "failed").exists())
        changed = json.loads(json_bytes(entries))
        producer = next(e for e in changed if e["role"] == "producer_outbox")
        producer["sqlite"]["producer_uuid"] = "22222222-2222-4222-8222-222222222222"
        self.rewrite_inventory(manifest, changed)
        with self.assertRaisesRegex(InvalidArchive, "identity differs"):
            import_archive(self.output, self.root / "failed", reserve=0)
        self.assertFalse((self.root / "failed").exists())

    def test_foreign_keys_and_producer_binding_rejected(self):
        self.db.execute("INSERT INTO redirects VALUES('broken','missing')")
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "foreign key"):
            self.export()
        self.assertFalse(self.output.exists())
        self.db.execute("DELETE FROM redirects WHERE old_uuid='broken'")
        self.db.commit()
        for statement in ("PRAGMA application_id=1", "UPDATE binding SET producer='not-a-uuid'",
                          "UPDATE binding SET endpoint='https://username:secret@stash.example'",
                          "DELETE FROM binding"):
            with self.subTest(statement=statement), contextlib.closing(sqlite3.connect(self.outbox)) as db:
                db.execute(statement)
                db.commit()
                with self.assertRaises(InvalidArchive): self.export()
                self.assertFalse(self.output.exists())
                db.execute("PRAGMA application_id=1398032719")
                db.execute("DELETE FROM binding")
                db.execute("INSERT INTO binding VALUES(1,'https://stash.example','11111111-1111-4111-8111-111111111111')")
                db.commit()

    def test_source_changed_during_packing_is_refused(self):
        from stash_archive import storage
        target = self.root / "packing"
        (target / "objects").mkdir(parents=True)
        publish = storage.publish_bytes
        def change_source(path, body):
            publish(path, body)
            self.config.write_bytes(b"concurrent configuration change")
        with patch.object(storage, "publish_bytes", change_source):
            with self.assertRaisesRegex(InvalidArchive, "changed while being read"):
                store_file(target, self.config, reserve=0)

    def test_offline_listing_bounds_binary_output(self):
        self.db.execute("INSERT INTO attachments VALUES('post-1',3,?)", (b'x' * (7 << 20),))
        self.db.commit()
        self.export()
        restored = self.root / "restored"
        import_archive(self.output, restored, reserve=0)
        self.assertEqual(len(list_records(restored, "attachments")["rows"]), 4)
        with self.assertRaisesRegex(InvalidArchive, "offline list limit"):
            list_records(restored, "attachments", columns=["body"], after=["post-1", 2])

    def test_cli_inspect_and_space_reservation(self):
        self.export()
        output = io.StringIO()
        with contextlib.redirect_stdout(output): main(["inspect", str(self.output)])
        self.assertEqual(json.loads(output.getvalue())["coverage"], "declared-components")
        self.assertNotIn("private_setting", output.getvalue())
        with self.assertRaises(InvalidArchive): import_archive(self.output, self.root / "failed", reserve=1 << 100)
        self.assertFalse((self.root / "failed").exists())


if __name__ == "__main__":
    unittest.main()
