-- V1 job identities, acknowledgements and released proofs remain unchanged.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_enrichment_release_rows AS SELECT rowid AS preserved_rowid,* FROM enrichment_checkpoint_releases;
DROP TRIGGER enrichment_checkpoint_published_delete;
DROP TABLE enrichment_checkpoint_releases;
CREATE TABLE enrichment_checkpoint_releases (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_publications(job_uuid),
 version INTEGER NOT NULL CHECK(version IN (1,2)),
 proof_sha256 TEXT NOT NULL CHECK(length(proof_sha256)=64 AND proof_sha256 NOT GLOB '*[^0-9a-f]*'),
 checkpoint_bytes INTEGER NOT NULL CHECK(typeof(checkpoint_bytes)='integer' AND checkpoint_bytes BETWEEN 1 AND 33554432),
 unresolved TEXT NOT NULL CHECK(json_valid(unresolved) AND json_type(unresolved)='array'
  AND json_array_length(unresolved)<=256 AND length(CAST(unresolved AS BLOB))<=4194304),
 created_at DATETIME NOT NULL
);
INSERT INTO enrichment_checkpoint_releases(rowid,job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at)
SELECT preserved_rowid,job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at FROM native_enrichment_release_rows;
DROP TABLE native_enrichment_release_rows;
CREATE TRIGGER enrichment_checkpoint_release_immutable BEFORE UPDATE ON enrichment_checkpoint_releases
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint release is immutable'); END;
CREATE TRIGGER enrichment_checkpoint_release_scope BEFORE INSERT ON enrichment_checkpoint_releases
WHEN NOT EXISTS(SELECT 1 FROM enrichment_publications p
 JOIN archive_jobs j ON j.uuid=p.job_uuid AND j.state='succeeded' AND j.fence=p.fence
 JOIN enrichment_checkpoints h ON h.job_uuid=p.job_uuid AND h.revision=p.checkpoint_revision
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
 WHERE p.job_uuid=NEW.job_uuid AND NEW.version=json_extract(j.arguments,'$.version') AND NEW.created_at>=p.created_at AND h.byte_size=NEW.checkpoint_bytes
 AND r.pending_count=0 AND r.unresolved_count=json_array_length(NEW.unresolved))
BEGIN SELECT RAISE(ABORT,'enrichment staging release requires its completed publication'); END;
CREATE TRIGGER enrichment_checkpoint_published_delete BEFORE DELETE ON enrichment_checkpoints
WHEN EXISTS(SELECT 1 FROM enrichment_publications p WHERE p.job_uuid=OLD.job_uuid)
 AND NOT EXISTS(SELECT 1 FROM enrichment_checkpoint_releases r WHERE r.job_uuid=OLD.job_uuid)
BEGIN SELECT RAISE(ABORT,'published enrichment checkpoint requires a release receipt'); END;

DROP TRIGGER enrichment_job_target_scope;
CREATE TRIGGER enrichment_job_target_scope BEFORE INSERT ON enrichment_job_targets
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN enrichment_targets t ON t.uuid=NEW.target_uuid
 JOIN source_posts p ON p.uuid=t.post_uuid AND p.state='active'
 JOIN source_collections c ON c.uuid=t.collection_uuid AND c.revision=t.collection_revision
 JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision AND r.state='active'
 WHERE j.uuid=NEW.job_uuid AND j.kind='post.enrich' AND j.state='queued' AND j.fence=0
 AND t.revision=NEW.target_revision AND t.state='pending'
 AND json_extract(j.arguments,'$.version') IN (1,2)
 AND ((json_extract(j.arguments,'$.version')=1 AND json_type(j.arguments,'$.capture_policy') IS NULL AND json_type(j.arguments,'$.handoff') IS NULL)
 OR (json_extract(j.arguments,'$.version')=2 AND json_extract(j.arguments,'$.capture_policy')='source-retention-v1+capture-context-v1'))
 AND json_extract(j.arguments,'$.target_uuid')=t.uuid AND json_extract(j.arguments,'$.target_revision')=t.revision
 AND json_extract(j.arguments,'$.post_uuid')=t.post_uuid
 AND json_extract(j.arguments,'$.collection_uuid')=t.collection_uuid
 AND json_extract(j.arguments,'$.collection_revision')=t.collection_revision
 AND json_extract(j.arguments,'$.root_uuid') IS r.root_uuid)
BEGIN SELECT RAISE(ABORT,'enrichment job target scope does not match'); END;

