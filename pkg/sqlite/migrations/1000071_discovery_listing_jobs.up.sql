-- Preserve existing job rows, receipts, attempts, indexes and guards.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_listing_archive_job_rows AS SELECT * FROM archive_jobs;
DROP TABLE archive_jobs;

CREATE TABLE archive_jobs (
 id INTEGER PRIMARY KEY,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind IN ('media.verify','album.backfill','text.translate','post.enrich','account.list_page')),
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

INSERT INTO archive_jobs SELECT * FROM native_listing_archive_job_rows;
DROP TABLE native_listing_archive_job_rows;

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
WHEN NEW.kind IN ('post.enrich','account.list_page') AND NEW.revision!=OLD.revision
BEGIN DELETE FROM source_enrichment_waiters WHERE job_uuid=NEW.uuid; END;

-- Generalize the shared metadata reservation, preserving its original rows.
CREATE TABLE native_listing_pacing_rows AS SELECT * FROM enrichment_job_pacing;
DROP TABLE enrichment_job_pacing;
CREATE TABLE enrichment_job_pacing (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES archive_jobs(uuid),
 scope TEXT NOT NULL REFERENCES source_pacing(scope)
);
INSERT INTO enrichment_job_pacing SELECT * FROM native_listing_pacing_rows;
DROP TABLE native_listing_pacing_rows;
CREATE INDEX enrichment_job_pacing_scope ON enrichment_job_pacing(scope,job_uuid);
CREATE TRIGGER enrichment_job_pacing_immutable BEFORE UPDATE ON enrichment_job_pacing
BEGIN SELECT RAISE(ABORT,'source pacing binding is immutable'); END;
DROP TRIGGER enrichment_attempt_pacing_current;
CREATE TRIGGER enrichment_attempt_pacing_current BEFORE INSERT ON enrichment_attempt_pacing
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.kind IN ('post.enrich','account.list_page') AND j.state='running' AND j.fence=NEW.fence)
BEGIN SELECT RAISE(ABORT,'source reservation requires the current metadata attempt'); END;
DROP TRIGGER source_enrichment_waiter_current;
CREATE TRIGGER source_enrichment_waiter_current BEFORE INSERT ON source_enrichment_waiters
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.kind IN ('post.enrich','account.list_page') AND j.state='queued' AND j.fence=NEW.fence)
BEGIN SELECT RAISE(ABORT,'source waiter requires the current queued metadata job'); END;

CREATE TABLE discovery_listings (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
 root_uuid TEXT REFERENCES media_roots(uuid),
 definition TEXT NOT NULL CHECK(json_valid(definition) AND json_type(definition)='object' AND length(CAST(definition AS BLOB))<=32768),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK(json_extract(definition,'$.uuid') IS uuid AND json_extract(definition,'$.account_uuid') IS account_uuid
  AND json_extract(definition,'$.collection_uuid') IS collection_uuid AND json_extract(definition,'$.collection_revision') IS collection_revision
  AND json_extract(definition,'$.root_uuid') IS root_uuid)
);
CREATE INDEX discovery_listings_collection ON discovery_listings(collection_uuid,uuid);
CREATE INDEX discovery_listings_account ON discovery_listings(account_uuid,uuid);
CREATE TRIGGER discovery_listing_immutable BEFORE UPDATE ON discovery_listings
BEGIN SELECT RAISE(ABORT,'discovery definition is immutable'); END;
CREATE TABLE discovery_listing_legacy (
 listing_uuid TEXT PRIMARY KEY NOT NULL REFERENCES discovery_listings(uuid),
 snapshot_uuid TEXT NOT NULL,
 account_ordinal INTEGER NOT NULL,
 FOREIGN KEY(snapshot_uuid,account_ordinal) REFERENCES automation_discovery_records(snapshot_uuid,ordinal)
);
CREATE TRIGGER discovery_listing_legacy_immutable BEFORE UPDATE ON discovery_listing_legacy
BEGIN SELECT RAISE(ABORT,'discovery resume evidence is immutable'); END;
CREATE TABLE discovery_listing_jobs (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES archive_jobs(uuid),
 listing_uuid TEXT NOT NULL REFERENCES discovery_listings(uuid),
 generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0),
 page_ordinal INTEGER NOT NULL CHECK(typeof(page_ordinal)='integer' AND page_ordinal BETWEEN 1 AND 10000),
 UNIQUE(listing_uuid,generation)
);
CREATE TRIGGER discovery_listing_job_immutable BEFORE UPDATE ON discovery_listing_jobs
BEGIN SELECT RAISE(ABORT,'discovery job binding is immutable'); END;
CREATE TRIGGER discovery_listing_job_scope BEFORE INSERT ON discovery_listing_jobs
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN discovery_listings d ON d.uuid=NEW.listing_uuid
 WHERE j.uuid=NEW.job_uuid AND j.kind='account.list_page' AND j.state='queued' AND j.fence=0
 AND json_extract(j.arguments,'$.version')=1 AND json_extract(j.arguments,'$.listing_uuid')=d.uuid
 AND json_extract(j.arguments,'$.generation')=NEW.generation
 AND json_extract(j.arguments,'$.page_ordinal')=NEW.page_ordinal
 AND json_extract(j.arguments,'$.definition_sha256')=d.digest AND json_extract(j.arguments,'$.collection_uuid')=d.collection_uuid)
