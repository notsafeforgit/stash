from copy import deepcopy
import hashlib
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.discovery_detail_client import DiscoveryDetailClient, MATCH_POLICY
from stash_ingest.discovery_detail_configuration import DiscoveryDetailConfiguration, SCHEMA as PROFILE_SCHEMA
from stash_ingest.discovery_detail_dispatch import DiscoveryDetailDispatcher, DiscoveryDetailCollectionDispatcher
from stash_ingest.discovery_detail_journal import DiscoveryDetailJournal
from stash_ingest.encoding import InvalidData, decode, digest, encode, native_json
from stash_ingest.enrichment_client import CAPTURE_POLICY, checkpoint_bytes
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.metadata_bundle import Bundle, MAX_BYTES
from stash_ingest.outbox import Capacity, Conflict, Outbox, SCHEMA
from stash_ingest.worker_dispatch import Profiles, WorkerDispatcher, SCHEMA as DISPATCH_SCHEMA
from helpers import PRODUCER, capture
from test_enrichment_execution import execution_fixture as enrichment_fixture
from test_outbox_migration import schema_thirteen

WHEN = "2026-10-04T00:00:00Z"


def execution_fixture(**changes):
    work = {"version": 1, "generation": 1, "target_uuid": str(uuid.uuid4()), "target_revision": 2,
            "source_sha256": "1" * 64, "post_uuid": str(uuid.uuid4()), "post_revision": 1,
            "candidate_sequence": 1, "post_namespace": "native:reddit", "post_value": "abc123",
            "url": "https://www.reddit.com/comments/abc123", "listing_uuid": str(uuid.uuid4()),
            "definition_sha256": "2" * 64, "page_ordinal": 1, "page_sha256": "3" * 64,
            "collection_uuid": str(uuid.uuid4()), "collection_revision": 1, "root_uuid": None,
            "policy_sha256": "a" * 64, "extractor_version": "1.32.15-dev", "capture_policy": CAPTURE_POLICY,
            **changes}
    return {"job": {"uuid": str(uuid.uuid4()), "kind": "post.verify_candidate", "state": "queued",
        "revision": 1, "fence": 0, "max_attempts": 8, "arguments": work,
        "created_at": WHEN, "updated_at": WHEN, "available_at": WHEN,
        "work_key": hashlib.sha256(native_json(work, 16384)).hexdigest(),
        "resource_key": hashlib.sha256(("enrichment-collection\x00" + work["collection_uuid"]).encode()).hexdigest()}}


def comparison(job, receipt, *, status="corroborated", fence=None):
    work = job["arguments"]
    result = {"job_uuid": job["uuid"], "checkpoint_revision": receipt["revision"], "fence": fence or receipt["fence"],
        "created_at": WHEN, "evidence": {"policy": MATCH_POLICY,
            "post": {"Namespace": work["post_namespace"], "Value": work["post_value"]}, "url": work["url"],
            "page_sha256": work["page_sha256"], "transcript_sha256": receipt["sha256"], "status": status,
            "record_ordinals": list(range(receipt["record_count"])), "pending_count": 0,
            "unresolved_count": receipt["unresolved_count"]}}
    if status == "corroborated":
        result["evidence"].update(basis="exact-title-and-date", witness_ordinal=0)
    return result


class DetailFixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "outbox.sqlite"
        self.now = 1000
        self.box = Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: self.now)
        self.addCleanup(lambda: self.box.close())
        self.journal = DiscoveryDetailJournal(self.box)
        self.execution = execution_fixture()
        self.job = self.execution["job"]
        path = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
        self.bodies = decode(path.read_bytes(), preserve_numbers=True)

    def stage(self, body=None):
        value = self.journal.claim(self.journal.prepare(self.execution), self.job)
        self.lease = {**self.job, "state": "running", "revision": 2, "fence": 1,
                      "owner_uuid": value.state["claim"]["owner_uuid"], "lease_until": "2026-10-04T00:03:00Z"}
        value = self.journal.reserve(self.journal.claimed(value, self.lease))
        body = self.bodies["complete"] if body is None else body
        value = self.journal.checkpoint(value, self.lease, 0, body)
        self.receipt = {"job_uuid": self.job["uuid"], "revision": 1, "fence": 1, "sha256": digest(value.body),
                        "record_count": len(body["records"]), "pending_count": len(body["pending"]),
                        "unresolved_count": len(body["unresolved"]), "created_at": WHEN}
        return value

    def reopen(self):
        self.box.close()
        self.box = Outbox(self.path, "http://fixture.invalid", PRODUCER, clock=lambda: self.now)
        self.journal = DiscoveryDetailJournal(self.box)


