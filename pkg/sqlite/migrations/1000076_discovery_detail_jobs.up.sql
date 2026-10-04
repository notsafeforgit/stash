-- Preserve existing job rows, receipts, attempts, indexes and guards.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_detail_archive_job_rows AS SELECT * FROM archive_jobs;
DROP TABLE archive_jobs;

CREATE TABLE archive_jobs (
 id INTEGER PRIMARY KEY,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind IN ('media.verify','album.backfill','text.translate','post.enrich','account.list_page','post.verify_candidate')),
 work_key TEXT NOT NULL CHECK(length(work_key)=64 AND work_key NOT GLOB '*[^0-9a-f]*'),
 resource_key TEXT NOT NULL CHECK(length(resource_key)=64 AND resource_key NOT GLOB '*[^0-9a-f]*'),
 arguments TEXT NOT NULL CHECK(json_valid(arguments) AND json_type(arguments)='object' AND length(CAST(arguments AS BLOB))<=262144),
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
 priority INTEGER NOT NULL CHECK(typeof(priority)='integer' AND priority BETWEEN 0 AND 100),
 fence INTEGER NOT NULL DEFAULT 0 CHECK(typeof(fence)='integer' AND fence>=0),
 max_attempts INTEGER NOT NULL CHECK(typeof(max_attempts)='integer' AND max_attempts BETWEEN 1 AND 100),
 available_at_ms INTEGER NOT NULL CHECK(typeof(available_at_ms)='integer' AND available_at_ms>0),
 owner_uuid TEXT,
 lease_until_ms INTEGER,
 progress TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(progress) AND json_type(progress)='object' AND length(CAST(progress AS BLOB))<=16384),
 result TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(result) AND json_type(result)='object' AND length(CAST(result AS BLOB))<=16384),
 error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=128),
 created_at_ms INTEGER NOT NULL CHECK(typeof(created_at_ms)='integer' AND created_at_ms>0),
 updated_at_ms INTEGER NOT NULL CHECK(typeof(updated_at_ms)='integer' AND updated_at_ms>=created_at_ms),
 CHECK(fence<=max_attempts AND (state!='queued' OR fence<max_attempts)),
 CHECK((state='running' AND owner_uuid IS NOT NULL AND length(owner_uuid)=36
   AND typeof(lease_until_ms)='integer' AND lease_until_ms>0 AND fence>0)
   OR (state!='running' AND owner_uuid IS NULL AND lease_until_ms IS NULL))
);

INSERT INTO archive_jobs SELECT * FROM native_detail_archive_job_rows;
DROP TABLE native_detail_archive_job_rows;

CREATE UNIQUE INDEX archive_jobs_active_work ON archive_jobs(kind,work_key) WHERE state IN ('queued','running');

CREATE UNIQUE INDEX archive_jobs_enrichment_target ON archive_jobs(json_extract(arguments,'$.target_uuid'))
 WHERE kind='post.enrich' AND state IN ('queued','running');

CREATE INDEX archive_jobs_expired ON archive_jobs(lease_until_ms,id) WHERE state='running';

CREATE INDEX archive_jobs_list ON archive_jobs(kind,state,id);

CREATE INDEX archive_jobs_ready ON archive_jobs(kind,available_at_ms,priority DESC,id) WHERE state='queued';

CREATE INDEX archive_jobs_resource_history ON archive_jobs(kind,resource_key,id);

CREATE UNIQUE INDEX archive_jobs_running_resource ON archive_jobs(resource_key) WHERE state='running';

CREATE UNIQUE INDEX archive_jobs_translation_request ON archive_jobs(json_extract(arguments,'$.request_uuid'))
 WHERE kind='text.translate' AND state IN ('queued','running');

CREATE TRIGGER archive_job_identity BEFORE UPDATE ON archive_jobs
WHEN NEW.id!=OLD.id OR NEW.uuid!=OLD.uuid OR NEW.kind!=OLD.kind OR NEW.work_key!=OLD.work_key
 OR NEW.resource_key!=OLD.resource_key OR NEW.arguments!=OLD.arguments OR NEW.max_attempts!=OLD.max_attempts
 OR NEW.created_at_ms!=OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'archive job identity is immutable'); END;

