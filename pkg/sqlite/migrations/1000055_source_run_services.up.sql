-- Remember contacted and blocked linked services per fenced source attempt.
-- A blocked request survives expiry/retry without pretending to own the service.
CREATE TABLE source_run_attempt_pacing (
 run_uuid TEXT NOT NULL,
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>0),
 scope TEXT NOT NULL REFERENCES source_pacing(scope),
 reserved INTEGER NOT NULL CHECK(reserved IN (0,1)),
 PRIMARY KEY(run_uuid,fence,scope),
 FOREIGN KEY(run_uuid,fence) REFERENCES source_run_attempts(run_uuid,fence)
);
CREATE INDEX source_run_attempt_pacing_scope ON source_run_attempt_pacing(scope,run_uuid,fence);
INSERT INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved)
 SELECT a.run_uuid,a.fence,p.scope,1 FROM source_run_attempts a JOIN source_run_pacing p ON p.run_uuid=a.run_uuid;

CREATE TRIGGER source_run_attempt_pacing_bind AFTER INSERT ON source_run_attempts
BEGIN
 INSERT INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved)
  SELECT NEW.run_uuid,NEW.fence,scope,1 FROM source_run_pacing WHERE run_uuid=NEW.run_uuid;
 INSERT OR IGNORE INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved)
  SELECT NEW.run_uuid,NEW.fence,p.scope,1 FROM source_run_attempt_pacing p
  JOIN source_run_attempts a ON a.run_uuid=p.run_uuid AND a.fence=p.fence
  WHERE a.run_uuid=NEW.run_uuid AND a.fence=NEW.fence-1 AND a.window=NEW.window
   AND a.outcome IN ('retry','expired','deferred');
END;
CREATE TRIGGER source_run_attempt_pacing_current BEFORE INSERT ON source_run_attempt_pacing
WHEN NOT EXISTS(SELECT 1 FROM source_runs r WHERE r.uuid=NEW.run_uuid AND r.fence=NEW.fence AND r.state='running')
BEGIN SELECT RAISE(ABORT,'source service requires the current attempt'); END;
CREATE TRIGGER source_run_attempt_pacing_transition BEFORE UPDATE ON source_run_attempt_pacing
WHEN NEW.run_uuid!=OLD.run_uuid OR NEW.fence!=OLD.fence OR NEW.scope!=OLD.scope OR OLD.reserved!=0 OR NEW.reserved!=1
 OR NOT EXISTS(SELECT 1 FROM source_runs r WHERE r.uuid=NEW.run_uuid AND r.fence=NEW.fence AND r.state='running')
BEGIN SELECT RAISE(ABORT,'source service reservation cannot be replaced'); END;

CREATE TABLE source_run_attempt_failures (
 run_uuid TEXT NOT NULL,
 fence INTEGER NOT NULL,
 scope TEXT NOT NULL,
 error_code TEXT NOT NULL CHECK(error_code IN ('source_busy','rate_limited','timeout','extraction_failed',
  'authentication','access_denied','challenge','not_found')),
 created_at_ms INTEGER NOT NULL CHECK(typeof(created_at_ms)='integer' AND created_at_ms>0),
 PRIMARY KEY(run_uuid,fence),
 FOREIGN KEY(run_uuid,fence,scope) REFERENCES source_run_attempt_pacing(run_uuid,fence,scope)
);
CREATE TRIGGER source_run_attempt_failure_current BEFORE INSERT ON source_run_attempt_failures
WHEN NOT EXISTS(SELECT 1 FROM source_run_attempts a
 JOIN source_run_attempt_pacing p ON p.run_uuid=a.run_uuid AND p.fence=a.fence AND p.scope=NEW.scope
 WHERE a.run_uuid=NEW.run_uuid AND a.fence=NEW.fence AND a.outcome IN ('retry','deferred')
  AND a.error_code=NEW.error_code AND a.ended_at_ms=NEW.created_at_ms
  AND (p.reserved=1 OR NEW.error_code='source_busy'))
BEGIN SELECT RAISE(ABORT,'source failure requires the matching completed attempt and service'); END;
CREATE TRIGGER source_run_attempt_failure_immutable BEFORE UPDATE ON source_run_attempt_failures
BEGIN SELECT RAISE(ABORT,'source service failures are immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000055,'Fenced download service dependencies and typed failure attribution','{}');