class DetailClientTests(DetailFixture):
    def setUp(self):
        super().setUp()
        self.transport = Mock(producer=PRODUCER)
        self.client = DiscoveryDetailClient(self.transport)

    def test_job_pins_exact_candidate_url_original_evidence_runtime_and_scope(self):
        self.transport._request.return_value = self.job
        self.assertEqual(self.client.describe(self.job["uuid"]), self.execution)
        for changes in ({"page_sha256": "f" * 64}, {"version": True}, {"extra": 1}, {"candidate_sequence": False},
                        {"url": "https://x.com/i/web/status/abc123"}, {"post_namespace": "native:instagram"},
                        {"capture_policy": "unknown"}, {"extractor_version": "a\nprivate"}):
            changed = deepcopy(self.job)
            changed["arguments"].update(changes)
            with self.subTest(changes=changes), self.assertRaises(Unavailable):
                self.client._job(changed)
        self.assertEqual(self.client._job(execution_fixture(post_namespace="native:twitter", post_value="12345",
                              url="https://x.com/i/web/status/12345")["job"])["post_value"], "12345")

    def test_admission_and_explicit_retry_preserve_the_original_selection(self):
        work = self.job["arguments"]
        self.transport._request.return_value = self.job
        self.assertEqual(self.client.admit(work["target_uuid"], 2, 1, "a" * 64, work["extractor_version"]), self.job)
        self.assertEqual(decode(self.transport._request.call_args.args[2])["candidate_sequence"], 1)
        other = execution_fixture(candidate_sequence=2)["job"]
        self.transport._request.return_value = other
        with self.assertRaises(Unavailable):
            self.client.admit(work["target_uuid"], 2, 1, "a" * 64, work["extractor_version"])
        ended = {**self.job, "state": "failed"}
        retried = execution_fixture(**{**work, "generation": 2})["job"]
        self.transport._request.side_effect = [ended, retried]
        self.assertEqual(self.client.retry(self.job["uuid"]), retried)
        self.transport._request.side_effect = [ended, other]
        with self.assertRaises(Unavailable):
            self.client.retry(self.job["uuid"])

    def test_successful_empty_checkpoint_and_uncorroborated_receipt_are_valid(self):
        body = Bundle(self.job["arguments"]["url"], self.job["arguments"]["extractor_version"]).checkpoint()
        with self.journal.execution():
            self.stage(body)
        self.transport._request.return_value = self.receipt
        self.assertEqual(self.client.checkpoint(self.job["uuid"], self.lease, 0, body), self.receipt)
        self.assertEqual(decode(self.transport._request.call_args.args[2])["body"], body)
        result = comparison(self.job, self.receipt, status="uncorroborated")
        self.transport._request.return_value = result
        self.assertEqual(self.client.complete(self.job["uuid"], self.lease, self.receipt), result)
        self.assertTrue(self.transport._request.call_args.args[1].endswith("/complete"))
        self.assertEqual(self.client.completion(self.job["uuid"]), result)
        self.assertTrue(self.transport._request.call_args.args[1].endswith("/result"))

    def test_wrong_result_identity_proof_or_attempt_cannot_complete_delivery(self):
        with self.journal.execution():
            value = self.stage()
            value = self.journal.acknowledged(value, self.receipt)
            result = comparison(self.job, self.receipt)
            variants = []
            for key, invalid in (("page_sha256", "f" * 64), ("transcript_sha256", "f" * 64), ("status", "pending"),
                                 ("post", {"Namespace": "native:reddit", "Value": "different"}),
                                 ("record_ordinals", [True]), ("witness_ordinal", True), ("pending_count", 1)):
                other = deepcopy(result)
                other["evidence"][key] = invalid
                variants.append(other)
            variants += [dict(result, checkpoint_revision=2), dict(result, fence=2), dict(result, created_at="invalid")]
            for other in variants:
                with self.assertRaises(Unavailable):
                    self.journal.acknowledged(value, other)
                self.assertEqual(self.journal.find(value.job_uuid), value)
            done = self.journal.acknowledged(value, result)
            self.assertEqual(done.phase, "completed")
            self.assertEqual(self.journal.summary(value.job_uuid)["comparison"], result)
            self.assertNotIn("publication", done.state)
            with self.assertRaises(Conflict):
                self.journal.change(done)

    def test_collections_and_job_readiness_reject_reversed_or_foreign_results(self):
        collection = self.job["arguments"]["collection_uuid"]
        self.transport._request.return_value = [{"uuid": collection}]
        self.assertEqual(self.client.ready_collections("a" * 64, "version"), [{"uuid": collection}])
        self.assertTrue(self.transport._request.call_args.args[1].startswith("/discovery-details/"))
        with self.assertRaises(Unavailable):
            self.client.ready_collections("a" * 64, "version", after=collection)
        candidate = {"uuid": self.job["uuid"], "sequence": 4}
        self.transport._request.return_value = [candidate]
        self.assertEqual(self.client.ready_jobs(collection, "a" * 64, "version"), [candidate])
        for value in ([dict(candidate, sequence=True)], [candidate, candidate], [{**candidate, "extra": 1}]):
            self.transport._request.return_value = value
            with self.assertRaises(Unavailable):
                self.client.ready_jobs(collection, "a" * 64, "version")


