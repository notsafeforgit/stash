-- Preserve every existing job, receipt, attempt, index and guard.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_post_merge_job_rows AS SELECT * FROM archive_jobs;
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
INSERT INTO archive_jobs SELECT * FROM native_post_merge_job_rows;
DROP TABLE native_post_merge_job_rows;

CREATE UNIQUE INDEX archive_jobs_active_work ON archive_jobs(kind,work_key) WHERE state IN ('queued','running');

CREATE UNIQUE INDEX archive_jobs_discovery_detail ON archive_jobs(json_extract(arguments,'$.target_uuid'),json_extract(arguments,'$.candidate_sequence'))
 WHERE kind='post.verify_candidate' AND state IN ('queued','running');

CREATE UNIQUE INDEX archive_jobs_discovery_listing ON archive_jobs(json_extract(arguments,'$.listing_uuid'))
 WHERE kind='account.list_page' AND state IN ('queued','running');

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

-- Reviewed post merge receipts and their original decision scope.
CREATE INDEX archive_jobs_post_merge_notify ON archive_jobs(uuid) WHERE kind='post.merge_notify';
CREATE TABLE post_consolidation_reviews (
 request_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_post_consolidations(uuid),
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=4194304),
 result_json TEXT NOT NULL CHECK(json_valid(result_json) AND json_type(result_json)='object' AND length(CAST(result_json AS BLOB))<=4194304),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 selection_uuid TEXT UNIQUE REFERENCES post_attachment_decisions(uuid),
 gallery_decision_uuid TEXT UNIQUE REFERENCES post_gallery_decisions(uuid),
 notification_job_uuid TEXT UNIQUE REFERENCES archive_jobs(uuid)
) WITHOUT ROWID;

CREATE TABLE post_consolidation_review_members (
 request_uuid TEXT NOT NULL REFERENCES post_consolidation_reviews(request_uuid),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 previous_canonical_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 previous_revision INTEGER NOT NULL CHECK(typeof(previous_revision)='integer' AND previous_revision>0),
 PRIMARY KEY(request_uuid,post_uuid)
) WITHOUT ROWID;

CREATE TABLE post_consolidation_review_media (
 request_uuid TEXT NOT NULL REFERENCES post_consolidation_reviews(request_uuid),
 ordinal INTEGER NOT NULL CHECK(typeof(ordinal)='integer' AND ordinal>=0 AND ordinal<8192),
 decision_uuid TEXT NOT NULL UNIQUE REFERENCES post_media_decisions(uuid),
 PRIMARY KEY(request_uuid,ordinal)
) WITHOUT ROWID;

CREATE TABLE post_consolidation_review_attachments (
 request_uuid TEXT NOT NULL REFERENCES post_consolidation_reviews(request_uuid),
 ordinal INTEGER NOT NULL CHECK(typeof(ordinal)='integer' AND ordinal>=0 AND ordinal<8192),
 decision_uuid TEXT NOT NULL UNIQUE REFERENCES attachment_media_decisions(uuid),
 PRIMARY KEY(request_uuid,ordinal)
) WITHOUT ROWID;

CREATE INDEX archive_jobs_work_history ON archive_jobs(kind,work_key,id);

CREATE TRIGGER post_consolidation_review_immutable BEFORE UPDATE ON post_consolidation_reviews
BEGIN SELECT RAISE(ABORT,'post merge review receipts are immutable'); END;
CREATE TRIGGER post_consolidation_review_member_immutable BEFORE UPDATE ON post_consolidation_review_members
BEGIN SELECT RAISE(ABORT,'post merge review members are immutable'); END;
CREATE TRIGGER post_consolidation_review_media_immutable BEFORE UPDATE ON post_consolidation_review_media
BEGIN SELECT RAISE(ABORT,'post merge review media decisions are immutable'); END;
CREATE TRIGGER post_consolidation_review_attachment_immutable BEFORE UPDATE ON post_consolidation_review_attachments
BEGIN SELECT RAISE(ABORT,'post merge review attachment decisions are immutable'); END;

