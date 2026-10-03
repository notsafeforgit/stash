from copy import deepcopy
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.encoding import InvalidData, decode, digest, encode
from stash_ingest.enrichment_client import checkpoint_bytes
from stash_ingest.enrichment_configuration import EnrichmentConfiguration, SCHEMA as PROFILE_SCHEMA
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.metadata_bundle import MAX_BYTES
from stash_ingest.outbox import Capacity, Conflict, LeaseLost, Outbox, SCHEMA
from helpers import PRODUCER, capture
from test_outbox_migration import schema_seven


def execution_fixture():
    target = {"uuid": str(uuid.uuid4()), "post_uuid": str(uuid.uuid4()), "collection_uuid": str(uuid.uuid4()),
              "collection_revision": 1, "url": "https://www.reddit.com/comments/abc123", "policy": "gallery-dl-metadata-v1"}
    job = {"uuid": str(uuid.uuid4()), "kind": "post.enrich", "state": "queued", "revision": 1, "fence": 0, "max_attempts": 8,
           "arguments": {"version": 1, "target_uuid": target["uuid"], "target_revision": 1,
             "post_uuid": target["post_uuid"], "collection_uuid": target["collection_uuid"], "collection_revision": 1,
             "root_uuid": None, "policy_sha256": "a" * 64, "extractor_version": "1.32.15-dev"}}
    return {"job": job, "target": target}


class EnrichmentJournalTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.box = Outbox(self.path, "http://fixture.invalid", PRODUCER)
        self.addCleanup(lambda: self.box.close())
        self.journal = EnrichmentJournal(self.box)
        self.execution = execution_fixture()
        fixture = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
        self.bodies = decode(fixture.read_bytes(), preserve_numbers=True)

    def prepared(self):
        value = self.journal.prepare(self.execution)
        value = self.journal.claim(value, self.execution["job"])
        self.lease = {**self.execution["job"], "state": "running", "revision": 2, "fence": 1,
                      "owner_uuid": value.state["claim"]["owner_uuid"]}
        return self.journal.claimed(value, self.lease)

    def stage(self, kind="complete"):
        value = self.journal.reserve(self.prepared())
        body = self.bodies[kind]
        value = self.journal.checkpoint(value, self.lease, 0, body)
        self.receipt = {"job_uuid": value.job_uuid, "revision": 1, "fence": 1,
            "sha256": digest(value.body), "record_count": len(body["records"]),
            "pending_count": len(body["pending"]), "unresolved_count": len(body["unresolved"])}
        return value

    def test_stable_claim_fenced_local_updates_and_restart_keep_original_request(self):
        with self.journal.execution() as owned:
            self.assertTrue(owned)
            value = self.prepared()
            saved = deepcopy(value.state["claim"])
            self.assertEqual(self.journal.claim(value, self.lease), value)
            newer = self.journal.reserve(value)
            with self.assertRaises(Conflict):
                self.journal.change(value, reserved=0)
            self.assertEqual(self.journal.find(value.job_uuid), newer)
        self.box.close()
        self.box = Outbox(self.path, "http://fixture.invalid", PRODUCER)
        self.journal = EnrichmentJournal(self.box)
        self.assertEqual(self.journal.find(value.job_uuid).state["claim"], saved)
        with self.assertRaises(LeaseLost):
            self.journal.reserve(newer)

    def test_shared_byte_reservation_stops_other_payloads_before_fetch(self):
        self.box.max_bytes = MAX_BYTES
        with self.journal.execution():
            value = self.journal.reserve(self.prepared())
            with self.assertRaises(Capacity):
                self.box.enqueue(encode(capture()))
            value = self.journal.change(value, reserved=0)
            self.box.enqueue(encode(capture()))
            with self.assertRaises(Capacity):
                self.journal.reserve(value)
            with self.assertRaises(Conflict):
                self.journal.change(value, reserved=MAX_BYTES)

    def test_acknowledgement_releases_only_verified_body_and_stages_publication(self):
        with self.journal.execution():
            value = self.stage()
            for mutation in ({"sha256": "f" * 64}, {"record_count": 1}, {"fence": 2}, {"revision": 2}):
                with self.assertRaises(Conflict):
                    self.journal.acknowledged(value, dict(self.receipt, **mutation))
                self.assertEqual(self.journal.find(value.job_uuid).body, value.body)
            acknowledged = self.journal.acknowledged(value, self.receipt)
            self.assertIsNone(acknowledged.body)
            self.assertEqual(acknowledged.reserved_bytes, 0)
            self.assertEqual(acknowledged.state["pending"]["kind"], "publish")
            self.assertEqual(acknowledged.state["pending"]["receipt"], self.receipt)
            publication = {"job_uuid": value.job_uuid, "checkpoint_revision": 1,
                "checkpoint_sha256": self.receipt["sha256"], "fence": 1, "completion_uuid": str(uuid.uuid4()),
                "record_count": self.receipt["record_count"], "capture_count": 3, "unresolved_count": 1}
            done = self.journal.acknowledged(acknowledged, publication)
            self.assertEqual(done.phase, "completed")
            self.assertEqual(self.journal.summary(value.job_uuid)["publication"], publication)
            with self.assertRaises(Conflict):
                self.journal.change(done)

    def test_pending_children_stage_precise_failure_in_the_same_acknowledgement(self):
        with self.journal.execution():
            value = self.stage("initial")
            acknowledged = self.journal.acknowledged(value, self.receipt)
            self.assertIsNone(acknowledged.body)
            pending = acknowledged.state["pending"]
            self.assertEqual(pending["kind"], "failure")
            self.assertEqual(pending["error_code"], "timeout")
            receipt = {"job_uuid": value.job_uuid, "producer_uuid": PRODUCER, **pending["lease"],
                       "error_code": "timeout", "outcome": "retry", "ended_at": "2026-10-03T00:00:00Z"}
            with self.assertRaises(Conflict):
                self.journal.acknowledged(acknowledged, dict(receipt, producer_uuid=str(uuid.uuid4())))
            retry = self.journal.acknowledged(acknowledged, receipt)
            self.assertEqual(retry.phase, "active")
            self.assertIsNone(retry.state["pending"])
            self.assertIsNone(retry.state["claim"])
            self.assertEqual(retry.state["checkpoint"], self.receipt)

    def test_unknown_definition_or_tampered_bytes_stop_before_replay(self):
        with self.journal.execution():
            value = self.stage()
            other = deepcopy(self.execution)
            other["target"]["url"] += "/changed"
            with self.assertRaises(Conflict):
                self.journal.prepare(other)
            self.box.db.execute("UPDATE enrichment_executions SET body=? WHERE job_uuid=?", (b"{}", value.job_uuid))
            with self.assertRaises(InvalidData):
                self.journal.find(value.job_uuid)

    def test_concurrent_process_lock_never_blocks_download_delivery(self):
        other = Outbox(self.path, "http://fixture.invalid", PRODUCER)
        self.addCleanup(other.close)
        peer = EnrichmentJournal(other)
        with self.journal.execution() as owned:
            self.assertTrue(owned)
            with peer.execution() as peer_owned:
                self.assertFalse(peer_owned)
            other.enqueue(encode(capture()))
        with peer.execution() as peer_owned:
            self.assertTrue(peer_owned)

    def test_process_death_releases_lock_but_preserves_unacknowledged_evidence(self):
        script = """
import json, os, signal, sys
from stash_ingest.outbox import Outbox
from stash_ingest.enrichment_journal import EnrichmentJournal
data = json.load(sys.stdin)
box = Outbox(sys.argv[1], 'http://fixture.invalid', data['producer'])
journal = EnrichmentJournal(box)
with journal.execution() as owned:
    assert owned
    value = journal.prepare(data['execution'])
    value = journal.claim(value, data['execution']['job'])
    lease = dict(data['execution']['job'], state='running', revision=2, fence=1, owner_uuid=value.state['claim']['owner_uuid'])
    value = journal.claimed(value, lease)
    value = journal.reserve(value)
    journal.checkpoint(value, lease, 0, data['body'])
    os.kill(os.getpid(), signal.SIGKILL)
"""
        result = subprocess.run([sys.executable, "-B", "-c", script, str(self.path)], input=encode({
            "producer": PRODUCER, "execution": self.execution, "body": self.bodies["complete"]}), capture_output=True, timeout=10)
        self.assertEqual(result.returncode, -signal.SIGKILL, result.stderr)
        with self.journal.execution() as owned:
            self.assertTrue(owned)
            value = self.journal.find(self.execution["job"]["uuid"])
            self.assertEqual(value.body, checkpoint_bytes(self.bodies["complete"]))
            self.assertEqual(value.state["pending"]["kind"], "checkpoint")

    def test_schema_seven_promotion_preserves_every_existing_table(self):
        self.box.enqueue(encode(capture()))
        schema_seven(self.box.db)
        tables = [r[0] for r in self.box.db.execute("SELECT name FROM sqlite_schema WHERE type='table'")]
        before = {table: [tuple(r) for r in self.box.db.execute('SELECT * FROM "' + table + '"')] for table in tables}
        self.box.close()
        self.box = Outbox(self.path, "http://fixture.invalid", PRODUCER)
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
        for table, rows in before.items():
            self.assertEqual([tuple(r) for r in self.box.db.execute('SELECT * FROM "' + table + '"')], rows)
        self.assertEqual(self.box.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_schema_collision_rolls_back_instead_of_replacing_staged_work(self):
        schema_seven(self.box.db)
        self.box.db.execute("CREATE TABLE enrichment_executions(original BLOB)")
        self.box.db.execute("INSERT INTO enrichment_executions VALUES(?)", (b"retained",))
        with self.assertRaises(sqlite3.OperationalError):
            Outbox(self.path, "http://fixture.invalid", PRODUCER)
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], 7)
        self.assertEqual(self.box.db.execute("SELECT original FROM enrichment_executions").fetchone()[0], b"retained")