CREATE TRIGGER archive_job_transition BEFORE UPDATE ON archive_jobs
WHEN OLD.state IN ('succeeded','failed','cancelled') OR NEW.revision!=OLD.revision+1
 OR NEW.updated_at_ms<OLD.updated_at_ms
 OR (OLD.state='queued' AND NEW.state NOT IN ('queued','running','failed','cancelled'))
 OR (NEW.state='running' AND OLD.state='queued' AND NEW.fence!=OLD.fence+1)
 OR (NOT (NEW.state='running' AND OLD.state='queued') AND NEW.fence!=OLD.fence)
 OR (OLD.state='running' AND NEW.state='running' AND NEW.owner_uuid!=OLD.owner_uuid)
BEGIN SELECT RAISE(ABORT,'invalid archive job transition'); END;

CREATE TRIGGER enrichment_job_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='post.enrich' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM enrichment_publications p
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
 JOIN enrichment_completions e ON e.uuid=p.completion_uuid
 WHERE p.job_uuid=NEW.uuid AND p.fence=NEW.fence
 AND r.record_count=(SELECT count(*) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid)
 AND e.capture_count=(SELECT count(DISTINCT capture_uuid) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid))
BEGIN SELECT RAISE(ABORT,'enrichment success requires complete native capture publication'); END;

CREATE TRIGGER source_enrichment_waiter_end AFTER UPDATE ON archive_jobs
WHEN NEW.kind IN ('post.enrich','account.list_page','post.verify_candidate') AND NEW.revision!=OLD.revision
BEGIN DELETE FROM source_enrichment_waiters WHERE job_uuid=NEW.uuid; END;

DROP TRIGGER enrichment_attempt_pacing_current;
CREATE TRIGGER enrichment_attempt_pacing_current BEFORE INSERT ON enrichment_attempt_pacing
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.kind IN ('post.enrich','account.list_page','post.verify_candidate') AND j.state='running' AND j.fence=NEW.fence)
BEGIN SELECT RAISE(ABORT,'source reservation requires the current metadata attempt'); END;
DROP TRIGGER source_enrichment_waiter_current;
CREATE TRIGGER source_enrichment_waiter_current BEFORE INSERT ON source_enrichment_waiters
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.kind IN ('post.enrich','account.list_page','post.verify_candidate') AND j.state='queued' AND j.fence=NEW.fence)
BEGIN SELECT RAISE(ABORT,'source waiter requires the current queued metadata job'); END;

CREATE UNIQUE INDEX archive_jobs_discovery_listing ON archive_jobs(json_extract(arguments,'$.listing_uuid'))
 WHERE kind='account.list_page' AND state IN ('queued','running');
CREATE TRIGGER discovery_listing_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='account.list_page' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM discovery_listing_jobs b JOIN discovery_pages p ON p.listing_uuid=b.listing_uuid
 WHERE b.job_uuid=NEW.uuid AND b.page_ordinal=p.ordinal AND p.job_uuid=NEW.uuid AND p.fence=NEW.fence
 AND json_extract(NEW.result,'$.listing_uuid')=b.listing_uuid
 AND json_extract(NEW.result,'$.page_ordinal')=p.ordinal AND json_extract(NEW.result,'$.page_sha256')=p.digest)
BEGIN SELECT RAISE(ABORT,'listing job success requires its retained page'); END;

