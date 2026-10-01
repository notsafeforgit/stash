-- Application-reviewed recovery bindings are separate from immutable evidence.
-- No historical PID, lease, producer token, attempt or completion is invented.
CREATE TABLE scan_journal_activations (
  uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)),
  journal_uuid TEXT NOT NULL REFERENCES scan_journals(uuid),
  run_uuid TEXT NOT NULL UNIQUE REFERENCES source_runs(uuid),
  plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(plan)<=1048576),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>0)
);
CREATE TRIGGER scan_journal_activation_valid BEFORE INSERT ON scan_journal_activations
WHEN NEW.uuid IS NOT json_extract(NEW.plan,'$.binding.uuid')
  OR NEW.journal_uuid IS NOT json_extract(NEW.plan,'$.journal_uuid')
  OR NOT EXISTS(SELECT 1 FROM source_runs r JOIN scan_journals j ON j.uuid=NEW.journal_uuid
    WHERE r.uuid=NEW.run_uuid AND r.fence=0 AND r.state IN ('queued','deferred')
      AND r.root_uuid=j.root_uuid AND r.root_uuid=json_extract(NEW.plan,'$.root_uuid')
      AND r.collection_uuid=json_extract(NEW.plan,'$.binding.collection_uuid')
      AND r.policy_sha256=json_extract(NEW.plan,'$.binding.policy_sha256'))
BEGIN SELECT RAISE(ABORT,'scan activation requires a fresh bound source run'); END;
CREATE TRIGGER scan_journal_activation_immutable BEFORE UPDATE ON scan_journal_activations
BEGIN SELECT RAISE(ABORT,'scan activation is immutable'); END;
CREATE TABLE scan_journal_activation_jobs (
  source_uuid TEXT NOT NULL,
  source_key TEXT NOT NULL,
  record_uuid TEXT NOT NULL UNIQUE REFERENCES scan_journal_records(uuid),
  activation_uuid TEXT NOT NULL REFERENCES scan_journal_activations(uuid),
  PRIMARY KEY(source_uuid,source_key)
);
CREATE INDEX scan_journal_activation_job_page ON scan_journal_activation_jobs(activation_uuid,record_uuid);
CREATE TRIGGER scan_journal_activation_job_valid BEFORE INSERT ON scan_journal_activation_jobs
WHEN NOT EXISTS(SELECT 1 FROM scan_journal_records r JOIN scan_journals j ON j.uuid=r.journal_uuid
  JOIN scan_journal_activations a ON a.journal_uuid=j.uuid
  WHERE r.uuid=NEW.record_uuid AND r.source_key=NEW.source_key AND j.source_uuid=NEW.source_uuid
    AND r.source_table='scan_jobs' AND a.uuid=NEW.activation_uuid
    AND NEW.record_uuid IN (SELECT value FROM json_each(a.plan,'$.job_uuids')))
BEGIN SELECT RAISE(ABORT,'scan activation job does not belong to its reviewed plan'); END;
CREATE TRIGGER scan_journal_activation_job_immutable BEFORE UPDATE ON scan_journal_activation_jobs
BEGIN SELECT RAISE(ABORT,'scan activation job is immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000027,'Reviewed activation and resume of retained scan requests','{}');
