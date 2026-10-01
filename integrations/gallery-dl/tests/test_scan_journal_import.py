from contextlib import closing, redirect_stdout, redirect_stderr
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.encoding import InvalidData, digest, encode
from stash_ingest.scan_journal_import import KEYS, TABLES, snapshot, main, receipt_fields
from helpers import ROOT


def journal_fixture(path):
    value=json.loads((Path(__file__).resolve().parents[3]/"pkg/scrape/testdata/legacy_scan_journal.json").read_text())
    with closing(sqlite3.connect(path)) as db:
        for table,columns in TABLES.items():
            db.execute("CREATE TABLE "+table+" ("+",".join(columns)+")")
            for record in value["tables"][table]:
                db.execute("INSERT INTO "+table+" VALUES("+",".join("?" for _ in columns)+")",[record[key] for key in columns])
        db.execute("CREATE TABLE backfill_completion(platform,account,component,completed_at,result_json)")
        db.execute("INSERT INTO backfill_completion VALUES('reddit','example','reddit-new','2026-09-29T00:00:00Z','{}')")
        db.commit()
    value["external_tables"]={"backfill_completion":1}
    for table, records in value["tables"].items():
        records.sort(key=lambda row: tuple(row[key] for key in KEYS[table]))
    return value


class ScanJournalImportTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.path=Path(self.temp.name)/"journal.sqlite"
        self.value=journal_fixture(self.path)
        self.source,self.identity=str(uuid.uuid4()),str(uuid.uuid4())
        self.args=["--journal",str(self.path),"--root",ROOT,"--source",self.source,"--snapshot",self.identity,"--captured-at",self.value["captured_at"]]

    def invoke(self,args):
        out,err=io.StringIO(),io.StringIO()
        with redirect_stdout(out),redirect_stderr(err): code=main(args)
        return code,json.loads(out.getvalue() or err.getvalue())

    def test_one_read_snapshot_preserves_every_family_and_reports_external_account_history(self):
        before=self.path.read_bytes()
        self.assertEqual(snapshot(self.path,self.value["captured_at"]),self.value)
        code,report=self.invoke(self.args)
        self.assertEqual(code,0);self.assertEqual(report["record_count"],8)
        self.assertEqual(report["input_sha256"],digest(encode(self.value)))
        self.assertEqual(report["inventory"]["external_tables"],{"backfill_completion":1})
        self.assertEqual(report["jobs_activated"],0)
        self.assertNotIn("/worker",json.dumps(report));self.assertNotIn("Café",json.dumps(report))
        self.assertEqual(self.path.read_bytes(),before)

    def test_unknown_tables_views_columns_and_duplicate_keys_are_not_silently_ignored(self):
        for ddl in ("CREATE TABLE unknown_family(value)", "CREATE TABLE sqliteghost(value)", "CREATE VIEW unknown_view AS SELECT 1", "ALTER TABLE scan_jobs ADD COLUMN unknown_field", "INSERT INTO scan_jobs SELECT * FROM scan_jobs"):
            with self.subTest(ddl=ddl), closing(sqlite3.connect(self.path)) as db:
                db.execute("BEGIN")
                db.execute(ddl);db.commit()
                with self.assertRaises(InvalidData):snapshot(self.path,self.value["captured_at"])
            self.path.unlink();journal_fixture(self.path)

    def test_expected_digest_and_fixed_time_are_required_before_api_access(self):
        with patch("stash_ingest.scan_journal_import.ScanJournalClient.submit",side_effect=AssertionError("unreviewed input reached API")):
            self.assertEqual(self.invoke([*self.args,"--apply","--endpoint","http://fixture.invalid"])[0],1)
            self.assertEqual(self.invoke([*self.args,"--apply","--endpoint","http://fixture.invalid","--expected-sha256","a"*64])[0],1)
            self.assertEqual(self.invoke([*self.args,"--captured-at","today"])[0],1)
        report=receipt_fields({"uuid":self.identity,"source_uuid":self.source,"root_uuid":ROOT,"document":self.value})
        with patch("stash_ingest.scan_journal_import.ScanJournalClient.submit",return_value=report) as submit:
            code,result=self.invoke([*self.args,"--apply","--endpoint","http://fixture.invalid","--expected-sha256",report["input_sha256"]])
            self.assertEqual(code,0);self.assertEqual(result["state"],"retained")
            self.assertEqual(submit.call_args.args[0]["document"],self.value)


if __name__=="__main__":unittest.main()