class DetailJournalTests(DetailFixture):
    def test_restart_keeps_original_bytes_claim_and_completion_intent(self):
        with self.journal.execution():
            value = self.stage()
        self.reopen()
        self.assertEqual(self.journal.find(value.job_uuid), value)
        with self.journal.execution():
            value = self.journal.acknowledged(value, self.receipt)
            self.assertIsNone(value.body)
            self.assertEqual(value.state["pending"]["kind"], "complete")
        self.reopen()
        with self.journal.execution():
            value = self.journal.acknowledged(self.journal.find(value.job_uuid), comparison(self.job, self.receipt))
        self.assertEqual(value.phase, "completed")

    def test_partial_child_ack_stages_failure_instead_of_positive_comparison(self):
        with self.journal.execution():
            value = self.stage(self.bodies["initial"])
            value = self.journal.acknowledged(value, self.receipt)
            pending = value.state["pending"]
            self.assertEqual((pending["kind"], pending["error_code"]), ("failure", "timeout"))
            result = {"job_uuid": value.job_uuid, "producer_uuid": PRODUCER, **pending["lease"],
                      "error_code": "timeout", "outcome": "retry", "ended_at": WHEN}
            value = self.journal.acknowledged(value, result)
            self.assertEqual(value.phase, "active")
            self.assertIsNone(value.state["claim"])
            self.assertEqual(value.state["checkpoint"], self.receipt)

    def test_detail_reservation_shares_capacity_with_enrichment_and_media_events(self):
        self.box.max_bytes = MAX_BYTES
        with self.journal.execution():
            value = self.journal.reserve(self.journal.prepare(self.execution))
            with self.assertRaises(Capacity):
                self.box.enqueue(encode(capture()))
            other = EnrichmentJournal(self.box)
            with other.execution():
                original = other.prepare(enrichment_fixture())
                with self.assertRaises(Capacity):
                    other.reserve(original)
                self.journal.change(value, reserved=0)
                other.reserve(original)
                with self.assertRaises(Capacity):
                    self.journal.reserve(self.journal.find(value.job_uuid))

    def test_process_death_releases_only_the_lock_and_preserves_source_bytes(self):
        script = """
import os, signal, sys
from stash_ingest.encoding import decode
from stash_ingest.outbox import Outbox
from stash_ingest.discovery_detail_journal import DiscoveryDetailJournal
data=decode(sys.stdin.buffer.read())
box=Outbox(sys.argv[1], 'http://fixture.invalid', data['producer'])
journal=DiscoveryDetailJournal(box)
with journal.execution() as owned:
    assert owned
    job=data['execution']['job']
    value=journal.claim(journal.prepare(data['execution']), job)
    lease=dict(job, state='running', revision=2, fence=1, owner_uuid=value.state['claim']['owner_uuid'])
    value=journal.reserve(journal.claimed(value,lease))
    journal.checkpoint(value,lease,0,data['body'])
    os.kill(os.getpid(),signal.SIGKILL)
"""
        result = subprocess.run([sys.executable, "-B", "-c", script, str(self.path)], input=encode({
            "execution": self.execution, "producer": PRODUCER, "body": self.bodies["complete"]}), capture_output=True, timeout=10)
        self.assertEqual(result.returncode, -9, result.stderr)
        with self.journal.execution() as owned:
            self.assertTrue(owned)
            self.assertEqual(self.journal.find(self.job["uuid"]).body, checkpoint_bytes(self.bodies["complete"]))

    def test_schema_thirteen_promotion_preserves_all_existing_tables_and_pending_evidence(self):
        legacy = EnrichmentJournal(self.box)
        with legacy.execution():
            original = enrichment_fixture()
            value = legacy.claim(legacy.prepare(original), original["job"])
            lease = {**original["job"], "fence": 1, "owner_uuid": value.state["claim"]["owner_uuid"]}
            legacy.checkpoint(legacy.reserve(value), lease, 0, self.bodies["complete"])
        self.box.enqueue(encode(capture()))
        self.box.db.execute("INSERT INTO worker_dispatch(uuid,after_entry,enrichment_delivery_after) VALUES(?,?,?)",
                            (str(uuid.uuid4()), "selected-before-restart", value.job_uuid))
        schema_thirteen(self.box.db)
        tables = [r[0] for r in self.box.db.execute("SELECT name FROM sqlite_schema WHERE type='table'")]
        columns = {name: ','.join(r[1] for r in self.box.db.execute('PRAGMA table_info("'+name+'")')) for name in tables}
        def rows():
            return {name: [tuple(r) for r in self.box.db.execute('SELECT '+columns[name]+' FROM "'+name+'" ORDER BY rowid')] for name in tables}
        before = rows()
        self.reopen()
        self.assertEqual(before, rows())
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], SCHEMA)
        self.assertEqual(self.box.db.execute("PRAGMA foreign_key_check").fetchall(), [])
        self.assertEqual(EnrichmentJournal(self.box).find(value.job_uuid).body, checkpoint_bytes(self.bodies["complete"]))
        self.assertEqual(self.journal.summary()["staged_bytes"], 0)

    def test_unknown_migration_object_rolls_back_without_replacing_the_outbox(self):
        self.box.enqueue(encode(capture()))
        schema_thirteen(self.box.db)
        self.box.db.execute("CREATE TABLE discovery_detail_dispatch(unknown TEXT)")
        before = [tuple(r) for r in self.box.db.execute("SELECT * FROM events")]
        with self.assertRaises(sqlite3.OperationalError):
            Outbox(self.path, self.box.endpoint, PRODUCER)
        self.assertEqual(self.box.db.execute("PRAGMA user_version").fetchone()[0], 13)
        self.assertEqual([tuple(r) for r in self.box.db.execute("SELECT * FROM events")], before)
        self.assertIsNone(self.box.db.execute("SELECT name FROM sqlite_schema WHERE name='discovery_detail_executions'").fetchone())


