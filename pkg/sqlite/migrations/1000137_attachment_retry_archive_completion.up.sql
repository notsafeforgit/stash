ALTER TABLE source_run_attachment_retries ADD COLUMN finalize INTEGER NOT NULL DEFAULT 0 CHECK(finalize IN (0,1));
DROP TRIGGER source_run_attachment_retry_immutable;
CREATE TRIGGER source_run_attachment_retry_immutable BEFORE UPDATE ON source_run_attachment_retries
WHEN NEW.run_uuid!=OLD.run_uuid OR NEW.producer_uuid!=OLD.producer_uuid OR NEW.event_uuid!=OLD.event_uuid OR NEW.finalize<OLD.finalize
BEGIN SELECT RAISE(ABORT,'attachment retry source is immutable'); END;
DROP TRIGGER source_run_attachment_retry_retained;
CREATE TRIGGER source_run_attachment_retry_retained BEFORE DELETE ON source_run_attachment_retries
WHEN EXISTS(SELECT 1 FROM source_runs WHERE uuid=OLD.run_uuid AND state NOT IN ('succeeded','cancelled'))
BEGIN SELECT RAISE(ABORT,'unfinished attachment retry source is retained'); END;

-- Older workers closed the retry before recording the archive decision. Finish
-- that local write using the already retained capture, without another fetch.
UPDATE source_run_attachment_retries SET finalize=1 WHERE run_uuid IN
 (SELECT uuid FROM source_runs WHERE state='cancelled' AND error_code='attachment_retry_exhausted');
DROP TRIGGER source_run_transition;
UPDATE source_runs SET state='queued',error_code='attachment_retry_archive',revision=revision+1
 WHERE state='cancelled' AND error_code='attachment_retry_exhausted'
 AND uuid IN (SELECT run_uuid FROM source_run_attachment_retries WHERE finalize=1);
CREATE TRIGGER source_run_transition BEFORE UPDATE ON source_runs
WHEN OLD.state IN ('succeeded','cancelled') OR NEW.revision!=OLD.revision+1 OR NEW.updated_at_ms<OLD.updated_at_ms
  OR (NEW.state='succeeded' AND OLD.state!='running')
  OR (NEW.state='running' AND OLD.state!='running' AND (OLD.state!='queued' OR NEW.fence!=OLD.fence+1))
  OR ((NEW.state!='running' OR OLD.state='running') AND NEW.fence!=OLD.fence)
  OR (OLD.state='running' AND NEW.state='running' AND (NEW.owner_uuid!=OLD.owner_uuid OR NEW.producer_uuid!=OLD.producer_uuid OR NEW.window!=OLD.window
    OR NEW.destination!=OLD.destination OR NEW.root_identity!=OLD.root_identity OR NEW.destination_prefix!=OLD.destination_prefix))
BEGIN SELECT RAISE(ABORT,'invalid source run transition'); END;
DELETE FROM source_run_attachment_retries WHERE run_uuid IN
 (SELECT uuid FROM source_runs WHERE state IN ('succeeded','cancelled'));
CREATE TRIGGER source_run_attachment_retry_completed AFTER UPDATE OF state ON source_runs
WHEN NEW.state IN ('succeeded','cancelled')
BEGIN DELETE FROM source_run_attachment_retries WHERE run_uuid=NEW.uuid; END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000137,'Finish exhausted post retries in downloader archives and retire retry-only state','{}');
