from contextlib import closing, redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.backfill_import import Journal, main
from stash_ingest.encoding import InvalidData
from helpers import ROOT


def journal_fixture(path, count=1):
    result = '{"command_failed":false,"exit_code":0,"network_blocked":false,"user_accepted_as_complete":true,"opaque":90071992547409931234}'
    with closing(sqlite3.connect(path)) as db, db:
        db.execute("CREATE TABLE backfill_completion(platform TEXT,account TEXT,component TEXT,completed_at TEXT,result_json TEXT,PRIMARY KEY(platform,account,component))")
        db.execute("CREATE TABLE legacy_backfill_skip(platform TEXT,account TEXT,recorded_at TEXT,reason TEXT,PRIMARY KEY(platform,account))")
        for index in range(count):
            db.execute("INSERT INTO backfill_completion VALUES (?,?,?,?,?)", ("twitter", str(index + 1), "twitter", "2026-09-29T12:34:56.123456-07:00", result))
        db.execute("INSERT INTO legacy_backfill_skip VALUES (?,?,?,?)", ("reddit", "Skipped_Account", "2026-09-29T00:00:00Z", "accepted legacy skip"))
    return result


class BackfillImportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "journal.sqlite"
        self.source = str(uuid.uuid4())
        self.args = ["--journal", str(self.path), "--root", ROOT, "--source", self.source]

    def test_read_only_preflight_preserves_exact_provenance_and_needs_no_key_or_api(self):
        result = journal_fixture(self.path)
        original = self.path.read_bytes()
        with closing(Journal(self.path, ROOT, self.source)) as reader:
            records = list(reader.records())
            self.assertEqual(records[0]["record"]["result_json"], result)
            with self.assertRaises(sqlite3.OperationalError):
                reader.db.execute("DELETE FROM backfill_completion")
        output = io.StringIO()
        with redirect_stdout(output), patch("stash_ingest.backfill_import.ImportClient", side_effect=AssertionError("dry run contacted API")):
            self.assertEqual(main(self.args), 0)
        self.assertEqual(json.loads(output.getvalue())["tables"], {"backfill_completion": 1, "legacy_backfill_skip": 1})
        self.assertNotIn("opaque", output.getvalue())
        self.assertEqual(self.path.read_bytes(), original)

    def test_unknown_columns_and_late_bad_rows_stop_before_any_network_write(self):
        journal_fixture(self.path, count=52)
        with closing(sqlite3.connect(self.path)) as db, db:
            db.execute("UPDATE backfill_completion SET completed_at='invalid' WHERE account='52'")
        with redirect_stderr(io.StringIO()), patch("stash_ingest.backfill_import.ImportClient", side_effect=AssertionError("partial preflight wrote rows")):
            self.assertEqual(main([*self.args, "--apply", "--endpoint", "http://fixture.invalid"]), 1)
        with closing(sqlite3.connect(self.path)) as db, db:
            db.execute("ALTER TABLE legacy_backfill_skip ADD COLUMN unknown TEXT")
        with self.assertRaises(InvalidData):
            Journal(self.path, ROOT, self.source)

    def test_one_snapshot_and_bounded_batches_preserve_replay_identity(self):
        journal_fixture(self.path, count=103)
        output = io.StringIO()
        with redirect_stdout(output), patch("stash_ingest.backfill_import.ImportClient") as factory:
            self.assertEqual(main([*self.args, "--apply", "--endpoint", "http://fixture.invalid"]), 0)
        calls = factory.return_value.submit.call_args_list
        self.assertEqual([len(call.args[0]) for call in calls], [50, 50, 4])
        self.assertEqual(json.loads(output.getvalue())["acknowledged"], 104)
        self.assertEqual(len({(record["table"], record["record"]["account"]) for call in calls for record in call.args[0]}), 104)


if __name__ == "__main__":
    unittest.main()
