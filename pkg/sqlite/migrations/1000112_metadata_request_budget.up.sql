CREATE TABLE metadata_request_permits (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES archive_jobs(uuid),
 fence INTEGER NOT NULL CHECK(fence>0),
 request_uuid TEXT NOT NULL CHECK(length(request_uuid)=36),
 scope TEXT NOT NULL REFERENCES source_request_budgets(scope),
 operation TEXT NOT NULL CHECK(operation IN ('listing','profile','media-resolution','other')),
 granted_at_ms INTEGER NOT NULL,
 reported INTEGER NOT NULL DEFAULT 0 CHECK(reported IN (0,1))
);
CREATE INDEX metadata_request_permits_age ON metadata_request_permits(granted_at_ms);
DROP TRIGGER archive_job_transition;
CREATE TRIGGER archive_job_transition BEFORE UPDATE ON archive_jobs
WHEN OLD.state IN ('succeeded','failed','cancelled') OR NEW.revision!=OLD.revision+1
 OR NEW.updated_at_ms<OLD.updated_at_ms
 OR (OLD.state='queued' AND NEW.state NOT IN ('queued','running','failed','cancelled'))
 OR (NEW.state='running' AND OLD.state='queued' AND NEW.fence!=OLD.fence+1)
 OR (NOT (NEW.state='running' AND OLD.state='queued') AND NEW.fence!=OLD.fence)
 OR (OLD.state='running' AND NEW.state='running' AND NEW.owner_uuid!=OLD.owner_uuid)
 OR NEW.failures!=OLD.failures+CASE WHEN OLD.state='running' AND NEW.state IN ('queued','failed')
   AND EXISTS(SELECT 1 FROM archive_job_attempts a WHERE a.job_uuid=OLD.uuid AND a.fence=OLD.fence
     AND a.outcome IN ('retry','failed') AND NOT (OLD.kind='post.enrich' AND a.error_code='budget_yield')) THEN 1 ELSE 0 END
BEGIN SELECT RAISE(ABORT,'invalid archive job transition'); END;
INSERT INTO native_migration_history(version,name,details)
 VALUES(1000112,'Idle metadata gap requests share the source budget','{}');