CREATE TABLE discovery_detail_jobs (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES archive_jobs(uuid),
 target_uuid TEXT NOT NULL REFERENCES discovery_match_targets(uuid),
 target_revision INTEGER NOT NULL CHECK(typeof(target_revision)='integer' AND target_revision>0),
 candidate_sequence INTEGER NOT NULL REFERENCES discovery_match_candidates(id),
 generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0),
 UNIQUE(target_uuid,target_revision,candidate_sequence,generation)
);
CREATE TRIGGER discovery_detail_job_immutable BEFORE UPDATE ON discovery_detail_jobs
BEGIN SELECT RAISE(ABORT,'discovery detail selection is immutable'); END;
CREATE TRIGGER discovery_detail_job_scope BEFORE INSERT ON discovery_detail_jobs
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN discovery_match_targets t ON t.uuid=NEW.target_uuid
 JOIN discovery_match_candidates c ON c.id=NEW.candidate_sequence AND c.target_uuid=t.uuid
 JOIN discovery_match_evidence e ON e.target_uuid=t.uuid AND e.page_ordinal=c.best_page AND e.namespace=c.namespace AND e.value=c.value
 JOIN discovery_pages p ON p.listing_uuid=t.listing_uuid AND p.ordinal=c.best_page
 JOIN discovery_listings d ON d.uuid=t.listing_uuid
 WHERE j.uuid=NEW.job_uuid AND j.kind='post.verify_candidate' AND j.state='queued' AND j.fence=0
 AND t.revision=NEW.target_revision AND e.needs_detail=1
 AND json_extract(j.arguments,'$.version')=1 AND json_extract(j.arguments,'$.generation')=NEW.generation
 AND json_extract(j.arguments,'$.target_uuid')=t.uuid AND json_extract(j.arguments,'$.target_revision')=t.revision
 AND json_extract(j.arguments,'$.source_sha256')=t.source_sha256 AND json_extract(j.arguments,'$.post_uuid')=t.post_uuid
 AND json_extract(j.arguments,'$.post_revision')=t.post_revision AND json_extract(j.arguments,'$.candidate_sequence')=c.id
 AND json_extract(j.arguments,'$.post_namespace')=c.namespace AND json_extract(j.arguments,'$.post_value')=c.value
 AND json_extract(j.arguments,'$.url')=e.url AND json_extract(j.arguments,'$.listing_uuid')=t.listing_uuid
 AND json_extract(j.arguments,'$.definition_sha256')=d.digest AND json_extract(j.arguments,'$.page_ordinal')=p.ordinal
 AND json_extract(j.arguments,'$.page_sha256')=p.digest AND json_extract(j.arguments,'$.collection_uuid')=d.collection_uuid
 AND json_extract(j.arguments,'$.collection_revision')=d.collection_revision AND json_extract(j.arguments,'$.root_uuid') IS d.root_uuid)
BEGIN SELECT RAISE(ABORT,'discovery detail job does not match its candidate'); END;
CREATE UNIQUE INDEX archive_jobs_discovery_detail ON archive_jobs(json_extract(arguments,'$.target_uuid'),json_extract(arguments,'$.candidate_sequence'))
 WHERE kind='post.verify_candidate' AND state IN ('queued','running');
CREATE TRIGGER discovery_detail_pacing_bind AFTER INSERT ON discovery_detail_jobs
BEGIN
 INSERT OR IGNORE INTO source_pacing(scope)
 SELECT source_scope_v1(json_extract(arguments,'$.url')) FROM archive_jobs WHERE uuid=NEW.job_uuid;
 INSERT INTO enrichment_job_pacing(job_uuid,scope)
 SELECT NEW.job_uuid,source_scope_v1(json_extract(arguments,'$.url')) FROM archive_jobs WHERE uuid=NEW.job_uuid;
END;
CREATE TABLE discovery_detail_attempts (
 job_uuid TEXT NOT NULL REFERENCES discovery_detail_jobs(job_uuid),
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>0),
 producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
 PRIMARY KEY(job_uuid,fence),
 FOREIGN KEY(job_uuid,fence) REFERENCES archive_job_attempts(job_uuid,fence)
);
CREATE TRIGGER discovery_detail_attempt_immutable BEFORE UPDATE ON discovery_detail_attempts
BEGIN SELECT RAISE(ABORT,'discovery detail attempt producer is immutable'); END;
CREATE TRIGGER discovery_detail_attempt_scope BEFORE INSERT ON discovery_detail_attempts
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN archive_job_attempts a ON a.job_uuid=j.uuid AND a.fence=j.fence
 WHERE j.uuid=NEW.job_uuid AND j.fence=NEW.fence AND j.kind='post.verify_candidate' AND j.state='running'
 AND a.outcome='running' AND a.owner_uuid=j.owner_uuid)
