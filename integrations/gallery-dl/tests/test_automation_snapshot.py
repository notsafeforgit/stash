from contextlib import closing, redirect_stderr, redirect_stdout
import copy
import io
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.automation_snapshot import main, prepare, validate_manifest, validate_record, verify
from stash_ingest.automation_source import AutomationSource
from stash_ingest.encoding import InvalidData, digest


SNAPSHOT = "b4d46dc5-c44d-4d4e-b17c-cb7f5cec6c01"
SOURCE = "a6f3cbea-bbe6-4ad7-9886-1db352983d21"
CAPTURED = "2026-10-02T14:06:29.556558+00:00"


def automation_fixture(path, empty=False):
    with closing(sqlite3.connect(path)) as db, db:
        db.executescript((Path(__file__).parent / "fixtures/automation-schema.sql").read_text())
        if empty:
            return
        db.execute("INSERT INTO maintenance VALUES(?,?)", ("binary cursor", b"\x00\xfforiginal"))
        db.execute("INSERT INTO translation_jobs VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
                   ("cached", "Exact original 🌿\n", "en", 25, None, "done", '{"unfinished":', 3, 1790949970.125, "legacy error", CAPTURED, CAPTURED))
        db.execute("INSERT INTO translation_jobs VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
                   ("pending", "Second", "en", 100, None, "pending", None, 0, 0, None, CAPTURED, CAPTURED))
        db.executemany("INSERT INTO translation_targets VALUES(?,?,?,?,?)",
                       [("cached", "catalog", "reddit:post:one", "title", 1),
                        ("pending", "catalog", "reddit:post:one", "caption", 0),
                        ("orphan", "catalog", "reddit:post:missing", "title", 0)])
        for version in (10, 2):
            db.execute("INSERT INTO enrichment_jobs VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                       ("catalog", "reddit:post:one", version, "reddit", "reddit:id:account", None,
                        "retry", 0, (1 << 63) - 1, 1790949970.125, None, '  {"a":1, "a":2}  ', CAPTURED, CAPTURED))
        db.execute("INSERT INTO enrichment_cooldowns VALUES('reddit',1790949970.875,'source unavailable')")
        db.execute("INSERT INTO enrichment_seed_progress VALUES('catalog','last',0,'{broken')")
        db.execute("INSERT INTO enrichment_source_progress VALUES('reddit',1790949970.125)")
        db.execute("INSERT INTO discovery_accounts VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
                   ("discovery", "reddit", "reddit:id:account", "https://example.invalid/profile", "retry", None, None,
                    2, 3, 1790949970.25, "legacy failure", CAPTURED, CAPTURED))
        db.execute("INSERT INTO discovery_targets VALUES('catalog','reddit:post:one','discovery','{}','pending')")
        db.execute("INSERT INTO discovery_candidates VALUES('catalog','reddit:post:one','https://example.invalid/post','source','{}')")


def snapshot_rows(directory):
    manifest = json.loads((directory / "manifest.json").read_bytes())
    return [json.loads(line) for chunk in manifest["chunks"] for line in (directory / chunk["file"]).read_bytes().splitlines()]


