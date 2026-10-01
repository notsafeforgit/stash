"""Caller completion follows its original submissions, not later rescans."""

from contextlib import closing
import copy
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.client import Client, Unavailable
from stash_ingest.completion import inspect_ticket
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.outbox import Outbox, SCHEMA
from stash_ingest.run_queue import RunQueue
from helpers import PRODUCER, ROOT
from test_outbox_migration import schema_three
from test_run_queue import request, admission, window


class CompletionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "producer.sqlite"
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())
        self.queue = RunQueue(self.box)
        self.client = Client(self.box.endpoint, PRODUCER)
        self.client.capabilities = Mock(return_value={"source_runs": True, "source_run_protocol": 1})
        self.current = {}
        self.client._request = Mock(side_effect=lambda method, route: self.current[route.rsplit('/', 1)[1]])

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: 1000)

    def ticket(self, since=10, until=20):
        ticket = str(uuid.uuid4())
        self.queue.enqueue(request(since, until), ticket_uuid=ticket)
        return ticket

    def admit(self, frozen=None, *, state="queued", completed=()):
        frozen = frozen or self.queue.claim(str(uuid.uuid4()))
        response = admission(frozen.body, uuid=str(uuid.uuid4()))
        self.queue.admit(frozen, response)
        current = {**response, "state": state, "completed": list(completed)}
        self.current[current["uuid"]] = current
        return current

    def inspect(self, ticket):
        return inspect_ticket(self.box, self.client, ticket)

    def split_ticket(self):
        self.queue.enqueue(request())
        frozen = self.queue.claim(str(uuid.uuid4()))
        ticket = self.ticket(0, 30)
        middle = self.admit(frozen, state="succeeded", completed=[window(10, 20)])
        newest = self.admit()
        oldest = self.admit(state="running", completed=[window(0, 5)])
        return ticket, middle, newest, oldest

    def test_admission_and_success_labels_do_not_replace_completed_window_evidence(self):
        ticket = self.ticket()
        self.assertEqual(self.inspect(ticket)["state"], "recorded")
        self.client.capabilities.assert_not_called()
        frozen = self.queue.claim(str(uuid.uuid4()))
        self.assertEqual(self.inspect(ticket)["state"], "sending")
        self.client._request.assert_not_called()
        current = self.admit(frozen, state="succeeded")
        result = self.inspect(ticket)
        self.assertEqual(result["state"], "review")
        self.assertEqual(result["remaining"], [window(10, 20)])
        current.update(state="running", completed=[window(10, 15)])
        self.assertEqual(self.inspect(ticket)["remaining"], [window(15, 20)])
        current["completed"] = [window(10, 20)]
        result = self.inspect(ticket)
        self.assertEqual(result["state"], "source_succeeded", "a wider shared run may still be running")
        self.assertEqual(result["intake_completion"], "inspect_native_receipts")
        self.assertEqual(result["remaining"], [])

    def test_split_windows_require_every_original_submission_and_survive_restart(self):
        ticket, middle, newest, oldest = self.split_ticket()
        self.box.close()
        self.box = self.open()
        result = self.inspect(ticket)
        self.assertEqual(len(result["submissions"]), 3)
        self.assertEqual(result["remaining"], [window(5, 10), window(20, 30)])
        self.assertEqual(result["state"], "running")
        newest.update(state="succeeded", completed=[window(20, 30)])
        oldest.update(state="succeeded", completed=[window(0, 10)])
        self.assertEqual(self.inspect(ticket)["state"], "source_succeeded")

    def test_cancelled_ticket_is_not_completed_by_a_later_successful_rescan(self):
        original = self.ticket()
        old = self.admit(state="cancelled")
        replacement = self.ticket()
        new = self.admit(state="succeeded", completed=[window(10, 20)])
        result = self.inspect(original)
        self.assertEqual(result["state"], "cancelled")
        self.assertEqual([part["run_uuid"] for part in result["submissions"]], [old["uuid"]])
        result = self.inspect(replacement)
        self.assertEqual(result["state"], "source_succeeded")
        self.assertEqual([part["run_uuid"] for part in result["submissions"]], [new["uuid"]])

    def test_review_and_deferral_remain_visible_without_new_network_work(self):
        ticket = self.ticket()
        frozen = self.queue.claim(str(uuid.uuid4()))
        self.queue.fail(frozen, "outside_scope", review=True)
        result = self.inspect(ticket)
        self.assertEqual(result["state"], "review")
        self.client.capabilities.assert_not_called()
        self.queue.retry(frozen.request_uuid)
        current = self.admit(state="deferred")
        self.assertEqual(self.inspect(ticket)["state"], "deferred")
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM run_requests").fetchone()[0], 1)
        self.assertEqual(current["completed"], [])

    def test_mismatched_status_and_missing_ranges_never_certify_completion(self):
        ticket = self.ticket()
        current = self.admit(state="succeeded", completed=[window(10, 20)])
        original = copy.deepcopy(current)
        for field, value in (("uuid", str(uuid.uuid4())), ("collection_revision", True), ("policy_sha256", "b" * 64),
                             ("root_uuid", None), ("completed", None), ("completed", [window(20, 30)])):
            current.clear()
            current.update(original)
            current[field] = value
            with self.subTest(field=field):
                self.assertEqual(self.inspect(ticket)["state"], "review")
        current.clear()
        current.update(original)
        self.assertEqual(self.inspect(ticket)["state"], "source_succeeded")
        self.client._request.side_effect = Unavailable("network_unavailable")
        result = self.inspect(ticket)
        self.assertEqual(result["state"], "unavailable", "do not present a stale cached success as current proof")
        self.assertEqual(result["remaining"], [window(10, 20)])

    def test_incompatible_capabilities_are_shared_across_all_components(self):
        ticket, _, _, _ = self.split_ticket()
        self.client.capabilities.return_value = {"source_runs": True, "source_run_protocol": 2}
        self.assertEqual(self.inspect(ticket)["state"], "unavailable")
        self.client.capabilities.assert_called_once()
        self.client._request.assert_not_called()

    def test_corrupt_assignments_stop_before_status_requests(self):
        ticket = self.ticket()
        self.admit(state="succeeded", completed=[window(10, 20)])
        self.box.db.execute("UPDATE run_ticket_requests SET windows=?", (encode([window(10, 15)]),))
        with self.assertRaises(InvalidData):
            self.inspect(ticket)
        self.client.capabilities.assert_not_called()

    def test_failed_assignment_rolls_back_the_frozen_request_and_pending_windows(self):
        ticket = self.ticket()
        self.box.db.execute("""CREATE TRIGGER reject_assignment BEFORE INSERT ON run_ticket_requests
            BEGIN SELECT RAISE(ABORT,'fixture assignment failure'); END""")
        with self.assertRaises(sqlite3.IntegrityError):
            self.queue.claim(str(uuid.uuid4()))
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM run_requests").fetchone()[0], 0)
        self.assertEqual(self.inspect(ticket)["state"], "recorded")
        self.box.db.execute("DROP TRIGGER reject_assignment")
        self.admit(state="succeeded", completed=[window(10, 20)])
        self.assertEqual(self.inspect(ticket)["state"], "source_succeeded")

    def test_schema_three_rebuilds_original_assignments_and_preserves_dispatch_state(self):
        ticket, _, _, _ = self.split_ticket()
        original = self.inspect(ticket)
        replacement = self.ticket(0, 30)
        self.admit(state="succeeded", completed=[window(0, 30)])
        before = [tuple(row) for row in self.box.db.execute("SELECT * FROM run_ticket_requests ORDER BY ticket_uuid,request_uuid")]
        self.box.db.execute("""INSERT INTO dispatch_cursors(root_uuid,policy_sha256,after_sequence,revision,failures,available_at,error_code)
            VALUES(?,?,12,3,4,2000,'network_unavailable')""", (ROOT, "a" * 64))
        schema_three(self.box.db)
        self.box.close()
        self.box = self.open()
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
        after = [tuple(row) for row in self.box.db.execute("SELECT * FROM run_ticket_requests ORDER BY ticket_uuid,request_uuid")]
        self.assertEqual(before, after)
        self.assertEqual(self.inspect(ticket), original)
        self.assertEqual(self.inspect(replacement)["state"], "source_succeeded")
        row = self.box.db.execute("SELECT * FROM dispatch_cursors").fetchone()
        self.assertEqual((row["after_sequence"], row["revision"], row["available_at"]), (12, 3, 2000))
        self.assertEqual(self.box.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_invalid_schema_three_ticket_rolls_back_promotion(self):
        ticket = self.ticket()
        self.admit()
        schema_three(self.box.db)
        self.box.db.execute("UPDATE run_intent_tickets SET window=? WHERE uuid=?", (b'{}', ticket))
        self.box.close()
        with self.assertRaises(InvalidData):
            self.open()
        with closing(sqlite3.connect(self.path)) as raw:
            self.assertEqual(raw.execute("PRAGMA user_version").fetchone()[0], 3)
            self.assertIsNone(raw.execute("SELECT name FROM sqlite_schema WHERE name='run_ticket_requests'").fetchone())
            self.assertEqual(raw.execute("SELECT window FROM run_intent_tickets").fetchone()[0], b'{}')

    def test_assignment_and_promotion_include_every_page_of_waiting_tickets(self):
        tickets = [self.ticket() for _ in range(205)]
        self.admit(state="succeeded", completed=[window(10, 20)])
        pending = self.ticket(20, 30)
        before = [tuple(row) for row in self.box.db.execute("SELECT * FROM run_ticket_requests ORDER BY ticket_uuid")]
        self.assertEqual(len(before), len(tickets))
        schema_three(self.box.db)
        self.box.close()
        self.box = self.open()
        after = [tuple(row) for row in self.box.db.execute("SELECT * FROM run_ticket_requests ORDER BY ticket_uuid")]
        self.assertEqual(after, before)
        for ticket in tickets:
            self.assertEqual(self.inspect(ticket)["state"], "source_succeeded")
        self.assertEqual(self.inspect(pending)["unassigned"], [window(20, 30)])

    def test_cli_reports_recorded_as_pending_without_a_token_or_network(self):
        ticket = self.ticket()
        result = subprocess.run([sys.executable, "-m", "stash_ingest.cli", "--outbox", str(self.path),
                                 "--endpoint", self.box.endpoint, "--producer", PRODUCER, "ticket-status", ticket],
                                capture_output=True, timeout=15)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(json.loads(result.stdout)["state"], "recorded")


if __name__ == "__main__":
    unittest.main()
