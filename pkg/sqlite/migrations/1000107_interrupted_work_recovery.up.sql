-- Keep lease generations separate from the budget for actual failed attempts.
-- Existing job states, receipts, attempt history and worker ownership are retained.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_restart_job_rows AS SELECT * FROM archive_jobs;
DROP TABLE archive_jobs;
CREATE TABLE archive_jobs (
 id INTEGER PRIMARY KEY,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind IN ('media.verify','album.backfill','text.translate','post.enrich','account.list_page','post.verify_candidate','post.merge_notify')),
 work_key TEXT NOT NULL CHECK(length(work_key)=64 AND work_key NOT GLOB '*[^0-9a-f]*'),
 resource_key TEXT NOT NULL CHECK(length(resource_key)=64 AND resource_key NOT GLOB '*[^0-9a-f]*'),
 arguments TEXT NOT NULL CHECK(json_valid(arguments) AND json_type(arguments)='object' AND length(CAST(arguments AS BLOB))<=262144),
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
 priority INTEGER NOT NULL CHECK(typeof(priority)='integer' AND priority BETWEEN 0 AND 100),
 fence INTEGER NOT NULL DEFAULT 0 CHECK(typeof(fence)='integer' AND fence>=0),
 max_attempts INTEGER NOT NULL CHECK(typeof(max_attempts)='integer' AND max_attempts BETWEEN 1 AND 100),
 failures INTEGER NOT NULL DEFAULT 0 CHECK(typeof(failures)='integer' AND failures>=0),
 available_at_ms INTEGER NOT NULL CHECK(typeof(available_at_ms)='integer' AND available_at_ms>0),
 owner_uuid TEXT,
 lease_until_ms INTEGER,
 progress TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(progress) AND json_type(progress)='object' AND length(CAST(progress AS BLOB))<=16384),
 result TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(result) AND json_type(result)='object' AND length(CAST(result AS BLOB))<=16384),
 error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=128),
 created_at_ms INTEGER NOT NULL CHECK(typeof(created_at_ms)='integer' AND created_at_ms>0),
 updated_at_ms INTEGER NOT NULL CHECK(typeof(updated_at_ms)='integer' AND updated_at_ms>=created_at_ms),
 CHECK(failures<=fence AND failures<=max_attempts AND (state NOT IN ('queued','running') OR failures<max_attempts)),
 CHECK((state='running' AND owner_uuid IS NOT NULL AND length(owner_uuid)=36
   AND typeof(lease_until_ms)='integer' AND lease_until_ms>0 AND fence>0)
   OR (state!='running' AND owner_uuid IS NULL AND lease_until_ms IS NULL))
);
INSERT INTO archive_jobs(id,uuid,kind,work_key,resource_key,arguments,state,revision,priority,fence,max_attempts,available_at_ms,owner_uuid,lease_until_ms,progress,result,error_code,created_at_ms,updated_at_ms,failures)
SELECT j.id,j.uuid,j.kind,j.work_key,j.resource_key,j.arguments,j.state,j.revision,j.priority,j.fence,j.max_attempts,j.available_at_ms,j.owner_uuid,j.lease_until_ms,j.progress,j.result,j.error_code,j.created_at_ms,j.updated_at_ms,
 (SELECT count(*) FROM archive_job_attempts a WHERE a.job_uuid=j.uuid AND a.outcome IN ('retry','failed'))
FROM native_restart_job_rows j;
DROP TABLE native_restart_job_rows;

CREATE UNIQUE INDEX archive_jobs_active_work ON archive_jobs(kind,work_key) WHERE state IN ('queued','running');

CREATE UNIQUE INDEX archive_jobs_discovery_detail ON archive_jobs(json_extract(arguments,'$.target_uuid'),json_extract(arguments,'$.candidate_sequence'))
 WHERE kind='post.verify_candidate' AND state IN ('queued','running');

CREATE UNIQUE INDEX archive_jobs_discovery_listing ON archive_jobs(json_extract(arguments,'$.listing_uuid'))
 WHERE kind='account.list_page' AND state IN ('queued','running');

CREATE UNIQUE INDEX archive_jobs_enrichment_target ON archive_jobs(json_extract(arguments,'$.target_uuid'))
 WHERE kind='post.enrich' AND state IN ('queued','running');

CREATE INDEX archive_jobs_expired ON archive_jobs(lease_until_ms,id) WHERE state='running';

CREATE INDEX archive_jobs_list ON archive_jobs(kind,state,id);

CREATE INDEX archive_jobs_kind_history ON archive_jobs(kind,id);
CREATE INDEX archive_jobs_state_history ON archive_jobs(state,id);
CREATE INDEX archive_jobs_work_history ON archive_jobs(kind,work_key,id);

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
 OR NEW.failures!=OLD.failures+CASE WHEN OLD.state='running' AND NEW.state IN ('queued','failed')
   AND EXISTS(SELECT 1 FROM archive_job_attempts a WHERE a.job_uuid=OLD.uuid AND a.fence=OLD.fence
     AND a.outcome IN ('retry','failed')) THEN 1 ELSE 0 END
BEGIN SELECT RAISE(ABORT,'invalid archive job transition'); END;

CREATE TRIGGER discovery_detail_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='post.verify_candidate' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM discovery_detail_results r WHERE r.job_uuid=NEW.uuid AND r.fence=NEW.fence
 AND json_extract(NEW.result,'$.checkpoint_revision')=r.checkpoint_revision
 AND json_extract(NEW.result,'$.checkpoint_sha256')=json_extract(r.evidence,'$.transcript_sha256'))
BEGIN SELECT RAISE(ABORT,'detail success requires its retained comparison'); END;

CREATE TRIGGER discovery_listing_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='account.list_page' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM discovery_listing_jobs b JOIN discovery_pages p ON p.listing_uuid=b.listing_uuid
 WHERE b.job_uuid=NEW.uuid AND b.page_ordinal=p.ordinal AND p.job_uuid=NEW.uuid AND p.fence=NEW.fence
 AND json_extract(NEW.result,'$.listing_uuid')=b.listing_uuid
 AND json_extract(NEW.result,'$.page_ordinal')=p.ordinal AND json_extract(NEW.result,'$.page_sha256')=p.digest)
BEGIN SELECT RAISE(ABORT,'listing job success requires its retained page'); END;

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

CREATE INDEX archive_jobs_post_merge_notify ON archive_jobs(uuid) WHERE kind='post.merge_notify';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000107,'Resume interrupted work without exhausting failure budgets','{}');