CREATE TRIGGER post_consolidation_review_scope BEFORE INSERT ON post_consolidation_reviews
WHEN NOT EXISTS(SELECT 1 FROM source_post_consolidations c
 WHERE c.uuid=NEW.request_uuid AND c.origin='review'
 AND json_extract(NEW.request_json,'$.request_uuid') IS c.uuid
 AND json_extract(NEW.request_json,'$.source_uuid') IS c.source_uuid
 AND json_extract(NEW.request_json,'$.destination_uuid') IS c.destination_uuid
 AND json_extract(NEW.request_json,'$.digest') IS c.review_signature
 AND json_extract(NEW.request_json,'$.reason') IS c.reason
 AND json_extract(NEW.result_json,'$.consolidation.uuid') IS c.uuid
 AND json_extract(NEW.result_json,'$.selection_uuid') IS NEW.selection_uuid
 AND json_extract(NEW.result_json,'$.gallery_decision_uuid') IS NEW.gallery_decision_uuid
 AND json_extract(NEW.result_json,'$.notification_job_uuid') IS NEW.notification_job_uuid
 AND (NEW.selection_uuid IS NULL OR EXISTS(SELECT 1 FROM post_attachment_decisions d
   JOIN post_attachment_selections h ON h.decision_uuid=d.uuid AND h.post_uuid=d.post_uuid
   WHERE d.uuid=NEW.selection_uuid AND d.post_uuid=c.destination_uuid AND d.origin='review' AND d.reason=c.reason))
 AND (NEW.gallery_decision_uuid IS NULL OR EXISTS(SELECT 1 FROM post_gallery_decisions d
   JOIN post_gallery_links h ON h.decision_uuid=d.uuid AND h.post_uuid=d.post_uuid
   WHERE d.uuid=NEW.gallery_decision_uuid AND d.post_uuid=c.destination_uuid
   AND d.gallery_uuid IS json_extract(NEW.result_json,'$.gallery.gallery_uuid')
   AND ((d.origin='review' AND d.reason=c.reason) OR
     (d.origin='source' AND json_extract(NEW.result_json,'$.gallery.created')=1 AND d.selection_uuid=NEW.selection_uuid))))
 AND (NEW.notification_job_uuid IS NULL OR EXISTS(SELECT 1 FROM archive_jobs j
   WHERE j.uuid=NEW.notification_job_uuid AND j.kind='post.merge_notify'
   AND json_extract(j.arguments,'$.review_uuid')=c.uuid AND json_extract(j.arguments,'$.version')=1
   AND json_extract(j.arguments,'$.resume_from_job_uuid') IS NULL)))
BEGIN SELECT RAISE(ABORT,'post merge review is outside its consolidation scope'); END;

CREATE TRIGGER post_consolidation_review_member_scope BEFORE INSERT ON post_consolidation_review_members
WHEN NOT EXISTS(SELECT 1 FROM source_post_consolidations c
 JOIN source_post_identities i ON i.post_uuid=NEW.post_uuid AND i.canonical_uuid=c.destination_uuid
 WHERE c.uuid=NEW.request_uuid AND NEW.previous_canonical_uuid IN (c.source_uuid,c.destination_uuid))
BEGIN SELECT RAISE(ABORT,'post merge member is outside the reviewed identity'); END;

CREATE TRIGGER post_consolidation_review_media_scope BEFORE INSERT ON post_consolidation_review_media
WHEN NOT EXISTS(SELECT 1 FROM source_post_consolidations c
 JOIN post_media_decisions d ON d.post_uuid=c.destination_uuid AND d.uuid=NEW.decision_uuid
 JOIN post_media_links h ON h.post_uuid=d.post_uuid AND h.decision_uuid=d.uuid
 WHERE c.uuid=NEW.request_uuid AND d.origin='review' AND d.reason=c.reason)
BEGIN SELECT RAISE(ABORT,'post merge media decision is outside its review'); END;

CREATE TRIGGER post_consolidation_review_attachment_scope BEFORE INSERT ON post_consolidation_review_attachments
WHEN NOT EXISTS(SELECT 1 FROM source_post_consolidations c
 JOIN post_consolidation_review_members m ON m.request_uuid=c.uuid
 JOIN source_attachments a ON a.post_uuid=m.post_uuid
 JOIN attachment_media_decisions d ON d.attachment_uuid=a.uuid AND d.uuid=NEW.decision_uuid
 JOIN attachment_media_links h ON h.attachment_uuid=d.attachment_uuid AND h.decision_uuid=d.uuid
 WHERE c.uuid=NEW.request_uuid AND d.origin='review' AND d.reason=c.reason)
BEGIN SELECT RAISE(ABORT,'post merge attachment decision is outside its review'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000092,'Atomic reviewed post merges and durable notification work','{}');
