-- Keep only the origin of a source request, never signed paths or query values.
-- Existing root/Imgur/Redgifs bindings remain historical evidence without an
-- invented origin. Newly observed dependencies require a canonical origin.
ALTER TABLE source_run_attempt_pacing ADD COLUMN source_origin TEXT;

CREATE TRIGGER source_run_attempt_pacing_origin BEFORE INSERT ON source_run_attempt_pacing
WHEN (NEW.source_origin IS NULL
 AND NEW.scope NOT IN ('service:redgifs','service:imgur')
 AND NOT EXISTS(SELECT 1 FROM source_run_pacing WHERE run_uuid=NEW.run_uuid AND scope=NEW.scope))
 OR (NEW.source_origin IS NOT NULL
 AND (typeof(NEW.source_origin)!='text' OR source_origin_v1(NEW.source_origin)!=NEW.source_origin
  OR source_scope_v1(NEW.source_origin)!=NEW.scope))
BEGIN SELECT RAISE(ABORT,'source dependency requires its canonical HTTP origin'); END;

CREATE TRIGGER source_run_attempt_pacing_origin_immutable BEFORE UPDATE OF source_origin ON source_run_attempt_pacing
WHEN NEW.source_origin IS NOT OLD.source_origin
BEGIN SELECT RAISE(ABORT,'source dependency origin is immutable'); END;

DROP TRIGGER source_run_attempt_pacing_bind;
CREATE TRIGGER source_run_attempt_pacing_bind AFTER INSERT ON source_run_attempts
BEGIN
 INSERT INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved)
  SELECT NEW.run_uuid,NEW.fence,scope,1 FROM source_run_pacing WHERE run_uuid=NEW.run_uuid;
 INSERT OR IGNORE INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved,source_origin)
  SELECT NEW.run_uuid,NEW.fence,p.scope,1,p.source_origin FROM source_run_attempt_pacing p
  JOIN source_run_attempts a ON a.run_uuid=p.run_uuid AND a.fence=p.fence
  WHERE a.run_uuid=NEW.run_uuid AND a.fence=NEW.fence-1 AND a.window=NEW.window
   AND a.outcome IN ('retry','expired','deferred');
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000099,'Canonical origins for observed download source dependencies','{}');