class AutomationSnapshotTests(unittest.TestCase):
    def test_bounded_repeatable_lossless_preparation_and_go_fixture(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "automation.sqlite"
            automation_fixture(source)
            before = source.read_bytes()
            with patch("stash_ingest.catalog_snapshot.MAX_CHUNK_ROWS", 2):
                first = prepare(source, root / "first", SNAPSHOT, SOURCE, CAPTURED)
                again = prepare(source, root / "again", SNAPSHOT, SOURCE, CAPTURED)
            self.assertEqual(first, again)
            self.assertEqual(first, verify(root / "first", first["manifest_sha256"]))
            self.assertEqual(digest(before), first["source_sha256"])
            self.assertEqual(before, source.read_bytes())
            self.assertEqual(10, len(first["pending_families"]))
            self.assertFalse(first["imported"])
            self.assertGreater(first["chunks"], 1)
            manifest = json.loads((root / "first/manifest.json").read_bytes())
            self.assertEqual(1, manifest["integrity"]["foreign_key_violations"])
            rows = snapshot_rows(root / "first")
            cached = next(r["values"] for r in rows if r["table"] == "translation_jobs" and r["key"] == ["cached"])
            self.assertEqual('{"unfinished":', cached["result_json"])
            self.assertEqual("Exact original 🌿\n", cached["original_text"])
            self.assertEqual(1790949970.125, cached["next_attempt"])
            enrichments = [r["values"] for r in rows if r["table"] == "enrichment_jobs"]
            self.assertEqual([2, 10], [r["version"] for r in enrichments])
            self.assertTrue(all(r["staged_json"] == '  {"a":1, "a":2}  ' for r in enrichments))
            self.assertTrue(all(r["attempts"] == (1 << 63) - 1 for r in enrichments))
            self.assertEqual({"sqlite_blob_base64": "AP9vcmlnaW5hbA=="}, next(r["values"]["value"] for r in rows if r["table"] == "maintenance"))
            self.assertEqual(0o700, (root / "first").stat().st_mode & 0o777)
            self.assertTrue(all(p.stat().st_mode & 0o777 == 0o600 for p in (root / "first").iterdir()))
            self.assertEqual({"automation.sqlite", "first", "again"}, {p.name for p in root.iterdir()})
            with self.assertRaises(InvalidData):
                prepare(source, root / "first", SNAPSHOT, SOURCE, CAPTURED)
        fixture = Path(__file__).resolve().parents[3] / "pkg/scrape/testdata/automation_snapshot"
        self.assertEqual(14, verify(fixture)["records"])

    def test_empty_database_still_inventories_all_families(self):
        for version in (1, 2, 3):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                automation_fixture(root / "source", empty=True)
                with closing(sqlite3.connect(root / "source")) as db:
                    db.execute(f"PRAGMA user_version={version}")
                result = prepare(root / "source", root / "prepared", SNAPSHOT, SOURCE, CAPTURED)
                self.assertEqual((0, 0, False), (result["records"], result["chunks"], result["imported"]))
                self.assertEqual(result, verify(root / "prepared"))

    def test_nonfrozen_or_unrecognized_sources_never_publish(self):
        mutations = ["CREATE TABLE unexpected(id INTEGER)", "ALTER TABLE maintenance ADD COLUMN unknown TEXT",
                     "DROP TABLE discovery_candidates", "CREATE VIEW unexpected AS SELECT * FROM maintenance",
                     "PRAGMA user_version=4", "PRAGMA application_id=0", "UPDATE enrichment_cooldowns SET until_time=1e999"]
        for sql in mutations:
            with self.subTest(sql=sql), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                automation_fixture(root / "source")
                with closing(sqlite3.connect(root / "source")) as db, db:
                    db.execute(sql)
                before = (root / "source").read_bytes()
                with self.assertRaises(InvalidData):
                    prepare(root / "source", root / "output", SNAPSHOT, SOURCE, CAPTURED)
                self.assertEqual(before, (root / "source").read_bytes())
                self.assertEqual({"source"}, {p.name for p in root.iterdir()})
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            automation_fixture(root / "source")
            for suffix in ("-wal", "-journal"):
                journal = root / ("source" + suffix)
                journal.write_bytes(b"unfinished")
                with self.assertRaises(InvalidData):
                    prepare(root / "source", root / "output", SNAPSHOT, SOURCE, CAPTURED)
                journal.unlink()
            (root / "link").symlink_to(root / "source")
            with self.assertRaises(OSError):
                prepare(root / "link", root / "output", SNAPSHOT, SOURCE, CAPTURED)
            os.mkfifo(root / "pipe")
            with self.assertRaises(InvalidData):
                prepare(root / "pipe", root / "output", SNAPSHOT, SOURCE, CAPTURED)

    def test_source_change_or_output_failure_leaves_no_published_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            automation_fixture(source)
            before = source.read_bytes()
            original = AutomationSource.rows

            def changed(reader, table):
                yield from original(reader, table)
                if table == "maintenance":
                    with source.open("ab") as file:
                        file.write(b"changed")

            with patch.object(AutomationSource, "rows", changed), self.assertRaises(InvalidData):
                prepare(source, root / "output", SNAPSHOT, SOURCE, CAPTURED)
            self.assertEqual({"source"}, {p.name for p in root.iterdir()})
            source.write_bytes(before)
            with patch("stash_ingest.automation_snapshot.os.rename", side_effect=OSError("fixture")), self.assertRaises(OSError):
                prepare(source, root / "output", SNAPSHOT, SOURCE, CAPTURED)
            self.assertEqual({"source"}, {p.name for p in root.iterdir()})

    def test_digest_structure_order_and_file_checks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            automation_fixture(root / "source")
            result = prepare(root / "source", root / "output", SNAPSHOT, SOURCE, CAPTURED)
            manifest = json.loads((root / "output/manifest.json").read_bytes())
            with self.assertRaises(InvalidData): verify(root / "output", "0" * 64)
            chunk = root / "output/records-000000.jsonl"
            original = chunk.read_bytes()
            chunk.write_bytes(original.replace(b"legacy error", b"legacy other"))
            with self.assertRaises(InvalidData): verify(root / "output")
            chunk.write_bytes(original)
            (root / "output/extra").write_bytes(b"extra")
            with self.assertRaises(InvalidData): verify(root / "output")
            (root / "output/extra").unlink()
            chunk.unlink()
            chunk.symlink_to(root / "source")
            with self.assertRaises(OSError): verify(root / "output")
            chunk.unlink()
            chunk.write_bytes(original)
            self.assertEqual(result, verify(root / "output"))
            for name, replacement in (("chunks", None), ("records", None), ("application_id", False), ("source_sha256", "bad")):
                altered = copy.deepcopy(manifest)
                altered[name] = replacement
                with self.subTest(name=name), self.assertRaises(InvalidData): validate_manifest(altered)
            row = snapshot_rows(root / "output")[0]
            for value in (True, {"unexpected": 1}, {"sqlite_blob_base64": "AB=="}, 1 << 63):
                altered = copy.deepcopy(row)
                altered["values"]["last_error"] = value
                with self.subTest(value=value), self.assertRaises(InvalidData): validate_record(altered, manifest["tables"])

    def test_cli_preserves_output_after_lost_publication_ack_and_never_claims_import(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            automation_fixture(root / "source")
            args = ["--automation", str(root / "source"), "--output", str(root / "output"),
                    "--snapshot", SNAPSHOT, "--source", SOURCE, "--captured-at", CAPTURED]
            with patch("stash_ingest.automation_snapshot.sync_directory", side_effect=[None, OSError("lost ack")]), redirect_stderr(io.StringIO()):
                self.assertEqual(1, main(args))
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(0, main(["--verify", str(root / "output")]))
            self.assertFalse(json.loads(output.getvalue())["imported"])
            with redirect_stderr(io.StringIO()):
                self.assertEqual(1, main(["--verify", str(root / "output"), "--snapshot", SNAPSHOT]))


if __name__ == "__main__":
    unittest.main()
