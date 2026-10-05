"""Real producer journal transitions against native acknowledgement table DDL."""

from contextlib import closing
from copy import deepcopy
from datetime import datetime
import hashlib
import json
import re
import sqlite3
import sys
import uuid

from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.discovery_detail_journal import DiscoveryDetailJournal
from stash_ingest.enrichment_journal import EnrichmentJournal
from stash_ingest.encoding import decode, encode
from stash_archive.job_receipts import instant
from stash_archive.storage import InvalidArchive
from test_receipts import ReceiptFixture, ROOT, PRODUCER

# Reuse the producer's contract fixtures; importing them does not run their tests.
sys.path.insert(0, str(ROOT / "integrations/gallery-dl/tests"))
try:
    from test_discovery_client import execution_fixture as discovery_fixture
    from test_enrichment_execution import execution_fixture as enrichment_fixture
    from test_discovery_detail_execution import execution_fixture as detail_fixture, comparison
finally:
    sys.path.pop(0)

WHEN = "2026-10-04T14:00:00Z"
WHEN_MS = int(datetime.fromisoformat(WHEN).timestamp()) * 1000
ATTEMPTS = {"enrichment":"enrichment_job_attempts", "discovery":"discovery_job_attempts", "discovery_detail":"discovery_detail_attempts"}


