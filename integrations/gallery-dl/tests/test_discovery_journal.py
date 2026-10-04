from concurrent.futures import ThreadPoolExecutor
from contextlib import closing, contextmanager
from copy import deepcopy
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.discovery_client import page_bytes
from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.encoding import InvalidData, decode, digest, encode
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.metadata_bundle import MAX_BYTES
from stash_ingest.outbox import Capacity, Conflict, LeaseLost, Outbox, SCHEMA
from helpers import PRODUCER, capture
from test_discovery_client import execution_fixture
from test_enrichment_execution import execution_fixture as enrichment_fixture
from test_outbox_migration import schema_ten


class DiscoveryJournalTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.box = self.open()
        self.addCleanup(lambda: self.box.close())
        self.journal = DiscoveryJournal(self.box)
        self.description, self.page = execution_fixture()

    def open(self):
        return Outbox(self.path, "http://fixture.invalid", PRODUCER)

    def claim(self, description=None):
        description = description or self.description
        value = self.journal.prepare(description)
        value = self.journal.claim(value, description)
        job = {**description["job"], "state": "running", "revision": description["job"]["revision"] + 1,
               "fence": description["job"]["fence"] + 1, "owner_uuid": value.state["claim"]["owner_uuid"],
               "lease_until": "2026-10-04T14:03:00Z"}
        return self.journal.claimed(value, job)

    def stage(self, description=None):
        value = self.claim(description)
        value = self.journal.reserve(value)
        return self.journal.page(value, value.state["lease"], self.page)

    def receipt(self, value):
        pending = value.state["pending"]
        return {"job_uuid": value.job_uuid, "listing_uuid": value.definition["listing"]["uuid"],
            "ordinal": value.definition["arguments"]["page_ordinal"], "producer_uuid": PRODUCER,
            "fence": pending["lease"]["fence"], "sha256": pending["sha256"], "record_count": pending["record_count"],
            "complete": pending["complete"], "created_at": "2026-10-04T14:00:01Z"}

    def failure_receipt(self, value):
        pending = value.state["pending"]
        return {"job_uuid": value.job_uuid, "producer_uuid": PRODUCER, **pending["lease"],
                "error_code": pending["error_code"], "outcome": "retry" if pending["error_code"] == "timeout" else "failed",
                "started_at": "2026-10-04T14:00:00Z", "ended_at": "2026-10-04T14:00:01Z"}

    def test_claim_intent_recovers_original_owner_after_reopen_and_rejects_changed_page(self):
        with self.assertRaises(LeaseLost):
            self.journal.prepare(self.description)
        with self.journal.execution():
            value = self.journal.prepare(self.description)
            value = self.journal.claim(value, self.description)
            saved = value.state["claim"]
        self.box.close()
        self.box = self.open()
        self.journal = DiscoveryJournal(self.box)
        with self.journal.execution():
            value = self.journal.find(value.job_uuid)
            self.assertEqual(self.journal.claim(value, self.description).state["claim"], saved)
            running = {**self.description["job"], "state": "running", "revision": 2, "fence": 1,
                       "owner_uuid": saved["owner_uuid"], "lease_until": "2026-10-04T14:03:00Z"}
            self.assertEqual(self.journal.claim(value, {**self.description, "job": running}), value)
            for change in ({"owner_uuid": str(uuid.uuid4())}, {"fence": 2}):
                with self.assertRaises(Conflict):
                    self.journal.claimed(value, {**running, **change})
            changed = deepcopy(self.description)
            changed["job"]["arguments"]["generation"] = 2
            with self.assertRaises(Conflict):
                self.journal.prepare(changed)
            claimed = self.journal.claimed(value, running)
            with self.assertRaises(Conflict):
                self.journal.claim(value, self.description)
            self.assertEqual(claimed.state["claim"], saved)

    def test_page_receipt_and_body_release_commit_together_and_nonfinal_is_only_page_success(self):
        with self.journal.execution():
            value = self.stage()
            receipt = self.receipt(value)
            self.assertFalse(receipt["complete"])
            self.assertEqual(value.body, page_bytes(self.page))
            for change in ({"sha256": "f" * 64}, {"fence": 2}, {"producer_uuid": str(uuid.uuid4())},
                           {"record_count": 0}, {"ordinal": 2}, {"complete": True}):
                with self.subTest(change=change), self.assertRaises(Conflict):
                    self.journal.acknowledged(value, {**receipt, **change})
                self.assertEqual(self.journal.find(value.job_uuid), value)
            original = self.box.transaction

            @contextmanager
            def interrupted_commit():
                with original():
                    yield
                    raise RuntimeError("simulated power loss before commit")

            with patch.object(self.box, "transaction", interrupted_commit), self.assertRaises(RuntimeError):
                self.journal.acknowledged(value, receipt)
            self.assertEqual(self.journal.find(value.job_uuid), value)
            done = self.journal.acknowledged(value, receipt)
            self.assertEqual(done.phase, "completed")
            self.assertEqual(done.state["receipt"], receipt)
            self.assertIsNone(done.body)
            self.assertEqual(done.reserved_bytes, 0)
            self.assertFalse(self.journal.summary(value.job_uuid)["receipt"]["complete"])
            with self.assertRaises(Conflict):
                self.journal.note(done, "")
            with self.assertRaises(sqlite3.IntegrityError):
                self.box.db.execute("UPDATE discovery_executions SET revision=revision+1 WHERE job_uuid=?", (value.job_uuid,))

    def test_pending_page_cannot_be_overwritten_or_discarded_and_review_counts_toward_capacity(self):
        self.journal = DiscoveryJournal(self.box, max_pending=1)
        with self.journal.execution():
            value = self.stage()
            for operation in (lambda: self.journal.reserve(value), lambda: self.journal.release_reservation(value),
                              lambda: self.journal.page(value, value.state["lease"], self.page),
                              lambda: self.journal.failure(value, value.state["lease"], "timeout")):
                with self.assertRaises(Conflict):
                    operation()
            held = self.journal.note(value, "page_requires_review", review=True)
            other, _ = execution_fixture()
            with self.assertRaises(Capacity):
                self.journal.prepare(other)
            self.assertEqual(held.body, value.body)
            self.journal.acknowledged(held, self.receipt(held))
            self.assertEqual(self.journal.prepare(other).phase, "active")

    def test_page_rebind_changes_only_attempt_after_an_expired_claim(self):
        with self.journal.execution():
            value = self.stage()
            original = value.body
            next_description = deepcopy(self.description)
            next_description["job"].update(fence=1, revision=4)
            value = self.journal.claim(value, next_description)
            replacement = {**next_description["job"], "state": "running", "revision": 5, "fence": 2,
                "owner_uuid": value.state["claim"]["owner_uuid"], "lease_until": "2026-10-04T14:05:00Z"}
            value = self.journal.claimed(value, replacement)
            old_receipt = self.receipt(value)
            value = self.journal.rebind_page(value, replacement)
            self.assertEqual(value.body, original)
            self.assertEqual(value.state["pending"]["lease"]["fence"], 2)
            with self.assertRaises(Conflict):
                self.journal.acknowledged(value, old_receipt)
            self.assertEqual(self.journal.acknowledged(value, self.receipt(value)).phase, "completed")

    def test_observed_native_completion_cannot_discard_an_unacknowledged_local_page(self):
        with self.journal.execution():
            value = self.stage()
            receipt = {**self.receipt(value), "producer_uuid": str(uuid.uuid4()), "fence": 2}
            done = deepcopy(self.description)
            done["job"].update(state="succeeded", revision=5, fence=2, result={
                "listing_uuid": receipt["listing_uuid"], "page_ordinal": receipt["ordinal"], "page_sha256": receipt["sha256"]})
            done["receipt"] = receipt
            with self.assertRaises(Conflict):
                self.journal.observe_terminal(value, done)
            self.assertEqual(self.journal.find(value.job_uuid), value)
        # Another worker's completed page can settle a local claim that fetched
        # no data; it never becomes this producer's own delivery receipt.
        with tempfile.TemporaryDirectory() as directory, closing(Outbox(Path(directory) / "peer.sqlite", self.box.endpoint, PRODUCER)) as box:
            peer = DiscoveryJournal(box)
            with peer.execution():
                unfinished = peer.reserve(peer.prepare(self.description))
                observed = peer.observe_terminal(unfinished, done)
                self.assertEqual(observed.phase, "completed")
                self.assertIsNone(observed.state["receipt"])
                self.assertEqual(observed.state["terminal"], done)
                self.assertEqual(observed.reserved_bytes, 0)
                self.assertEqual(peer.summary(value.job_uuid)["receipt"], receipt)
                for state in ("failed", "cancelled"):
                    description, _ = execution_fixture()
                    value = peer.prepare(description)
                    ended = deepcopy(description)
                    ended["job"].update(state=state, revision=2)
                    self.assertEqual(peer.observe_terminal(value, ended).phase, "failed")

    def test_failure_replay_retains_owned_outcome_and_retry_releases_the_original_claim(self):
        with self.journal.execution():
            value = self.claim()
            value = self.journal.reserve(value)
            value = self.journal.failure(value, value.state["lease"], "timeout")
            self.assertEqual(value.reserved_bytes, 0)
            receipt = self.failure_receipt(value)
            for change in ({"owner_uuid": str(uuid.uuid4())}, {"producer_uuid": str(uuid.uuid4())},
                           {"outcome": "failed"}, {"error_code": "worker_failed"}, {"ended_at": None}):
                with self.assertRaises(Conflict):
                    self.journal.acknowledged(value, {**receipt, **change})
            with self.assertRaises(Conflict):
                self.journal.claim(value, self.description)
            retry = self.journal.acknowledged(value, receipt)
            self.assertEqual(retry.phase, "active")
            self.assertIsNone(retry.state["lease"])
            self.assertIsNone(retry.state["claim"])
            self.assertEqual(retry.state["failure"], receipt)
            next_description = deepcopy(self.description)
            next_description["job"].update(fence=1, revision=4)
            value = self.claim(next_description)
            value = self.journal.failure(value, value.state["lease"], "pagination_stalled")
            done = self.journal.acknowledged(value, self.failure_receipt(value))
            self.assertEqual(done.phase, "failed")
            with self.assertRaises(Conflict):
                self.journal.note(done, "")

    def test_discovery_enrichment_and_event_bytes_share_one_transactional_capacity_limit(self):
        self.box.max_bytes = MAX_BYTES
        enrichment = EnrichmentJournal(self.box)
        with self.journal.execution(), enrichment.execution():
            value = self.journal.reserve(self.journal.prepare(self.description))
            enriched = enrichment.prepare(enrichment_fixture())
            with self.assertRaises(Capacity):
                enrichment.reserve(enriched)
            with self.assertRaises(Capacity):
                self.box.enqueue(encode(capture()))
            value = self.journal.release_reservation(value)
            enriched = enrichment.reserve(enriched)
            with self.assertRaises(Capacity):
                self.journal.reserve(value)
            with self.assertRaises(Capacity):
                self.box.enqueue(encode(capture()))
            enrichment.change(enriched, reserved=0)
            self.box.enqueue(encode(capture()))
            with self.assertRaises(Capacity):
                self.journal.reserve(value)
            with self.assertRaises(Capacity):
                enrichment.reserve(enrichment.find(enriched.job_uuid))

    def test_concurrent_extractors_cannot_both_reserve_the_last_page_budget(self):
        ready = threading.Barrier(2)

        def reserve(discovery):
            with closing(self.open()) as box:
                box.max_bytes = MAX_BYTES
                journal = DiscoveryJournal(box) if discovery else EnrichmentJournal(box)
                with journal.execution() as owned:
                    self.assertTrue(owned)
                    value = journal.prepare(self.description if discovery else enrichment_fixture())
                    ready.wait(timeout=5)
                    try:
                        journal.reserve(value)
                        return "reserved"
                    except Capacity:
                        return "capacity"

        with ThreadPoolExecutor(max_workers=2) as pool:
            self.assertEqual(sorted(pool.map(reserve, (True, False))), ["capacity", "reserved"])
        self.assertEqual(self.box.metadata_reserved_bytes(), MAX_BYTES)

    def test_page_must_match_the_original_cursor_before_replacing_reserved_capacity(self):
        with self.journal.execution():
            value = self.journal.reserve(self.claim())
            changed = deepcopy(self.page)
            changed["cursor"] = {"after": "t3_unrequested"}
            with self.assertRaises(InvalidData):
                self.journal.page(value, value.state["lease"], changed)
            self.assertEqual(self.journal.find(value.job_uuid), value)
            with self.assertRaises(Conflict):
                self.journal.page(value, {**value.state["lease"], "owner_uuid": str(uuid.uuid4())}, self.page)
            self.assertEqual(self.journal.find(value.job_uuid), value)

    def test_kernel_lock_excludes_peer_discovery_without_blocking_downloads_or_enrichment(self):
        with closing(self.open()) as peer_box:
            peer = DiscoveryJournal(peer_box)
            with self.journal.execution() as owned:
                self.assertTrue(owned)
                with peer.execution() as other:
                    self.assertFalse(other)
                    with self.assertRaises(LeaseLost):
                        peer.prepare(self.description)
                with EnrichmentJournal(peer_box).execution() as independent:
                    self.assertTrue(independent)
                peer_box.enqueue(encode(capture()))
            with peer.execution() as owned:
                self.assertTrue(owned)

    def test_process_death_preserves_original_page_and_releases_only_the_kernel_lock(self):
        script = """
import os, signal, sys
from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.encoding import decode
from stash_ingest.outbox import Outbox
data = decode(sys.stdin.buffer.read(), preserve_numbers=True)
box = Outbox(sys.argv[1], 'http://fixture.invalid', data['producer'])
journal = DiscoveryJournal(box)
with journal.execution() as owned:
    assert owned
    value = journal.prepare(data['description'])
    value = journal.claim(value, data['description'])
    lease = dict(data['description']['job'], state='running', revision=2, fence=1,
        owner_uuid=value.state['claim']['owner_uuid'], lease_until='2026-10-04T14:03:00Z')
    value = journal.claimed(value, lease)
    value = journal.reserve(value)
    journal.page(value, lease, data['page'])
    os.kill(os.getpid(), signal.SIGKILL)
"""
        result = subprocess.run([sys.executable, "-B", "-c", script, str(self.path)], input=encode({
            "producer": PRODUCER, "description": self.description, "page": self.page}), capture_output=True, timeout=10)
        self.assertEqual(result.returncode, -signal.SIGKILL, result.stderr)
        with self.journal.execution() as owned:
            self.assertTrue(owned)
            value = self.journal.find(self.description["job"]["uuid"])
            self.assertEqual(value.body, page_bytes(self.page))
            self.assertEqual(value.state["pending"]["sha256"], digest(value.body))
            self.assertEqual(self.journal.acknowledged(value, self.receipt(value)).phase, "completed")

    def test_tampered_body_or_missing_intent_is_rejected_before_delivery(self):
        with self.journal.execution():
            value = self.stage()
            self.box.db.execute("UPDATE discovery_executions SET body=? WHERE job_uuid=?", (b"{}", value.job_uuid))
            with self.assertRaises(InvalidData):
                self.journal.find(value.job_uuid)
            self.box.db.execute("UPDATE discovery_executions SET body=? WHERE job_uuid=?", (value.body, value.job_uuid))
            state = encode({**value.state, "pending": None})
            self.box.db.execute("UPDATE discovery_executions SET state=?,state_sha256=? WHERE job_uuid=?",
                                (state, digest(state), value.job_uuid))
            with self.assertRaises(InvalidData):
                self.journal.find(value.job_uuid)

    def test_schema_ten_promotion_preserves_every_prior_table_and_exact_staged_enrichment(self):
        self.box.enqueue(encode(capture()))
        enrichment = EnrichmentJournal(self.box)
        with enrichment.execution():
            description = enrichment_fixture()
            value = enrichment.prepare(description)
            value = enrichment.claim(value, description["job"])
            lease = {**description["job"], "owner_uuid": value.state["claim"]["owner_uuid"], "fence": 1}
            value = enrichment.claimed(value, lease)
            value = enrichment.reserve(value)
            fixture = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
            bodies = decode(fixture.read_bytes(), preserve_numbers=True)
            enrichment.checkpoint(value, lease, 0, bodies["complete"])
        schema_ten(self.box.db)
        tables = [r[0] for r in self.box.db.execute("SELECT name FROM sqlite_schema WHERE type='table'")]
        before = {table: [tuple(r) for r in self.box.db.execute('SELECT * FROM "' + table + '" ORDER BY rowid')] for table in tables}
        self.box.close()
        self.box = self.open()
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
        for table, rows in before.items():
            self.assertEqual([tuple(r) for r in self.box.db.execute('SELECT * FROM "' + table + '" ORDER BY rowid')], rows)
        self.assertEqual(self.box.db.execute("PRAGMA foreign_key_check").fetchall(), [])
        self.assertEqual(self.box.db.execute("PRAGMA integrity_check").fetchone()[0], "ok")
        self.assertEqual(self.box.db.execute("SELECT count(*) FROM discovery_executions").fetchone()[0], 0)

    def test_migration_collision_rolls_back_without_replacing_unknown_evidence(self):
        self.box.enqueue(encode(capture()))
        before = [tuple(r) for r in self.box.db.execute("SELECT * FROM events")]
        schema_ten(self.box.db)
        self.box.db.execute("CREATE TABLE discovery_executions(original BLOB)")
        self.box.db.execute("INSERT INTO discovery_executions VALUES(?)", (b"preserve",))
        with self.assertRaises(sqlite3.OperationalError):
            self.open()
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], 10)
        self.assertEqual(self.box.db.execute("SELECT original FROM discovery_executions").fetchone()[0], b"preserve")
        self.assertEqual([tuple(r) for r in self.box.db.execute("SELECT * FROM events")], before)
