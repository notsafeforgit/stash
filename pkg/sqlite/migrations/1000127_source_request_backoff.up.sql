-- Persist service outages with the shared HTTP budget, never in a worker cache.
ALTER TABLE source_request_budgets ADD COLUMN backoff_step INTEGER NOT NULL DEFAULT 0 CHECK(backoff_step BETWEEN 0 AND 5);
ALTER TABLE source_request_budgets ADD COLUMN backoff_at_ms INTEGER NOT NULL DEFAULT 0 CHECK(backoff_at_ms>=0);
ALTER TABLE source_request_budgets ADD COLUMN blocked_until_ms INTEGER NOT NULL DEFAULT 0 CHECK(blocked_until_ms>=0);
ALTER TABLE source_request_budgets ADD COLUMN probe_uuid TEXT NOT NULL DEFAULT '' CHECK(probe_uuid='' OR length(probe_uuid)=36);
ALTER TABLE source_request_budgets ADD COLUMN probe_until_ms INTEGER NOT NULL DEFAULT 0 CHECK(probe_until_ms>=0);
ALTER TABLE source_request_budgets ADD COLUMN probe_owner TEXT NOT NULL DEFAULT '' CHECK(length(probe_owner)<=96);
ALTER TABLE source_request_permits ADD COLUMN backoff INTEGER NOT NULL DEFAULT 0 CHECK(backoff IN (0,1));
ALTER TABLE metadata_request_permits ADD COLUMN backoff INTEGER NOT NULL DEFAULT 0 CHECK(backoff IN (0,1));
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
     AND a.outcome IN ('retry','failed') AND NOT (OLD.kind='post.enrich' AND a.error_code='budget_yield')
     AND NOT (OLD.kind IN ('post.enrich','account.list_page','post.verify_candidate') AND a.outcome='retry'
       AND EXISTS(SELECT 1 FROM metadata_request_permits p WHERE p.job_uuid=OLD.uuid AND p.fence=OLD.fence AND p.reported=1 AND p.backoff=1))) THEN 1 ELSE 0 END
BEGIN SELECT RAISE(ABORT,'invalid archive job transition'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000127,'Recover shared source throttles with bounded single-request probes','{}');