class JournalFixture(ReceiptFixture):
    def setUp(self):
        super().setUp()
        # Supporting domain parents are deliberately small, as in ReceiptFixture;
        # the acknowledgement tables below use the actual native migration DDL.
        for field in ("kind TEXT", "arguments TEXT", "state TEXT", "fence INTEGER", "revision INTEGER"):
            self.db.execute("ALTER TABLE archive_jobs ADD COLUMN " + field)
        self.db.executescript("""
            CREATE TABLE enrichment_job_targets(job_uuid TEXT PRIMARY KEY);
            CREATE TABLE discovery_listing_jobs(job_uuid TEXT PRIMARY KEY);
            CREATE TABLE discovery_detail_jobs(job_uuid TEXT PRIMARY KEY);
            CREATE TABLE enrichment_targets(uuid TEXT PRIMARY KEY,url_uuid TEXT,policy TEXT);
            CREATE TABLE source_post_urls(uuid TEXT PRIMARY KEY,url TEXT);
            CREATE TABLE enrichment_completions(uuid TEXT PRIMARY KEY,capture_count INTEGER);
            CREATE TABLE source_accounts(uuid TEXT PRIMARY KEY);
        """)
        migrations = {
            "1000019_archive_jobs.up.sql": ("archive_job_attempts",),
            "1000051_enrichment_jobs.up.sql": ("enrichment_job_attempts", "enrichment_checkpoint_receipts", "enrichment_checkpoints"),
            "1000052_enrichment_publications.up.sql": ("enrichment_publications",),
            "1000053_enrichment_checkpoint_release.up.sql": ("enrichment_checkpoint_releases",),
            "1000071_discovery_listing_jobs.up.sql": ("discovery_listings", "discovery_job_attempts", "discovery_pages"),
            "1000076_discovery_detail_jobs.up.sql": ("discovery_detail_attempts", "discovery_detail_checkpoint_receipts",
                                                      "discovery_detail_checkpoints", "discovery_detail_results"),
        }
        for name, tables in migrations.items():
            source = (ROOT / "pkg/sqlite/migrations" / name).read_text()
            for table in tables:
                ddl = re.search(r"CREATE TABLE " + table + r" \([\s\S]*?\n\);", source)
                self.assertIsNotNone(ddl, table)
                self.db.executescript(ddl.group())
        self.bodies = decode((ROOT / "pkg/archive/testdata/enrichment-transcript-v1.json").read_bytes(), preserve_numbers=True)
        self.db.commit()

    def prepare(self, family, *, claimed=True, claim_only=False):
        if family == "discovery":
            execution, body = discovery_fixture()
            journal = DiscoveryJournal(self.box)
            listing = execution["listing"]
            self.db.execute("INSERT INTO source_accounts VALUES(?)", (listing["account_uuid"],))
            self.db.execute("INSERT INTO source_collection_revisions VALUES(?,?)", (listing["collection_uuid"], listing["collection_revision"]))
            definition = {k:v for k,v in listing.items() if k not in ("sha256", "created_at")}
            self.db.execute("INSERT INTO discovery_listings VALUES(?,?,?,?,?,?,?,?)", (
                listing["uuid"], listing["account_uuid"], listing["collection_uuid"], listing["collection_revision"],
                listing["root_uuid"], json.dumps(definition), listing["sha256"], WHEN))
        elif family == "enrichment":
            execution, body = enrichment_fixture(), self.bodies["complete"]
            journal = EnrichmentJournal(self.box)
            target, url_id = execution["target"], str(uuid.uuid4())
            self.db.execute("INSERT INTO source_post_urls VALUES(?,?)", (url_id, target["url"]))
            self.db.execute("INSERT INTO enrichment_targets VALUES(?,?,?)", (target["uuid"], url_id, target["policy"]))
        else:
            execution, body = detail_fixture(), self.bodies["complete"]
            journal = DiscoveryDetailJournal(self.box)
        job = execution["job"]
        self.db.execute("INSERT INTO archive_jobs VALUES(?,?,?,?,?,?)", (job["uuid"], job["kind"], json.dumps(job["arguments"]), "queued", 0, 1))
        parent = {"enrichment":"enrichment_job_targets", "discovery":"discovery_listing_jobs", "discovery_detail":"discovery_detail_jobs"}[family]
        self.db.execute(f"INSERT INTO {parent} VALUES(?)", (job["uuid"],))
        with journal.execution():
            value = journal.prepare(execution)
            if claimed or claim_only:
                value = journal.claim(value, execution if family == "discovery" else job)
            if claimed:
                lease = {**job, "state":"running", "revision":2, "fence":1,
                         "owner_uuid":value.state["claim"]["owner_uuid"], "lease_until":"2026-10-04T14:03:00Z"}
                value = journal.claimed(value, lease)
                self.db.execute("UPDATE archive_jobs SET state='running',fence=1,revision=2 WHERE uuid=?", (job["uuid"],))
                self.db.execute("INSERT INTO archive_job_attempts(job_uuid,fence,owner_uuid,started_at_ms) VALUES(?,1,?,?)",
                                (job["uuid"], lease["owner_uuid"], WHEN_MS))
                self.db.execute(f"INSERT INTO {ATTEMPTS[family]} VALUES(?,1,?)", (job["uuid"], PRODUCER))
        self.db.commit()
        return journal, value, body

    def stage(self, family, *, body=None):
        journal, value, default = self.prepare(family)
        with journal.execution():
            value = journal.reserve(value)
            if family == "discovery":
                value = journal.page(value, value.state["lease"], body or default)
            else:
                value = journal.checkpoint(value, value.state["lease"], 0, body or default)
        return journal, value

    def receive(self, family, value, *, revision=1, raw=None):
        raw = value.body if raw is None else raw
        body = decode(raw, preserve_numbers=True)
        receipt = {"job_uuid":value.job_uuid, "fence":1, "sha256":hashlib.sha256(raw).hexdigest(),
                   "record_count":len(body["records"]), "created_at":WHEN}
        if family == "discovery":
            receipt.update(listing_uuid=value.definition["listing"]["uuid"], ordinal=value.definition["arguments"]["page_ordinal"],
                           producer_uuid=PRODUCER, complete=body["complete"])
            self.db.execute("INSERT INTO discovery_pages VALUES(?,?,?,?,?,?,?,?,?,?,?)", (
                receipt["listing_uuid"],receipt["ordinal"],value.job_uuid,1,PRODUCER,receipt["sha256"],len(raw),
                receipt["record_count"],int(receipt["complete"]),raw.decode(),WHEN))
            self.succeed(value.job_uuid)
        else:
            receipt.update(revision=revision, pending_count=len(body["pending"]), unresolved_count=len(body["unresolved"]))
            self.db.execute(f"INSERT INTO {family}_checkpoint_receipts VALUES(?,?,?,?,?,?,?,?)", (
                value.job_uuid,revision,receipt["sha256"],1,receipt["record_count"],receipt["pending_count"],receipt["unresolved_count"],WHEN))
            self.db.execute(f"INSERT OR REPLACE INTO {family}_checkpoints VALUES(?,?,?,?)", (value.job_uuid,revision,raw.decode(),len(raw)))
        self.db.commit()
        return receipt

    def succeed(self, job):
        self.db.execute("UPDATE archive_jobs SET state='succeeded',revision=revision+1 WHERE uuid=?", (job,))
        self.db.execute("UPDATE archive_job_attempts SET outcome='succeeded',ended_at_ms=? WHERE job_uuid=?", (WHEN_MS + 1000, job))

    def finish(self, family, journal, value, receipt):
        with journal.execution():
            value = journal.acknowledged(value, receipt)
            if family == "discovery":
                return value
            if family == "enrichment":
                completed = {"job_uuid":value.job_uuid, "checkpoint_revision":receipt["revision"], "fence":1,
                    "checkpoint_sha256":receipt["sha256"], "completion_uuid":str(uuid.uuid4()),
                    "record_count":receipt["record_count"], "capture_count":receipt["record_count"],
                    "unresolved_count":receipt["unresolved_count"], "created_at":WHEN}
                self.db.execute("INSERT INTO enrichment_completions VALUES(?,?)", (completed["completion_uuid"],completed["capture_count"]))
                self.db.execute("INSERT INTO enrichment_publications VALUES(?,?,?,?,?)", (value.job_uuid,receipt["revision"],1,completed["completion_uuid"],WHEN))
            else:
                completed = comparison(value.state["lease"], receipt)
                self.db.execute("INSERT INTO discovery_detail_results VALUES(?,?,?,?,?)", (
                    value.job_uuid,receipt["revision"],1,json.dumps(completed["evidence"]),completed["created_at"]))
            self.succeed(value.job_uuid)
            self.db.commit()
            return journal.acknowledged(value, completed)

    def counts(self, family, **kwargs):
        return self.verify(**kwargs)["producers"][0]["job_journals"][family]["counts"]


