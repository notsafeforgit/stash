-- No old body is removed by migration. The domain operation verifies the
-- checkpoint against its native publication before creating a release receipt.
CREATE TABLE enrichment_checkpoint_releases (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_publications(job_uuid),
 version INTEGER NOT NULL CHECK(version=1),
 proof_sha256 TEXT NOT NULL CHECK(length(proof_sha256)=64 AND proof_sha256 NOT GLOB '*[^0-9a-f]*'),
 checkpoint_bytes INTEGER NOT NULL CHECK(typeof(checkpoint_bytes)='integer' AND checkpoint_bytes BETWEEN 1 AND 33554432),
 unresolved TEXT NOT NULL CHECK(json_valid(unresolved) AND json_type(unresolved)='array'
  AND json_array_length(unresolved)<=256 AND length(CAST(unresolved AS BLOB))<=4194304),
 created_at DATETIME NOT NULL
);
CREATE TRIGGER enrichment_checkpoint_release_immutable BEFORE UPDATE ON enrichment_checkpoint_releases
BEGIN SELECT RAISE(ABORT,'enrichment checkpoint release is immutable'); END;
CREATE TRIGGER enrichment_checkpoint_release_scope BEFORE INSERT ON enrichment_checkpoint_releases
WHEN NOT EXISTS(SELECT 1 FROM enrichment_publications p
 JOIN archive_jobs j ON j.uuid=p.job_uuid AND j.state='succeeded' AND j.fence=p.fence
 JOIN enrichment_checkpoints h ON h.job_uuid=p.job_uuid AND h.revision=p.checkpoint_revision
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision
 WHERE p.job_uuid=NEW.job_uuid AND NEW.created_at>=p.created_at AND h.byte_size=NEW.checkpoint_bytes
 AND r.pending_count=0 AND r.unresolved_count=json_array_length(NEW.unresolved))
BEGIN SELECT RAISE(ABORT,'enrichment staging release requires its completed publication'); END;
CREATE TRIGGER enrichment_checkpoint_published_delete BEFORE DELETE ON enrichment_checkpoints
WHEN EXISTS(SELECT 1 FROM enrichment_publications p WHERE p.job_uuid=OLD.job_uuid)
 AND NOT EXISTS(SELECT 1 FROM enrichment_checkpoint_releases r WHERE r.job_uuid=OLD.job_uuid)
BEGIN SELECT RAISE(ABORT,'published enrichment checkpoint requires a release receipt'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000053,'Verified enrichment staging release with retained references and provenance','{}');
