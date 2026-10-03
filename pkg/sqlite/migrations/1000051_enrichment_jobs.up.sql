-- Preserve identities, submission receipts, attempts and publication checkpoints.
-- Recreate under the original name so incoming foreign keys and external
-- receipt/attempt triggers retain their references.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_archive_job_rows AS SELECT * FROM archive_jobs;
DROP TABLE archive_jobs;

CREATE TABLE archive_jobs (
 id INTEGER PRIMARY KEY,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind IN ('media.verify','album.backfill','text.translate','post.enrich')),
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
INSERT INTO archive_jobs(id,uuid,kind,work_key,resource_key,arguments,state,revision,priority,fence,max_attempts,available_at_ms,owner_uuid,lease_until_ms,progress,result,error_code,created_at_ms,updated_at_ms)
SELECT id,uuid,kind,work_key,resource_key,arguments,state,revision,priority,fence,max_attempts,available_at_ms,owner_uuid,lease_until_ms,progress,result,error_code,created_at_ms,updated_at_ms FROM native_archive_job_rows;
DROP TABLE native_archive_job_rows;

-- Equivalent requests share pending work, but completed requests retain their
-- own immutable acknowledgement below. Different work sharing a destination
-- cannot be running simultaneously, even across different worker processes.
CREATE UNIQUE INDEX archive_jobs_active_work ON archive_jobs(kind,work_key) WHERE state IN ('queued','running');
CREATE UNIQUE INDEX archive_jobs_running_resource ON archive_jobs(resource_key) WHERE state='running';
CREATE INDEX archive_jobs_ready ON archive_jobs(kind,available_at_ms,priority DESC,id) WHERE state='queued';
CREATE INDEX archive_jobs_expired ON archive_jobs(lease_until_ms,id) WHERE state='running';
CREATE INDEX archive_jobs_list ON archive_jobs(kind,state,id);
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

CREATE INDEX archive_jobs_resource_history ON archive_jobs(kind,resource_key,id);

CREATE UNIQUE INDEX archive_jobs_translation_request ON archive_jobs(json_extract(arguments,'$.request_uuid'))
 WHERE kind='text.translate' AND state IN ('queued','running');

CREATE TABLE enrichment_job_targets (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES archive_jobs(uuid),
 target_uuid TEXT NOT NULL,
 target_revision INTEGER NOT NULL CHECK(typeof(target_revision)='integer' AND target_revision>0),
 UNIQUE(target_uuid,target_revision),
 FOREIGN KEY(target_uuid,target_revision) REFERENCES enrichment_target_history(target_uuid,revision)
);
CREATE TRIGGER enrichment_job_target_immutable BEFORE UPDATE ON enrichment_job_targets
BEGIN SELECT RAISE(ABORT,'enrichment job target revisions are immutable'); END;
CREATE TRIGGER enrichment_job_target_scope BEFORE INSERT ON enrichment_job_targets
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN enrichment_targets t ON t.uuid=NEW.target_uuid
 JOIN source_posts p ON p.uuid=t.post_uuid AND p.state='active'
 JOIN source_collections c ON c.uuid=t.collection_uuid AND c.revision=t.collection_revision
 JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision AND r.state='active'
 WHERE j.uuid=NEW.job_uuid AND j.kind='post.enrich' AND j.state='queued' AND j.fence=0
 AND t.revision=NEW.target_revision AND t.state='pending'
 AND json_extract(j.arguments,'$.version')=1
 AND json_extract(j.arguments,'$.target_uuid')=t.uuid AND json_extract(j.arguments,'$.target_revision')=t.revision
 AND json_extract(j.arguments,'$.post_uuid')=t.post_uuid
 AND json_extract(j.arguments,'$.collection_uuid')=t.collection_uuid
 AND json_extract(j.arguments,'$.collection_revision')=t.collection_revision
 AND json_extract(j.arguments,'$.root_uuid') IS r.root_uuid)
BEGIN SELECT RAISE(ABORT,'enrichment job target scope does not match'); END;
CREATE UNIQUE INDEX archive_jobs_enrichment_target ON archive_jobs(json_extract(arguments,'$.target_uuid'))
 WHERE kind='post.enrich' AND state IN ('queued','running');

CREATE TABLE enrichment_job_attempts (
 job_uuid TEXT NOT NULL REFERENCES enrichment_job_targets(job_uuid),
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>0),
 producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
 PRIMARY KEY(job_uuid,fence),
 FOREIGN KEY(job_uuid,fence) REFERENCES archive_job_attempts(job_uuid,fence)
);
CREATE INDEX enrichment_job_attempts_producer ON enrichment_job_attempts(producer_uuid,job_uuid,fence);
CREATE TRIGGER enrichment_job_attempt_immutable BEFORE UPDATE ON enrichment_job_attempts
BEGIN SELECT RAISE(ABORT,'enrichment attempt producers are immutable'); END;
CREATE TRIGGER enrichment_job_attempt_scope BEFORE INSERT ON enrichment_job_attempts
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN archive_job_attempts a ON a.job_uuid=j.uuid AND a.fence=j.fence
 WHERE j.uuid=NEW.job_uuid AND j.fence=NEW.fence AND j.kind='post.enrich' AND j.state='running'
 AND a.outcome='running' AND a.owner_uuid=j.owner_uuid)