class DetailDispatchTests(DetailFixture):
    def test_detail_profiles_rotate_with_downloads_across_restarts(self):
        transport = SimpleNamespace(endpoint=self.box.endpoint, producer=PRODUCER)
        profile_path = self.path.parent / "profiles.json"
        profile_path.write_bytes(encode({"schema": DISPATCH_SCHEMA, "uuid": str(uuid.uuid4()), "profiles": [
            {"id": "downloads", "operation": "download", "profile": "downloads.json"},
            {"id": "details", "operation": "post.verify_candidate", "profile": "details.json"}]}))
        profiles = Profiles(profile_path)
        with patch("stash_ingest.worker_dispatch.Configuration"), \
                patch("stash_ingest.worker_dispatch.DiscoveryDetailConfiguration"), \
                patch("stash_ingest.worker_dispatch.Dispatcher") as download, \
                patch("stash_ingest.worker_dispatch.DiscoveryDetailCollectionDispatcher") as detail:
            download.return_value.once.return_value = {"state": "yielded"}
            detail.return_value.once.return_value = {"state": "completed"}
            self.assertEqual(WorkerDispatcher(self.box, transport, profiles).once()["profile_id"], "downloads")
            self.reopen()
            self.assertEqual(WorkerDispatcher(self.box, transport, profiles).once()["profile_id"], "details")
            self.reopen()
            self.assertEqual(WorkerDispatcher(self.box, transport, profiles).once()["profile_id"], "downloads")

    def test_blocked_detail_collection_does_not_prevent_a_peer_from_running(self):
        transport = SimpleNamespace(endpoint=self.box.endpoint, producer=PRODUCER)
        profile = SimpleNamespace(operation="post.verify_candidate", policy_sha256="a" * 64,
                                  extractor_version="version", check=Mock())
        collections = sorted(str(uuid.uuid4()) for _ in range(2))
        with patch.object(DiscoveryDetailClient, "capabilities", return_value={"discovery_detail_collections_protocol": 1}), \
                patch.object(DiscoveryDetailClient, "ready_collections", return_value=[{"uuid": v} for v in collections]), \
                patch.object(DiscoveryDetailDispatcher, "once", side_effect=[{"state": "waiting"}, {"state": "completed"}]):
            worker = DiscoveryDetailCollectionDispatcher(self.box, transport, profile)
            result = worker.once()
            self.assertEqual(result["collection_uuid"], collections[1])
            self.assertEqual(worker.state()["after_collection"], collections[1])

    def test_saved_delivery_works_without_loading_a_website_profile(self):
        with self.journal.execution():
            value = self.stage()
        transport = SimpleNamespace(endpoint=self.box.endpoint, producer=PRODUCER)
        profile_path = self.path.parent / "profiles.json"
        profile_path.write_bytes(encode({"schema": DISPATCH_SCHEMA, "uuid": str(uuid.uuid4()), "profiles": [
            {"id": "details", "operation": "post.verify_candidate", "profile": "missing-profile.json"}]}))
        with patch("stash_ingest.worker_dispatch.deliver_detail", return_value={"state": "delivery_pending"}) as deliver:
            result = WorkerDispatcher(self.box, transport, Profiles(profile_path)).once()
        deliver.assert_called_once_with(self.box, transport, None, value.job_uuid)
        self.assertEqual(result["discovery_detail_delivery"]["state"], "delivery_pending")
        self.assertEqual(result["profiles"][0]["error_code"], "worker_profile_unavailable")

    def test_job_cursor_commits_before_execution_and_wraps_after_restart(self):
        transport = SimpleNamespace(endpoint=self.box.endpoint, producer=PRODUCER)
        profile = SimpleNamespace(operation="post.verify_candidate", policy_sha256="a" * 64, extractor_version="version", check=Mock())
        collection = self.job["arguments"]["collection_uuid"]
        with patch.object(DiscoveryDetailClient, "capabilities", return_value={"discovery_detail_protocol": 1}), \
                patch.object(DiscoveryDetailClient, "ready_jobs", return_value=[{"sequence": 4, "uuid": self.job["uuid"]}]) as ready, \
                patch("stash_ingest.discovery_detail_dispatch.execute", side_effect=RuntimeError("process death")):
            with self.assertRaises(RuntimeError):
                DiscoveryDetailDispatcher(self.box, transport, collection, profile).once()
            self.reopen()
            worker = DiscoveryDetailDispatcher(self.box, transport, collection, profile)
            self.assertEqual(worker.state()["job_after"], 4)
            ready.return_value = []
            self.assertEqual(worker.once()["state"], "waiting")
            self.assertEqual(worker.state()["job_after"], 0)
            self.assertEqual(worker.once()["state"], "idle")
            self.assertEqual(worker.once()["state"], "backoff")

    def test_profile_has_distinct_policy_and_never_accepts_an_account_listing(self):
        document = {"schema": PROFILE_SCHEMA, "source_category": "reddit", "gallery": {}, "bindings": {}}
        profile = DiscoveryDetailConfiguration.from_document(document, self.path.parent)
        self.assertTrue(profile.accepts("https://www.reddit.com/comments/abc123"))
        self.assertFalse(profile.accepts("https://www.reddit.com/user/example/submitted/"))
        self.assertFalse(profile.accepts("https://x.com/i/web/status/12345"))
        with self.assertRaises(InvalidData):
            DiscoveryDetailConfiguration.from_document(dict(document, source_category="instagram"), self.path.parent)


if __name__ == "__main__":
    unittest.main()
