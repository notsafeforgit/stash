-- Fairness starts a new scheduling epoch without rewriting historical work.
CREATE TABLE source_service_turns (
 scope TEXT PRIMARY KEY NOT NULL REFERENCES source_pacing(scope),
 download_starts INTEGER NOT NULL DEFAULT 0 CHECK(typeof(download_starts)='integer' AND download_starts BETWEEN 0 AND 4),
 enrichment_started_at_ms INTEGER NOT NULL DEFAULT 0 CHECK(typeof(enrichment_started_at_ms)='integer' AND enrichment_started_at_ms>=0)
);
INSERT INTO source_service_turns(scope) SELECT scope FROM source_pacing;
CREATE TRIGGER source_service_turns_bind AFTER INSERT ON source_pacing
BEGIN INSERT INTO source_service_turns(scope) VALUES(NEW.scope); END;

CREATE TABLE source_enrichment_waiters (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_job_pacing(job_uuid),
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>=0),
 first_requested_at_ms INTEGER NOT NULL CHECK(typeof(first_requested_at_ms)='integer' AND first_requested_at_ms>0),
 refreshed_at_ms INTEGER NOT NULL CHECK(typeof(refreshed_at_ms)='integer' AND refreshed_at_ms>=first_requested_at_ms),
 expires_at_ms INTEGER NOT NULL CHECK(typeof(expires_at_ms)='integer' AND expires_at_ms=refreshed_at_ms+90000)
);
CREATE INDEX source_enrichment_waiters_expiry ON source_enrichment_waiters(expires_at_ms);
CREATE TABLE source_enrichment_waiter_scopes (
 job_uuid TEXT NOT NULL REFERENCES source_enrichment_waiters(job_uuid) ON DELETE CASCADE,
 scope TEXT NOT NULL REFERENCES source_pacing(scope),
 PRIMARY KEY(job_uuid,scope)
);
CREATE INDEX source_enrichment_waiter_scopes_scope ON source_enrichment_waiter_scopes(scope,job_uuid);
CREATE TRIGGER source_enrichment_waiter_current BEFORE INSERT ON source_enrichment_waiters
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.kind='post.enrich' AND j.state='queued' AND j.fence=NEW.fence)
BEGIN SELECT RAISE(ABORT,'source waiter requires the current queued job'); END;
CREATE TRIGGER source_enrichment_waiter_identity BEFORE UPDATE ON source_enrichment_waiters
WHEN NEW.job_uuid!=OLD.job_uuid OR NEW.fence!=OLD.fence OR NEW.refreshed_at_ms<OLD.refreshed_at_ms
BEGIN SELECT RAISE(ABORT,'source waiter identity cannot change'); END;
CREATE TRIGGER source_enrichment_waiter_end AFTER UPDATE ON archive_jobs
WHEN NEW.kind='post.enrich' AND NEW.revision!=OLD.revision
BEGIN DELETE FROM source_enrichment_waiters WHERE job_uuid=NEW.uuid; END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000056,'Bounded download preference and live enrichment interest','{}');