class JobReceiptTests(JournalFixture):
    def test_all_completed_journals_match_after_bodies_are_released_without_writes(self):
        for family in ATTEMPTS:
            journal, value = self.stage(family)
            receipt = self.receive(family, value)
            completed = self.finish(family, journal, value, receipt)
            self.assertIsNone(completed.body)
        before = list(self.box.db.iterdump())
        report = self.verify()
        for family in ATTEMPTS:
            self.assertEqual(report["producers"][0]["job_journals"][family]["counts"]["completions"], 1)
        self.assertEqual(before, list(self.box.db.iterdump()))
        self.assertEqual(report, self.verify())
        self.assertEqual(self.db.execute("PRAGMA foreign_key_check").fetchall(), [])

    def test_lost_replies_keep_exact_bodies_and_original_attempts(self):
        for family in ATTEMPTS:
            journal, value = self.stage(family)
            self.receive(family, value)
            report = self.counts(family)
            self.assertEqual(report["accepted_pending_bodies"], 1)
            self.assertEqual(report["pending_bodies"], 1)
            self.assertEqual(journal.find(value.job_uuid), value)

    def test_old_native_snapshot_cannot_cover_discarded_job_payloads(self):
        journal, value = self.stage("discovery_detail")
        old = self.root / "old.sqlite"
        with closing(sqlite3.connect(old)) as target:
            self.db.backup(target)
        receipt = self.receive("discovery_detail", value)
        with journal.execution():
            journal.acknowledged(value, receipt)
        with self.assertRaisesRegex(InvalidArchive, "checkpoint is missing"):
            self.verify(library=old)
        self.verify()

    def test_pending_claims_need_no_invented_attempt_but_acknowledged_leases_do(self):
        journal, value, _ = self.prepare("discovery", claimed=False, claim_only=True)
        self.assertIsNotNone(value.state["claim"])
        self.assertIsNone(value.state["lease"])
        self.assertEqual(self.counts("discovery")["active"], 1)
        other, staged = self.stage("enrichment")
        self.db.execute("DELETE FROM enrichment_job_attempts WHERE job_uuid=?", (staged.job_uuid,))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "attempt is missing"):
            self.verify()

    def test_changed_native_identity_owner_and_pending_bytes_are_rejected(self):
        journal, value = self.stage("discovery")
        owner = value.state["lease"]["owner_uuid"]
        self.db.execute("UPDATE archive_job_attempts SET owner_uuid=?", (str(uuid.uuid4()),))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "another owner"):
            self.verify()
        self.db.execute("UPDATE archive_job_attempts SET owner_uuid=?", (owner,))
        self.db.commit()
        self.box.db.execute("UPDATE discovery_executions SET body=zeroblob(length(body))")
        with self.assertRaisesRegex(InvalidArchive, "exact retained bytes"):
            self.verify()

    def test_listing_conflicting_acceptance_and_changed_retained_page_fail(self):
        journal, value = self.stage("discovery")
        self.receive("discovery", value)
        self.db.execute("UPDATE discovery_pages SET digest=?", ("b" * 64,))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "conflicts with its native acceptance"):
            self.verify()
        self.db.execute("UPDATE discovery_pages SET digest=?", (hashlib.sha256(value.body).hexdigest(),))
        # Retain valid JSON and byte_count while proving that payload hashes matter.
        text = value.body.decode().replace('reddit.com', 'reddit.net')
        self.db.execute("UPDATE discovery_pages SET body=?", (text,))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "exact retained bytes"):
            self.verify()

    def test_older_checkpoint_acknowledgement_survives_newer_native_head(self):
        journal, value = self.stage("enrichment", body=self.bodies["initial"])
        first = self.receive("enrichment", value)
        with journal.execution():
            acknowledged = journal.acknowledged(value, first)
        from stash_ingest.enrichment_client import checkpoint_bytes
        self.receive("enrichment", value, revision=2, raw=checkpoint_bytes(self.bodies["complete"]))
        self.assertEqual(self.counts("enrichment")["checkpoints"], 1)
        self.assertEqual(journal.find(value.job_uuid), acknowledged)

    def test_metadata_checkpoint_requires_retained_body_or_native_release(self):
        journal, value = self.stage("enrichment")
        receipt = self.receive("enrichment", value)
        self.finish("enrichment", journal, value, receipt)
        self.db.execute("DELETE FROM enrichment_checkpoints")
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "no retained body or release"):
            self.verify()
        self.db.execute("INSERT INTO enrichment_checkpoint_releases VALUES(?,1,?,?,?,?)", (value.job_uuid,"a"*64,len(value.body),'[]',WHEN))
        self.db.commit()
        self.assertEqual(self.counts("enrichment")["completions"], 1)

    def test_timestamps_keep_nanoseconds_and_normalize_timezones(self):
        self.assertEqual(instant("2026-10-04T07:00:00.123456789-07:00"), instant("2026-10-04 14:00:00.123456789", native=True))
        self.assertNotEqual(instant("2026-10-04T14:00:00.123456789Z"), instant("2026-10-04T14:00:00.123456788Z"))

    def test_failure_acknowledgements_preserve_retry_and_reject_changed_history(self):
        for family in ATTEMPTS:
            with self.subTest(family=family):
                journal, value, _ = self.prepare(family)
                lease = value.state["lease"]
                with journal.execution():
                    if family == "discovery":
                        value = journal.failure(value, lease, "timeout")
                    else:
                        value = journal.intent(value, lease, "failure", {"error_code":"timeout"})
                    receipt = {"job_uuid":value.job_uuid, "producer_uuid":PRODUCER, "owner_uuid":lease["owner_uuid"],
                        "fence":1, "outcome":"retry", "error_code":"timeout", "result":{},
                        "started_at":WHEN, "ended_at":"2026-10-04T14:00:01Z"}
                    self.db.execute("UPDATE archive_job_attempts SET outcome='retry',error_code='timeout',ended_at_ms=? WHERE job_uuid=?",
                                    (WHEN_MS + 1000, value.job_uuid))
                    self.db.execute("UPDATE archive_jobs SET state='queued',revision=3 WHERE uuid=?", (value.job_uuid,))
                    self.db.commit()
                    value = journal.acknowledged(value, receipt)
                self.assertEqual(self.counts(family)["failures"], 1)
                self.assertEqual(value.phase, "active")
                self.assertIsNone(value.state["lease"])
                self.db.execute("UPDATE archive_job_attempts SET ended_at_ms=ended_at_ms+1 WHERE job_uuid=?", (value.job_uuid,))
                self.db.commit()
                with self.assertRaisesRegex(InvalidArchive, "time differs"):
                    self.verify()
                self.db.execute("UPDATE archive_job_attempts SET ended_at_ms=ended_at_ms-1 WHERE job_uuid=?", (value.job_uuid,))
                self.db.commit()

    def test_observed_listing_completion_retains_native_terminal_receipt(self):
        journal, value = self.stage("discovery")
        receipt = self.receive("discovery", value)
        with journal.execution():
            # Another local outbox may learn an already completed page without
            # ever having staged its own body. Use a second journal for that case.
            from stash_ingest.outbox import Outbox
            from test_receipts import ORIGIN
            path = self.root / "observer.sqlite"
            with closing(Outbox(path, ORIGIN, PRODUCER)) as box:
                observer = DiscoveryJournal(box)
                job = {**value.state["lease"], "state":"succeeded", "revision":3,
                       "result":{"listing_uuid":receipt["listing_uuid"],"page_ordinal":receipt["ordinal"],"page_sha256":receipt["sha256"]}}
                job.pop("owner_uuid")
                job.pop("lease_until")
                description = {"job":job,"listing":value.definition["listing"],"cursor":value.definition["cursor"],"receipt":receipt}
                with observer.execution():
                    observed = observer.observe_terminal(observer.prepare(value.state["claim"]["description"]), description)
                self.assertEqual(observed.phase, "completed")
            self.assertEqual(self.counts("discovery", outbox=path)["completions"], 1)
        self.db.execute("UPDATE archive_jobs SET state='cancelled' WHERE uuid=?", (value.job_uuid,))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "terminal state differs"):
            self.verify(outbox=path)

    def test_changed_comparison_receipt_cannot_be_presented_as_native_completion(self):
        journal, value = self.stage("discovery_detail")
        receipt = self.receive("discovery_detail", value)
        completed = self.finish("discovery_detail", journal, value, receipt)
        self.assertEqual(self.counts("discovery_detail")["completions"], 1)
        evidence = deepcopy(completed.state["comparison"]["evidence"])
        evidence["status"] = "uncorroborated"
        evidence.pop("basis")
        evidence.pop("witness_ordinal")
        self.db.execute("UPDATE discovery_detail_results SET evidence=?", (json.dumps(evidence),))
        self.db.commit()
        with self.assertRaisesRegex(InvalidArchive, "completion acknowledgement differs"):
            self.verify()

    def test_schema_downgrade_cannot_hide_journal_acknowledgements(self):
        self.box.db.execute("PRAGMA user_version=13")
        with self.assertRaisesRegex(InvalidArchive, "omit or invent"):
            self.verify()