BEGIN SELECT RAISE(ABORT,'discovery detail producer requires the current running attempt'); END;
CREATE TABLE discovery_detail_checkpoint_receipts (
 job_uuid TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision BETWEEN 1 AND 128),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 fence INTEGER NOT NULL,
 record_count INTEGER NOT NULL CHECK(typeof(record_count)='integer' AND record_count BETWEEN 0 AND 1024),
 pending_count INTEGER NOT NULL CHECK(typeof(pending_count)='integer' AND pending_count BETWEEN 0 AND 256),
 unresolved_count INTEGER NOT NULL CHECK(typeof(unresolved_count)='integer' AND unresolved_count BETWEEN 0 AND 256),
 created_at DATETIME NOT NULL,
 PRIMARY KEY(job_uuid,revision),
 UNIQUE(job_uuid,digest),
 FOREIGN KEY(job_uuid,fence) REFERENCES discovery_detail_attempts(job_uuid,fence)
);
CREATE TRIGGER discovery_detail_checkpoint_receipt_immutable BEFORE UPDATE ON discovery_detail_checkpoint_receipts
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint receipts are immutable'); END;
CREATE TRIGGER discovery_detail_checkpoint_receipt_scope BEFORE INSERT ON discovery_detail_checkpoint_receipts
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.state='running' AND j.fence=NEW.fence)
 OR NEW.revision!=coalesce((SELECT revision+1 FROM discovery_detail_checkpoints WHERE job_uuid=NEW.job_uuid),1)
 OR NEW.record_count<coalesce((SELECT r.record_count FROM discovery_detail_checkpoints h
  JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=NEW.job_uuid),0)
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint requires a continuing owned attempt'); END;

CREATE TABLE discovery_detail_checkpoints (
 job_uuid TEXT PRIMARY KEY NOT NULL,
 revision INTEGER NOT NULL,
 body TEXT NOT NULL CHECK(json_valid(body) AND json_type(body)='object'),
 byte_size INTEGER NOT NULL CHECK(typeof(byte_size)='integer' AND byte_size=length(CAST(body AS BLOB)) AND byte_size BETWEEN 1 AND 33554432),
 FOREIGN KEY(job_uuid,revision) REFERENCES discovery_detail_checkpoint_receipts(job_uuid,revision)
);
CREATE TRIGGER discovery_detail_checkpoint_initial BEFORE INSERT ON discovery_detail_checkpoints
WHEN NEW.revision!=1
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint must begin at revision one'); END;
CREATE TRIGGER discovery_detail_checkpoint_transition BEFORE UPDATE ON discovery_detail_checkpoints
WHEN NEW.job_uuid!=OLD.job_uuid OR NEW.revision!=OLD.revision+1
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint revision must advance'); END;

CREATE TABLE discovery_detail_checkpoint_records (
 job_uuid TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(typeof(ordinal)='integer' AND ordinal BETWEEN 0 AND 1023),
 checkpoint_revision INTEGER NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 PRIMARY KEY(job_uuid,ordinal),
 FOREIGN KEY(job_uuid,checkpoint_revision) REFERENCES discovery_detail_checkpoint_receipts(job_uuid,revision)
);
CREATE INDEX discovery_detail_checkpoint_records_receipt ON discovery_detail_checkpoint_records(job_uuid,checkpoint_revision);
CREATE TRIGGER discovery_detail_checkpoint_record_immutable BEFORE UPDATE ON discovery_detail_checkpoint_records
BEGIN SELECT RAISE(ABORT,'enrichment observation provenance is immutable'); END;
CREATE TRIGGER discovery_detail_checkpoint_record_scope BEFORE INSERT ON discovery_detail_checkpoint_records
WHEN NOT EXISTS(SELECT 1 FROM discovery_detail_checkpoint_receipts r
 WHERE r.job_uuid=NEW.job_uuid AND r.revision=NEW.checkpoint_revision AND NEW.ordinal<r.record_count
 AND NEW.ordinal>=coalesce((SELECT p.record_count FROM discovery_detail_checkpoint_receipts p
  WHERE p.job_uuid=r.job_uuid AND p.revision=r.revision-1),0))