BEGIN SELECT RAISE(ABORT,'discovery job does not match its listing'); END;
CREATE UNIQUE INDEX archive_jobs_discovery_listing ON archive_jobs(json_extract(arguments,'$.listing_uuid'))
 WHERE kind='account.list_page' AND state IN ('queued','running');
CREATE TRIGGER discovery_listing_pacing_bind AFTER INSERT ON discovery_listing_jobs
BEGIN
 INSERT OR IGNORE INTO source_pacing(scope)
 SELECT source_scope_v1(json_extract(definition,'$.profile_url')) FROM discovery_listings WHERE uuid=NEW.listing_uuid;
 INSERT INTO enrichment_job_pacing(job_uuid,scope)
 SELECT NEW.job_uuid,source_scope_v1(json_extract(definition,'$.profile_url')) FROM discovery_listings WHERE uuid=NEW.listing_uuid;
END;
CREATE TABLE discovery_job_attempts (
 job_uuid TEXT NOT NULL REFERENCES discovery_listing_jobs(job_uuid),
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>0),
 producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
 PRIMARY KEY(job_uuid,fence),
 FOREIGN KEY(job_uuid,fence) REFERENCES archive_job_attempts(job_uuid,fence)
);
CREATE TRIGGER discovery_job_attempt_immutable BEFORE UPDATE ON discovery_job_attempts
BEGIN SELECT RAISE(ABORT,'discovery attempt producer is immutable'); END;
CREATE TABLE discovery_pages (
 listing_uuid TEXT NOT NULL REFERENCES discovery_listings(uuid),
 ordinal INTEGER NOT NULL CHECK(typeof(ordinal)='integer' AND ordinal BETWEEN 1 AND 10000),
 job_uuid TEXT NOT NULL,
 fence INTEGER NOT NULL,
 producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 byte_count INTEGER NOT NULL CHECK(byte_count=length(CAST(body AS BLOB)) AND byte_count BETWEEN 1 AND 33554432),
 record_count INTEGER NOT NULL CHECK(typeof(record_count)='integer' AND record_count BETWEEN 0 AND 4096),
 complete INTEGER NOT NULL CHECK(complete IN (0,1)),
 body TEXT NOT NULL CHECK(json_valid(body) AND json_type(body)='object'),
 created_at DATETIME NOT NULL,
 PRIMARY KEY(listing_uuid,ordinal),
 FOREIGN KEY(job_uuid,fence) REFERENCES discovery_job_attempts(job_uuid,fence),
 CHECK(json_type(body,'$.complete')=CASE WHEN complete=1 THEN 'true' ELSE 'false' END
  AND json_array_length(body,'$.records')=record_count)
);
CREATE INDEX discovery_pages_bytes ON discovery_pages(byte_count);
CREATE INDEX discovery_pages_cursor ON discovery_pages(listing_uuid,json_extract(body,'$.cursor'));
CREATE TRIGGER discovery_page_immutable BEFORE UPDATE ON discovery_pages
BEGIN SELECT RAISE(ABORT,'discovery pages are immutable'); END;
CREATE TRIGGER discovery_page_sequence BEFORE INSERT ON discovery_pages
WHEN NEW.ordinal!=coalesce((SELECT max(ordinal)+1 FROM discovery_pages WHERE listing_uuid=NEW.listing_uuid),1)
 OR EXISTS(SELECT 1 FROM discovery_pages WHERE listing_uuid=NEW.listing_uuid AND complete=1)
 OR NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN discovery_listing_jobs b ON b.job_uuid=j.uuid
 JOIN discovery_job_attempts a ON a.job_uuid=j.uuid AND a.fence=j.fence
 WHERE j.uuid=NEW.job_uuid AND b.listing_uuid=NEW.listing_uuid AND b.page_ordinal=NEW.ordinal AND j.state='running' AND j.fence=NEW.fence AND a.producer_uuid=NEW.producer_uuid)
BEGIN SELECT RAISE(ABORT,'discovery page does not continue its owned listing'); END;
CREATE TRIGGER discovery_listing_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='account.list_page' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM discovery_listing_jobs b JOIN discovery_pages p ON p.listing_uuid=b.listing_uuid
 WHERE b.job_uuid=NEW.uuid AND b.page_ordinal=p.ordinal AND p.job_uuid=NEW.uuid AND p.fence=NEW.fence
 AND json_extract(NEW.result,'$.listing_uuid')=b.listing_uuid
 AND json_extract(NEW.result,'$.page_ordinal')=p.ordinal AND json_extract(NEW.result,'$.page_sha256')=p.digest)
BEGIN SELECT RAISE(ABORT,'listing job success requires its retained page'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000071,'Durable account listing pages and shared metadata source pacing','{}');