BEGIN SELECT RAISE(ABORT,'enrichment producer requires the current running attempt'); END;

CREATE TABLE enrichment_checkpoint_receipts (
 job_uuid TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision BETWEEN 1 AND 128),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 fence INTEGER NOT NULL,
 record_count INTEGER NOT NULL CHECK(typeof(record_count)='integer' AND record_count BETWEEN 1 AND 1024),
 pending_count INTEGER NOT NULL CHECK(typeof(pending_count)='integer' AND pending_count BETWEEN 0 AND 256),
 unresolved_count INTEGER NOT NULL CHECK(typeof(unresolved_count)='integer' AND unresolved_count BETWEEN 0 AND 256),
 created_at DATETIME NOT NULL,
 PRIMARY KEY(job_uuid,revision),
 UNIQUE(job_uuid,digest),
 FOREIGN KEY(job_uuid,fence) REFERENCES enrichment_job_attempts(job_uuid,fence)
);
CREATE TRIGGER enrichment_checkpoint_receipt_immutable BEFORE UPDATE ON enrichment_checkpoint_receipts
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint receipts are immutable'); END;
CREATE TRIGGER enrichment_checkpoint_receipt_scope BEFORE INSERT ON enrichment_checkpoint_receipts
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.state='running' AND j.fence=NEW.fence)
 OR NEW.revision!=coalesce((SELECT revision+1 FROM enrichment_checkpoints WHERE job_uuid=NEW.job_uuid),1)
 OR NEW.record_count<coalesce((SELECT r.record_count FROM enrichment_checkpoints h
  JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=NEW.job_uuid),0)
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint requires a continuing owned attempt'); END;

CREATE TABLE enrichment_checkpoints (
 job_uuid TEXT PRIMARY KEY NOT NULL,
 revision INTEGER NOT NULL,
 body TEXT NOT NULL CHECK(json_valid(body) AND json_type(body)='object'),
 byte_size INTEGER NOT NULL CHECK(typeof(byte_size)='integer' AND byte_size=length(CAST(body AS BLOB)) AND byte_size BETWEEN 1 AND 33554432),
 FOREIGN KEY(job_uuid,revision) REFERENCES enrichment_checkpoint_receipts(job_uuid,revision)
);
CREATE TRIGGER enrichment_checkpoint_initial BEFORE INSERT ON enrichment_checkpoints
WHEN NEW.revision!=1
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint must begin at revision one'); END;
CREATE TRIGGER enrichment_checkpoint_transition BEFORE UPDATE ON enrichment_checkpoints
WHEN NEW.job_uuid!=OLD.job_uuid OR NEW.revision!=OLD.revision+1
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint revision must advance'); END;

CREATE TABLE enrichment_checkpoint_records (
 job_uuid TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(typeof(ordinal)='integer' AND ordinal BETWEEN 0 AND 1023),
 checkpoint_revision INTEGER NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 PRIMARY KEY(job_uuid,ordinal),
 FOREIGN KEY(job_uuid,checkpoint_revision) REFERENCES enrichment_checkpoint_receipts(job_uuid,revision)
);
CREATE INDEX enrichment_checkpoint_records_receipt ON enrichment_checkpoint_records(job_uuid,checkpoint_revision);
CREATE TRIGGER enrichment_checkpoint_record_immutable BEFORE UPDATE ON enrichment_checkpoint_records
BEGIN SELECT RAISE(ABORT,'enrichment observation provenance is immutable'); END;
CREATE TRIGGER enrichment_checkpoint_record_scope BEFORE INSERT ON enrichment_checkpoint_records
WHEN NOT EXISTS(SELECT 1 FROM enrichment_checkpoint_receipts r
 WHERE r.job_uuid=NEW.job_uuid AND r.revision=NEW.checkpoint_revision AND NEW.ordinal<r.record_count
 AND NEW.ordinal>=coalesce((SELECT p.record_count FROM enrichment_checkpoint_receipts p
  WHERE p.job_uuid=r.job_uuid AND p.revision=r.revision-1),0))
BEGIN SELECT RAISE(ABORT,'enrichment observation is outside its first checkpoint'); END;

-- Bounded staging includes failed/cancelled jobs: their evidence is retained
-- until publication or an explicit future discard, rather than silently lost.
CREATE TABLE enrichment_checkpoint_usage (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 byte_size INTEGER NOT NULL CHECK(typeof(byte_size)='integer' AND byte_size BETWEEN 0 AND 2147483648)
);
INSERT INTO enrichment_checkpoint_usage VALUES(1,0);
CREATE TRIGGER enrichment_checkpoint_usage_insert AFTER INSERT ON enrichment_checkpoints
BEGIN UPDATE enrichment_checkpoint_usage SET byte_size=byte_size+NEW.byte_size WHERE singleton=1; END;
CREATE TRIGGER enrichment_checkpoint_usage_update AFTER UPDATE ON enrichment_checkpoints
BEGIN UPDATE enrichment_checkpoint_usage SET byte_size=byte_size-OLD.byte_size+NEW.byte_size WHERE singleton=1; END;
CREATE TRIGGER enrichment_checkpoint_usage_delete AFTER DELETE ON enrichment_checkpoints
BEGIN UPDATE enrichment_checkpoint_usage SET byte_size=byte_size-OLD.byte_size WHERE singleton=1; END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000051,'Scoped enrichment job attempts and resumable observation checkpoints','{}');
