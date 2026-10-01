from contextlib import closing, redirect_stdout, redirect_stderr
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
import uuid

from stash_ingest.catalog_identity_import import EXTERNAL_TABLES, TABLES, snapshot as identity_snapshot
from stash_ingest.catalog_registry_import import main, snapshot
from stash_ingest.encoding import InvalidData, encode


def registry_fixture(path):
    fixtures = Path(__file__).resolve().parents[3] / "pkg/scrape/testdata"
    identities = json.loads((fixtures / "legacy_performer_registry.json").read_text())
    registry = json.loads((fixtures / "legacy_registry.json").read_text())
    with closing(sqlite3.connect(path)) as db, db:
        for table, columns in {**TABLES, **EXTERNAL_TABLES}.items():
            db.execute("CREATE TABLE " + table + "(" + ",".join(column + (" INTEGER" if column == "version" else " TEXT") for column in columns) + ")")
            rows = identities["tables"].get(table, registry["tables"].get(table))
            for row in rows:
                db.execute("INSERT INTO " + table + " VALUES(" + ",".join("?" for _ in columns) + ")", [row[column] for column in columns])
    identities["external_tables"] = {table: len(rows) for table, rows in registry["tables"].items()}
    registry["external_tables"] = {table: len(rows) for table, rows in identities["tables"].items()}
    return identities, registry


class CatalogRegistryImportTests(unittest.TestCase):
    def test_complete_complementary_readonly_snapshots(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            identities, registry = registry_fixture(path)
            before = path.read_bytes()
            left = identity_snapshot(path, identities["captured_at"])
            right = snapshot(path, registry["captured_at"])
            self.assertEqual(left["external_tables"], {key: len(rows) for key, rows in right["tables"].items()})
            self.assertEqual(right["external_tables"], {key: len(rows) for key, rows in left["tables"].items()})
            self.assertEqual(21, sum(map(len, right["tables"].values())))
            for table, rows in registry["tables"].items():
                self.assertEqual(sorted(map(encode, rows)), sorted(map(encode, right["tables"][table])))
            self.assertEqual(before, path.read_bytes())

    def test_unknown_table_and_incomplete_reader_shapes_fail(self):
        for change in ("CREATE VIEW unhandled AS SELECT id FROM catalogs", "ALTER TABLE catalogs ADD COLUMN extra TEXT", "DROP TABLE account_identifier_checkpoints"):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "registry.sqlite"
                _, document = registry_fixture(path)
                with closing(sqlite3.connect(path)) as db, db:
                    db.execute(change)
                with self.assertRaises(InvalidData):
                    snapshot(path, document["captured_at"])

    def test_account_id_without_a_handle_keeps_empty_alias_key(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            _, document = registry_fixture(path)
            with closing(sqlite3.connect(path)) as db, db:
                db.execute("UPDATE account_identifiers SET alias_key='',handle='' WHERE catalog_id='twitter'")
            result = snapshot(path, document["captured_at"])
            rows = [row for row in result["tables"]["account_identifiers"] if row["catalog_id"] == "twitter"]
            self.assertEqual(1, len(rows))
            self.assertEqual(("", ""), (rows[0]["alias_key"], rows[0]["handle"]))
            with closing(sqlite3.connect(path)) as db, db:
                db.execute("UPDATE account_identifiers SET account_key='' WHERE catalog_id='twitter'")
            with self.assertRaises(InvalidData):
                snapshot(path, document["captured_at"])

    def test_binding_preparation_and_retries_keep_parent(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            identities, _ = registry_fixture(path)
            parent = {"uuid": str(uuid.uuid4()), "source_uuid": str(uuid.uuid4()), "document": identities}
            parent_file = Path(directory) / "parent.json"
            parent_file.write_text(json.dumps({"binding": parent}))
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(0, main(["--registry", str(path), "--identity-import", str(parent_file), "--snapshot", str(uuid.uuid4())]))
            prepared = json.loads(output.getvalue())
            self.assertEqual(parent["uuid"], prepared["identity_import_uuid"])
            self.assertEqual(parent["source_uuid"], prepared["source_uuid"])
            frozen = Path(directory) / "frozen.json"
            frozen.write_text(output.getvalue())
            again = io.StringIO()
            with redirect_stdout(again):
                self.assertEqual(0, main(["--binding", str(frozen)]))
            self.assertEqual(prepared, json.loads(again.getvalue()))
            with redirect_stderr(io.StringIO()):
                self.assertEqual(1, main(["--binding", str(frozen), "--apply"]))
                self.assertEqual(1, main(["--binding", str(frozen), "--snapshot", str(uuid.uuid4())]))


if __name__ == "__main__":
    unittest.main()
