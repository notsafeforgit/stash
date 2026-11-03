-- Retain provider account distinctions without changing existing attempt history.
CREATE TABLE source_run_attempt_failures_v138 (
 run_uuid TEXT NOT NULL,
 fence INTEGER NOT NULL,
 scope TEXT NOT NULL,
 error_code TEXT NOT NULL CHECK(error_code IN ('source_busy','rate_limited','timeout','extraction_failed',
  'authentication','access_denied','challenge','not_found','source_history_limit','account_deleted',
  'account_suspended','account_deactivated','account_unavailable','account_not_found')),
 created_at_ms INTEGER NOT NULL CHECK(typeof(created_at_ms)='integer' AND created_at_ms>0),
 PRIMARY KEY(run_uuid,fence),
 FOREIGN KEY(run_uuid,fence,scope) REFERENCES source_run_attempt_pacing(run_uuid,fence,scope)
);
INSERT INTO source_run_attempt_failures_v138 SELECT * FROM source_run_attempt_failures;
DROP TABLE source_run_attempt_failures;
ALTER TABLE source_run_attempt_failures_v138 RENAME TO source_run_attempt_failures;
CREATE TRIGGER source_run_attempt_failure_current BEFORE INSERT ON source_run_attempt_failures
WHEN NOT EXISTS(SELECT 1 FROM source_run_attempts a
 JOIN source_run_attempt_pacing p ON p.run_uuid=a.run_uuid AND p.fence=a.fence AND p.scope=NEW.scope
 WHERE a.run_uuid=NEW.run_uuid AND a.fence=NEW.fence AND a.outcome IN ('retry','deferred')
  AND a.error_code=NEW.error_code AND a.ended_at_ms=NEW.created_at_ms
  AND (p.reserved=1 OR NEW.error_code='source_busy'))
BEGIN SELECT RAISE(ABORT,'source failure requires the matching completed attempt and service'); END;
CREATE TRIGGER source_run_attempt_failure_immutable BEFORE UPDATE ON source_run_attempt_failures
BEGIN SELECT RAISE(ABORT,'source service failures are immutable'); END;
CREATE TRIGGER source_run_attempt_account_failure BEFORE INSERT ON source_run_attempt_failures
WHEN NEW.error_code='account_deleted' AND (NEW.scope!='service:redgifs'
 OR NOT EXISTS(SELECT 1 FROM source_run_attempts a
  WHERE a.run_uuid=NEW.run_uuid AND a.fence=NEW.fence AND a.outcome='deferred'))
BEGIN SELECT RAISE(ABORT,'permanent account deletion requires an explicit Redgifs failure'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000138,'Distinct account availability and explicit Redgifs account retirement','{}');
