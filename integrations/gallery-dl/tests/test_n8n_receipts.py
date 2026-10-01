from concurrent.futures import ThreadPoolExecutor
from contextlib import closing, redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.backfill_calls import BackfillCalls
from stash_ingest.client import Client
from stash_ingest.encoding import InvalidData, digest, encode
from stash_ingest.n8n_receipts import LegacyReceipts, manifest, review
from stash_ingest.n8n_receipt_import import main, snapshot
from stash_ingest.n8n_runner import main as inspect_main
from stash_ingest.outbox import Capacity, Conflict, Outbox
from helpers import PRODUCER
from test_backfill_calls import specification


def receipt(**changes):
    value = {"command_failed": False, "exit_code": 0, "network_blocked": False, "stdout_tail": "historical Café output", "stderr_tail": ""}
    value.update(changes)
    # Deliberately noncanonical: the exact input bytes must survive.
    return json.dumps(value, ensure_ascii=True, indent=2).encode() + b"\n"


class LegacyN8nReceiptTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.path = self.directory / "native.sqlite"
        self.inputs = self.directory / "receipts"
        self.inputs.mkdir()
        self.source = str(uuid.uuid4())
        self.token = uuid.uuid4().hex

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER)

    def apply(self, store, records):
        return store.import_snapshot(self.source, records, digest(manifest(records)))

    def invoke(self, command, args):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = command(args)
        return code, json.loads(out.getvalue() or err.getvalue())

    def inspect(self, token, *options):
        return self.invoke(inspect_main, ["--outbox", str(self.path), "--endpoint", "http://fixture.invalid",
                                         "--producer", PRODUCER, "--inspect", token, *options])

    def test_exact_results_replay_after_restart_without_source_work_or_network(self):
        values = [(receipt(), "succeeded", 0),
                  (receipt(command_failed=True, exit_code=75), "failed", 1),
                  (receipt(network_blocked=True), "failed", 1),
                  (receipt(backfill_cached=True, account_backfill_complete=False, completed_at=None,
                           backfill_skip_reason="explicit old policy", legacy_skip_recorded_at="2026-09-29"), "skipped", 0),
                  (receipt(account_backfill_complete=True, backfill_cached=False, completed_at="2026-09-30T01:02:03Z"), "succeeded", 0),
                  (receipt(backfill_pending=True), "review", 1),
                  (receipt(command_failed=False, exit_code=4), "review", 1),
                  (receipt(exit_code=True), "review", 1)]
        records = [(uuid.uuid4().hex, body) for body, _, _ in values]
        with closing(self.open()) as box:
            first = self.apply(LegacyReceipts(box), records)
            self.assertEqual(first["added"], len(records))
            before = [tuple(row) for row in box.db.execute("SELECT * FROM n8n_receipt_imports")]
        with closing(self.open()) as box:
            store = LegacyReceipts(box)
            self.assertEqual(self.apply(store, records)["replayed"], len(records))
            self.assertEqual([tuple(row) for row in box.db.execute("SELECT * FROM n8n_receipt_imports")], before)
            self.assertEqual(store.summary()["receipts"], len(records))
            for (token, body), (_, outcome, _) in zip(records, values):
                self.assertEqual(box.db.execute("SELECT body FROM legacy_n8n_receipts WHERE token=?", (token,)).fetchone()[0], body)
                result = store.result(token)
                self.assertEqual(result["state"], "legacy_" + outcome)
                self.assertFalse(result["backfill_pending"])
                self.assertEqual(result["intake_completion"], "unverified")
                self.assertNotIn("source_call_uuid", result)
                if outcome != "review":
                    for key, value in json.loads(body).items():
                        if outcome == "failed" and key == "command_failed":
                            value = True
                        elif outcome == "failed" and key == "exit_code":
                            value = value or 1
                        self.assertEqual(result[key], value)
            for table in ("events", "run_requests", "source_calls", "backfill_calls"):
                self.assertEqual(box.db.execute("SELECT count(*) FROM " + table).fetchone()[0], 0)
            self.assertEqual(box.db.execute("PRAGMA foreign_key_check").fetchall(), [])
        with patch.object(Client, "__init__", side_effect=AssertionError("legacy inspection created a network client")):
            for (token, _), (_, outcome, strict) in zip(records, values):
                code, result = self.inspect(token, "--strict")
                self.assertEqual(code, strict)
                self.assertEqual(result["state"], "legacy_" + outcome)
                self.assertEqual(self.inspect(token)[0], 0)
                self.assertEqual(self.inspect(token, "--retry-reviewed")[0], 1)

    def test_conflicting_evidence_origin_or_native_token_rolls_back_the_whole_snapshot(self):
        with closing(self.open()) as box:
            store = LegacyReceipts(box)
            records = [(self.token, receipt())]
            self.apply(store, records)
            for changed in (receipt(stdout_tail="changed"), receipt().rstrip()):
                with self.assertRaises(Conflict):
                    self.apply(store, [("0" * 32, receipt()), (self.token, changed)])
            with self.assertRaises(Conflict):
                store.import_snapshot(str(uuid.uuid4()), records, digest(manifest(records)))
            native_uuid = str(uuid.uuid4())
            BackfillCalls(box).record(native_uuid, "a" * 64, specification)
            with self.assertRaises(Conflict):
                self.apply(store, [("0" * 32, receipt()), (uuid.UUID(native_uuid).hex, receipt())])
            with self.assertRaises(Conflict):
                BackfillCalls(box).record(str(uuid.UUID(self.token)), "a" * 64,
                                          lambda: self.fail("colliding legacy token reopened a profile"))
            self.assertEqual(store.summary()["receipts"], 1)
            self.assertEqual(box.db.execute("SELECT count(*) FROM n8n_receipt_imports").fetchone()[0], 1)

    def test_interruption_capacity_and_invalid_snapshot_leave_no_partial_publication(self):
        with closing(self.open()) as box:
            store = LegacyReceipts(box)
            records = [("1" * 32, receipt()), ("2" * 32, receipt())]
            box.db.execute("CREATE TRIGGER interrupted BEFORE INSERT ON legacy_n8n_receipts WHEN NEW.token='" + "2" * 32
                           + "' BEGIN SELECT RAISE(ABORT,'interruption'); END")
            with self.assertRaises(sqlite3.IntegrityError):
                self.apply(store, records)
            self.assertEqual(store.summary()["receipts"], 0)
            self.assertEqual(box.db.execute("SELECT count(*) FROM n8n_receipt_imports").fetchone()[0], 0)
            box.db.execute("DROP TRIGGER interrupted")
            self.apply(store, records[:1])
            with patch("stash_ingest.n8n_receipts.MAX_FILES", 1), self.assertRaises(Capacity):
                self.apply(store, records[1:])
            self.assertEqual(store.summary()["receipts"], 1)
            for invalid in ([(self.token, b'{"exit_code":0,"exit_code":1}')],
                            [(self.token, b'{"secret":"private",bad}')], records + records):
                with self.assertRaises(InvalidData):
                    self.apply(store, invalid)
            self.apply(store, records)
            with self.assertRaises(sqlite3.IntegrityError):
                box.db.execute("UPDATE legacy_n8n_receipts SET body=x'00'")
            with self.assertRaises(sqlite3.IntegrityError):
                box.db.execute("DELETE FROM legacy_n8n_receipts")

    def test_concurrent_importers_replay_one_atomic_manifest(self):
        records = [(self.token, receipt())]
        def import_one(_):
            with closing(self.open()) as box:
                return self.apply(LegacyReceipts(box), records)["added"]
        with ThreadPoolExecutor(max_workers=4) as pool:
            self.assertEqual(sum(pool.map(import_one, range(4))), 1)

    def test_cli_validates_before_creating_outbox_and_requires_the_reviewed_digest(self):
        path = self.inputs / (self.token + ".json")
        original = receipt()
        path.write_bytes(original)
        base = ["--receipts", str(self.inputs), "--source", self.source]
        code, preflight = self.invoke(main, base)
        self.assertEqual(code, 0)
        self.assertFalse(self.path.exists())
        apply = [*base, "--apply", "--outbox", str(self.path), "--endpoint", "http://fixture.invalid", "--producer", PRODUCER]
        self.assertEqual(self.invoke(main, apply)[0], 1)
        self.assertEqual(self.invoke(main, [*apply, "--expected-sha256", "a" * 64])[0], 1)
        self.assertFalse(self.path.exists())
        args = [*apply, "--expected-sha256", preflight["input_sha256"]]
        self.assertEqual(self.invoke(main, args)[1]["added"], 1)
        self.assertEqual(self.invoke(main, args)[1]["replayed"], 1)
        self.assertEqual(path.read_bytes(), original)
        self.assertNotIn("historical", json.dumps(preflight))
        path.write_bytes(receipt(stdout_tail="changed"))
        self.assertEqual(self.invoke(main, args)[0], 1)
        with closing(self.open()) as box:
            self.assertEqual(box.db.execute("SELECT body FROM legacy_n8n_receipts").fetchone()[0], original)

    def test_directory_inventory_rejects_unknown_symlink_special_and_changing_inputs(self):
        path = self.inputs / (self.token + ".json")
        path.write_bytes(receipt())
        extra = self.inputs / "unknown.txt"
        extra.write_text("private")
        with self.assertRaises(InvalidData):
            snapshot(self.inputs)
        extra.unlink()
        path.unlink()
        path.symlink_to(self.directory / "unavailable")
        with self.assertRaises(OSError):
            snapshot(self.inputs)
        path.unlink()
        os.mkfifo(path)
        with self.assertRaises(InvalidData):
            snapshot(self.inputs)
        path.unlink()
        path.write_bytes(b"x" * ((128 << 10) + 1))
        with self.assertRaises(InvalidData):
            snapshot(self.inputs)
        path.write_bytes(receipt())
        original_read = os.read
        def change_during_read(fd, size):
            value = original_read(fd, size)
            if value:
                path.write_bytes(receipt(stdout_tail="changed during read"))
            return value
        with patch("stash_ingest.n8n_receipt_import.os.read", side_effect=change_during_read), self.assertRaises(InvalidData):
            snapshot(self.inputs)
        self.assertEqual(review(snapshot(self.inputs))["receipts"], 1)


if __name__ == "__main__":
    unittest.main()
