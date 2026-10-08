-- A configured traversal has no publication-time coverage. Persist that basis
-- in the existing window envelopes and keep it separate from date backfills.
-- The native version change also fences binaries which cannot interpret it.
CREATE TRIGGER source_run_basis_insert BEFORE INSERT ON source_runs
BEGIN
 SELECT RAISE(ABORT,'invalid source run coverage basis') WHERE EXISTS (
  SELECT 1 FROM (
   SELECT value FROM json_each(NEW.pending)
   UNION ALL SELECT value FROM json_each(NEW.completed)
   UNION ALL SELECT NEW.window WHERE NEW.window IS NOT NULL
  ) WHERE COALESCE(json_extract(value,'$.basis'),'') NOT IN ('','traversal')
   OR (json_extract(value,'$.basis')='traversal'
       AND (NEW.operation!='download' OR json_extract(value,'$.since') IS NOT NULL))
 );
 SELECT RAISE(ABORT,'source run cannot mix coverage bases') WHERE (
  SELECT count(DISTINCT COALESCE(json_extract(value,'$.basis'),'')) FROM (
   SELECT value FROM json_each(NEW.pending)
   UNION ALL SELECT value FROM json_each(NEW.completed)
   UNION ALL SELECT NEW.window WHERE NEW.window IS NOT NULL
  )
 ) > 1;
END;
CREATE TRIGGER source_run_basis_update BEFORE UPDATE ON source_runs
BEGIN
 SELECT RAISE(ABORT,'invalid source run coverage basis') WHERE EXISTS (
  SELECT 1 FROM (
   SELECT value FROM json_each(NEW.pending)
   UNION ALL SELECT value FROM json_each(NEW.completed)
   UNION ALL SELECT NEW.window WHERE NEW.window IS NOT NULL
  ) WHERE COALESCE(json_extract(value,'$.basis'),'') NOT IN ('','traversal')
   OR (json_extract(value,'$.basis')='traversal'
       AND (NEW.operation!='download' OR json_extract(value,'$.since') IS NOT NULL))
 );
 SELECT RAISE(ABORT,'source run cannot mix coverage bases') WHERE (
  SELECT count(DISTINCT COALESCE(json_extract(value,'$.basis'),'')) FROM (
   SELECT value FROM json_each(NEW.pending)
   UNION ALL SELECT value FROM json_each(NEW.completed)
   UNION ALL SELECT NEW.window WHERE NEW.window IS NOT NULL
  )
 ) > 1;
END;
CREATE TRIGGER source_run_attempt_basis BEFORE INSERT ON source_run_attempts
BEGIN
 SELECT RAISE(ABORT,'invalid source attempt coverage basis')
 WHERE COALESCE(json_extract(NEW.window,'$.basis'),'') NOT IN ('','traversal')
  OR (json_extract(NEW.window,'$.basis')='traversal' AND
      (json_extract(NEW.window,'$.since') IS NOT NULL OR
       (SELECT operation FROM source_runs WHERE uuid=NEW.run_uuid)!='download'));
END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000098,'Configured source traversal distinct from publication windows','{}');