BEGIN SELECT RAISE(ABORT,'enrichment observation is outside its first checkpoint'); END;

-- Bounded staging includes failed/cancelled jobs: their evidence is retained
-- until publication or an explicit future discard, rather than silently lost.
CREATE TABLE discovery_detail_checkpoint_usage (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 byte_size INTEGER NOT NULL CHECK(typeof(byte_size)='integer' AND byte_size BETWEEN 0 AND 2147483648)
);
INSERT INTO discovery_detail_checkpoint_usage VALUES(1,0);
CREATE TRIGGER discovery_detail_checkpoint_usage_insert AFTER INSERT ON discovery_detail_checkpoints
BEGIN UPDATE discovery_detail_checkpoint_usage SET byte_size=byte_size+NEW.byte_size WHERE singleton=1; END;
CREATE TRIGGER discovery_detail_checkpoint_usage_update AFTER UPDATE ON discovery_detail_checkpoints
BEGIN UPDATE discovery_detail_checkpoint_usage SET byte_size=byte_size-OLD.byte_size+NEW.byte_size WHERE singleton=1; END;
CREATE TRIGGER discovery_detail_checkpoint_usage_delete AFTER DELETE ON discovery_detail_checkpoints
BEGIN UPDATE discovery_detail_checkpoint_usage SET byte_size=byte_size-OLD.byte_size WHERE singleton=1; END;

CREATE TABLE discovery_detail_results (
 job_uuid TEXT PRIMARY KEY NOT NULL,
 checkpoint_revision INTEGER NOT NULL,
 fence INTEGER NOT NULL,
 evidence TEXT NOT NULL CHECK(json_valid(evidence) AND json_type(evidence)='object' AND length(CAST(evidence AS BLOB))<=16384),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(job_uuid,checkpoint_revision) REFERENCES discovery_detail_checkpoint_receipts(job_uuid,revision),
 FOREIGN KEY(job_uuid,fence) REFERENCES discovery_detail_attempts(job_uuid,fence),
 CHECK(json_extract(evidence,'$.status') IN ('corroborated','uncorroborated') AND json_extract(evidence,'$.pending_count')=0)
);
CREATE TRIGGER discovery_detail_result_immutable BEFORE UPDATE ON discovery_detail_results
BEGIN SELECT RAISE(ABORT,'discovery detail result is immutable'); END;
CREATE TRIGGER discovery_detail_result_scope BEFORE INSERT ON discovery_detail_results
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN discovery_detail_checkpoints h ON h.job_uuid=j.uuid
 JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
 WHERE j.uuid=NEW.job_uuid AND j.state='running' AND j.fence=NEW.fence AND r.revision=NEW.checkpoint_revision
 AND r.pending_count=0 AND r.digest=json_extract(NEW.evidence,'$.transcript_sha256'))
BEGIN SELECT RAISE(ABORT,'discovery detail result requires its current completed checkpoint'); END;
CREATE TRIGGER discovery_detail_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='post.verify_candidate' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM discovery_detail_results r WHERE r.job_uuid=NEW.uuid AND r.fence=NEW.fence
 AND json_extract(NEW.result,'$.checkpoint_revision')=r.checkpoint_revision
 AND json_extract(NEW.result,'$.checkpoint_sha256')=json_extract(r.evidence,'$.transcript_sha256'))
BEGIN SELECT RAISE(ABORT,'detail success requires its retained comparison'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000076,'Candidate detail jobs and durable original-evidence comparisons','{}');