CREATE TABLE enrichment_handoff_jobs (
 handoff_uuid TEXT PRIMARY KEY NOT NULL REFERENCES checkpoint_handoffs(uuid),
 job_uuid TEXT NOT NULL UNIQUE REFERENCES enrichment_job_targets(job_uuid),
 consumed_revision INTEGER NOT NULL CHECK(consumed_revision>1),
 created_at DATETIME NOT NULL
);
CREATE TRIGGER enrichment_handoff_job_immutable BEFORE UPDATE ON enrichment_handoff_jobs
BEGIN SELECT RAISE(ABORT,'checkpoint handoff consumption is immutable'); END;
CREATE TRIGGER enrichment_handoff_job_scope BEFORE INSERT ON enrichment_handoff_jobs
WHEN NOT EXISTS(SELECT 1 FROM checkpoint_handoffs a JOIN archive_jobs j ON j.uuid=NEW.job_uuid
 JOIN enrichment_job_targets b ON b.job_uuid=j.uuid
 JOIN enrichment_target_history h ON h.target_uuid=a.target_uuid AND h.revision=NEW.consumed_revision
 JOIN enrichment_target_history r ON r.target_uuid=b.target_uuid AND r.revision=b.target_revision
 WHERE a.uuid=NEW.handoff_uuid AND NEW.consumed_revision=a.target_revision+1
 AND j.kind='post.enrich' AND j.state='queued' AND j.fence=0 AND json_extract(j.arguments,'$.version')=2
 AND json_extract(j.arguments,'$.handoff.uuid')=a.uuid AND json_extract(j.arguments,'$.handoff.plan_sha256')=a.plan_sha256
 AND json_extract(j.arguments,'$.handoff.seed_sha256')=a.seed_sha256
 AND json_extract(j.arguments,'$.capture_policy')=json_extract(a.plan,'$.capture_policy')
 AND b.target_uuid=json_extract(a.plan,'$.released_target_uuid') AND b.target_revision=json_extract(a.plan,'$.released_revision')
 AND h.recorded_at=NEW.created_at AND r.recorded_at=NEW.created_at AND r.state='pending' AND r.reason=''
 AND ((b.target_uuid=a.target_uuid AND h.state='pending' AND h.reason='')
 OR (b.target_uuid!=a.target_uuid AND h.state='excluded' AND h.reason='checkpoint_handoff')))
BEGIN SELECT RAISE(ABORT,'checkpoint handoff requires its atomic reviewed release and job'); END;

-- These bounded projections contain identities/digests and service names only.
-- They are not observations or checkpoint receipts and copy no source payloads.
CREATE TABLE enrichment_job_retained_records (
 job_uuid TEXT NOT NULL REFERENCES enrichment_handoff_jobs(job_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 1023),
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 PRIMARY KEY(job_uuid,ordinal),
 UNIQUE(job_uuid,capture_uuid)
);
CREATE TRIGGER enrichment_job_retained_immutable BEFORE UPDATE ON enrichment_job_retained_records
BEGIN SELECT RAISE(ABORT,'retained observation bindings are immutable'); END;
CREATE TRIGGER enrichment_job_retained_scope BEFORE INSERT ON enrichment_job_retained_records
WHEN NOT EXISTS(SELECT 1 FROM enrichment_handoff_jobs x JOIN checkpoint_handoffs h ON h.uuid=x.handoff_uuid
 JOIN checkpoint_evidence_captures c ON c.acceptance_uuid=h.evidence_uuid AND c.capture_uuid=NEW.capture_uuid
 JOIN archive_jobs j ON j.uuid=x.job_uuid AND j.state='queued' AND j.fence=0
 WHERE x.job_uuid=NEW.job_uuid)
BEGIN SELECT RAISE(ABORT,'retained records require accepted evidence before execution'); END;
CREATE TABLE enrichment_job_seed_services (
 job_uuid TEXT NOT NULL REFERENCES enrichment_handoff_jobs(job_uuid),
 scope TEXT NOT NULL CHECK(length(scope) BETWEEN 1 AND 1024),
 PRIMARY KEY(job_uuid,scope)
);
CREATE TRIGGER enrichment_job_seed_service_immutable BEFORE UPDATE ON enrichment_job_seed_services
BEGIN SELECT RAISE(ABORT,'seed service bindings are immutable'); END;
CREATE TRIGGER enrichment_job_seed_service_scope BEFORE INSERT ON enrichment_job_seed_services
WHEN NOT EXISTS(SELECT 1 FROM enrichment_handoff_jobs x JOIN archive_jobs j ON j.uuid=x.job_uuid
 WHERE x.job_uuid=NEW.job_uuid AND j.state='queued' AND j.fence=0)
BEGIN SELECT RAISE(ABORT,'seed services must be bound before execution'); END;

DROP TRIGGER enrichment_completion_capture_scope;
CREATE TRIGGER enrichment_completion_capture_scope BEFORE INSERT ON enrichment_completion_captures
WHEN NOT EXISTS(SELECT 1 FROM enrichment_completions e JOIN enrichment_targets t ON t.uuid=e.target_uuid
 JOIN source_captures c ON c.uuid=NEW.capture_uuid AND c.post_uuid=t.post_uuid
 JOIN source_collection_captures b ON b.capture_uuid=c.uuid AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision
 WHERE e.uuid=NEW.completion_uuid AND (c.origin IN ('gallery-dl','gallery-dl-enrichment') OR (c.origin='legacy-enrichment'
 AND EXISTS(SELECT 1 FROM enrichment_job_retained_records x JOIN enrichment_job_targets j ON j.job_uuid=x.job_uuid
 JOIN archive_jobs a ON a.uuid=j.job_uuid AND a.state='running'
 WHERE x.capture_uuid=c.uuid AND j.target_uuid=t.uuid AND j.target_revision=e.expected_revision)))
 AND e.legacy_receipt_uuid IS NULL AND e.legacy_capture_uuid IS NULL AND t.state='pending' AND t.revision=e.expected_revision
 AND e.capture_count>(SELECT count(*) FROM enrichment_completion_captures WHERE completion_uuid=e.uuid))
BEGIN SELECT RAISE(ABORT,'enrichment completion capture is outside its post or collection scope'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000065,'Execute reviewed retained checkpoints with original observation provenance','{}');