class EnrichmentConfigurationTests(unittest.TestCase):
    def test_metadata_profile_has_no_media_root_and_excludes_writer_behavior_and_access_values(self):
        with tempfile.TemporaryDirectory() as directory:
            value = {"schema": PROFILE_SCHEMA, "source_category": "reddit", "bindings": {
                "login": {"kind": "private", "env": "ENRICHMENT_FIXTURE_LOGIN"}},
                "gallery": {"extractor": {"reddit": {"cookies": "${stash:login}"}, "sleep-request": 0,
                    "archive": "/absent/old-archive", "filename": "old-pattern"},
                    "postprocessor": {"old": {"function": "/absent/catalog_hook.py:main"}}}}
            with patch.dict(os.environ, {"ENRICHMENT_FIXTURE_LOGIN": "first-secret"}):
                first = EnrichmentConfiguration.from_document(value, directory)
            with patch.dict(os.environ, {"ENRICHMENT_FIXTURE_LOGIN": "rotated-secret"}):
                value["gallery"]["extractor"]["filename"] = "changed-pattern"
                second = EnrichmentConfiguration.from_document(value, directory)
            self.assertEqual(first.policy_sha256, second.policy_sha256)
            self.assertFalse(hasattr(first, "root"))
            self.assertTrue(first.accepts("https://www.reddit.com/comments/abc123"))
            self.assertFalse(first.accepts("https://www.reddit.com/user/example"))
            self.assertFalse(first.accepts("https://x.com/example/status/1234567890123456789"))
            self.assertIsNone(second.settings()["extractor"]["archive"])
            self.assertNotIn("filename", second.settings()["extractor"])
            self.assertEqual(second.settings()["extractor"]["reddit"]["cookies"], "rotated-secret")
            with patch.dict(os.environ, {"ENRICHMENT_FIXTURE_LOGIN": "rotated-secret"}):
                value["gallery"]["extractor"]["sleep-request"] = 7
                self.assertNotEqual(second.policy_sha256, EnrichmentConfiguration.from_document(value, directory).policy_sha256)
            value["gallery"]["extractor"]["reddit"]["cookies"] = "inline-secret"
            with patch.dict(os.environ, {"ENRICHMENT_FIXTURE_LOGIN": "rotated-secret"}), self.assertRaises(InvalidData):
                EnrichmentConfiguration.from_document(value, directory)
