-- A successful extraction publishes its exact retained checkpoint and native
-- capture associations together with the existing target completion receipt.
-- Schema 51 had no publication path. Refuse an unsupported success assertion
-- rather than inventing provenance for it during promotion.
CREATE TABLE native_enrichment_publication_guard (
 unsupported_successes INTEGER NOT NULL CHECK(unsupported_successes=0)
);
INSERT INTO native_enrichment_publication_guard
 SELECT count(*) FROM archive_jobs WHERE kind='post.enrich' AND state='succeeded';
DROP TABLE native_enrichment_publication_guard;

CREATE TABLE enrichment_publications (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_job_targets(job_uuid),
 checkpoint_revision INTEGER NOT NULL,
 fence INTEGER NOT NULL,
 completion_uuid TEXT NOT NULL UNIQUE REFERENCES enrichment_completions(uuid),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(job_uuid,checkpoint_revision) REFERENCES enrichment_checkpoint_receipts(job_uuid,revision),
 FOREIGN KEY(job_uuid,fence) REFERENCES enrichment_job_attempts(job_uuid,fence)
);
CREATE TRIGGER enrichment_publication_immutable BEFORE UPDATE ON enrichment_publications
BEGIN SELECT RAISE(ABORT,'enrichment publication is immutable'); END;
CREATE TRIGGER enrichment_publication_scope BEFORE INSERT ON enrichment_publications
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j
 JOIN enrichment_job_targets b ON b.job_uuid=j.uuid
 JOIN enrichment_checkpoints h ON h.job_uuid=j.uuid AND h.revision=NEW.checkpoint_revision
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision AND r.pending_count=0
 JOIN enrichment_completions e ON e.uuid=NEW.completion_uuid AND e.target_uuid=b.target_uuid AND e.expected_revision=b.target_revision
 JOIN enrichment_targets t ON t.uuid=e.target_uuid AND t.state='completed' AND t.completion_uuid=e.uuid
 WHERE j.uuid=NEW.job_uuid AND j.kind='post.enrich' AND j.state='running' AND j.fence=NEW.fence
 AND r.fence<=NEW.fence AND e.created_at=NEW.created_at AND r.created_at<=NEW.created_at)
BEGIN SELECT RAISE(ABORT,'enrichment publication requires the owned checkpoint and exact target completion'); END;

CREATE TABLE enrichment_published_records (
 job_uuid TEXT NOT NULL REFERENCES enrichment_publications(job_uuid),
 ordinal INTEGER NOT NULL,
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 PRIMARY KEY(job_uuid,ordinal),
 FOREIGN KEY(job_uuid,ordinal) REFERENCES enrichment_checkpoint_records(job_uuid,ordinal)
);
CREATE INDEX enrichment_published_records_capture ON enrichment_published_records(capture_uuid,job_uuid,ordinal);
CREATE TRIGGER enrichment_published_record_immutable BEFORE UPDATE ON enrichment_published_records
BEGIN SELECT RAISE(ABORT,'enrichment published capture association is immutable'); END;
CREATE TRIGGER enrichment_published_record_scope BEFORE INSERT ON enrichment_published_records
WHEN NOT EXISTS(SELECT 1 FROM enrichment_publications p
 JOIN archive_jobs j ON j.uuid=p.job_uuid AND j.state='running' AND j.fence=p.fence
 JOIN enrichment_completions e ON e.uuid=p.completion_uuid
 JOIN enrichment_completion_captures c ON c.completion_uuid=e.uuid AND c.capture_uuid=NEW.capture_uuid
 WHERE p.job_uuid=NEW.job_uuid)
BEGIN SELECT RAISE(ABORT,'enrichment published record must belong to its completion'); END;

CREATE TRIGGER enrichment_job_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='post.enrich' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM enrichment_publications p
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
 JOIN enrichment_completions e ON e.uuid=p.completion_uuid
 WHERE p.job_uuid=NEW.uuid AND p.fence=NEW.fence
 AND r.record_count=(SELECT count(*) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid)
 AND e.capture_count=(SELECT count(DISTINCT capture_uuid) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid))
BEGIN SELECT RAISE(ABORT,'enrichment success requires complete native capture publication'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000052,'Native enrichment publication with original observation provenance','{}');
