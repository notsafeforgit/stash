import copy
from contextlib import closing, redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.catalog_identity_import import EXTERNAL_TABLES, TABLES, CatalogIdentityClient, main, receipt_fields, snapshot
from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData, encode


def registry_fixture(path):
    fixture = Path(__file__).resolve().parents[3] / "pkg/scrape/testdata/legacy_performer_registry.json"
    document = json.loads(fixture.read_text())
    with closing(sqlite3.connect(path)) as db, db:
        for table, columns in {**TABLES, **EXTERNAL_TABLES}.items():
            db.execute("CREATE TABLE " + table + "(" + ",".join(column + " TEXT" for column in columns) + ")")
            for row in document["tables"].get(table, []):
                db.execute("INSERT INTO " + table + " VALUES(" + ",".join("?" for _ in columns) + ")", [row[key] for key in columns])
            for _ in range(document["external_tables"].get(table, 0)):
                db.execute("INSERT INTO " + table + " DEFAULT VALUES")
    return document


class CatalogIdentityImportTests(unittest.TestCase):
    def test_readonly_complete_snapshot_and_unknown_shapes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            document = registry_fixture(path)
            before = path.read_bytes()
            captured = snapshot(path, document["captured_at"])
            self.assertEqual(set(TABLES), set(captured["tables"]))
            self.assertEqual(document["external_tables"], captured["external_tables"])
            for table, rows in document["tables"].items():
                self.assertEqual(sorted(map(encode, rows)), sorted(map(encode, captured["tables"][table])))
            self.assertEqual(before, path.read_bytes())
            with closing(sqlite3.connect(path)) as db, db:
                db.execute("ALTER TABLE performer_identities ADD COLUMN extra TEXT")
            with self.assertRaises(InvalidData):
                snapshot(path, document["captured_at"])

    def test_legacy_only_is_retained_and_invalid_embedded_json_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            document = registry_fixture(path)
            with closing(sqlite3.connect(path)) as db, db:
                for table in TABLES:
                    if table.startswith("performer_"):
                        db.execute("DROP TABLE " + table)
            captured = snapshot(path, document["captured_at"])
            self.assertEqual({"catalog_metadata_performers", "catalog_metadata_accounts"}, set(captured["tables"]))
            with closing(sqlite3.connect(path)) as db, db:
                db.execute("UPDATE catalog_metadata_performers SET profile_json=?", ('{"name":"one","name":"two"}',))
            with self.assertRaises(InvalidData):
                snapshot(path, document["captured_at"])

    def test_prepare_retry_binding_cannot_be_overridden(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            document = registry_fixture(path)
            args = ["--registry", str(path), "--source", str(uuid.uuid4()), "--snapshot", str(uuid.uuid4()),
                    "--namespace", "stash", "--captured-at", document["captured_at"]]
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(0, main(args))
            prepared = json.loads(output.getvalue())
            self.assertEqual("prepared", prepared["action"])
            self.assertNotIn("plan_sha256", prepared)
            frozen = Path(directory) / "binding.json"
            frozen.write_text(output.getvalue())
            repeated = io.StringIO()
            with redirect_stdout(repeated):
                self.assertEqual(0, main(["--binding", str(frozen)]))
            self.assertEqual(prepared, json.loads(repeated.getvalue()))
            with redirect_stderr(io.StringIO()):
                self.assertEqual(1, main(["--binding", str(frozen), "--namespace", "another"]))
                self.assertEqual(1, main(["--binding", str(frozen), "--apply"]))

    def test_receipt_must_match_all_input_dimensions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registry.sqlite"
            document = registry_fixture(path)
            binding = {"uuid": str(uuid.uuid4()), "source_uuid": str(uuid.uuid4()), "namespace": "stash",
                       "document": snapshot(path, document["captured_at"]), "account_bindings": {}}
        result = {**receipt_fields(binding), "plan_sha256": "a" * 64, "identities": [], "ownership": [], "records": [], "created_at": "2026-10-01T00:00:00Z"}

        class Response:
            status = 200
            from email.message import Message
            headers = Message()
            headers["Content-Type"] = "application/json"
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self, _): return encode(self.value, 9 << 20)

        client = CatalogIdentityClient("http://127.0.0.1:12345", "STASH_TEST_KEY")
        with patch.dict("os.environ", {"STASH_TEST_KEY": "fixture-key"}), patch.object(client.opener, "open") as open_response:
            for key in ("uuid", "source_uuid", "namespace", "input_sha256", "captured_at", "record_count", "inventory", "plan_sha256"):
                wrong = copy.deepcopy(result)
                wrong[key] = "wrong"
                response = Response()
                response.value = wrong
                open_response.return_value = response
                with self.subTest(key=key), self.assertRaises(Unavailable):
                    client.submit(binding, "a" * 64)


if __name__ == "__main__":
    unittest.main()
