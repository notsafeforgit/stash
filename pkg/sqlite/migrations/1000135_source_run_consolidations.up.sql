CREATE TABLE source_run_consolidations (
 request_uuid TEXT PRIMARY KEY NOT NULL CHECK(length(request_uuid)=36),
 source_uuid TEXT UNIQUE NOT NULL REFERENCES source_runs(uuid),
 target_uuid TEXT NOT NULL REFERENCES source_runs(uuid),
 source_revision INTEGER NOT NULL CHECK(source_revision>0),
 target_revision INTEGER NOT NULL CHECK(target_revision>0),
 policy_sha256 TEXT NOT NULL CHECK(length(policy_sha256)=64),
 reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1024),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>0),
 CHECK(source_uuid!=target_uuid)
);
CREATE INDEX source_run_consolidation_target ON source_run_consolidations(target_uuid);
CREATE TRIGGER source_run_consolidation_valid BEFORE INSERT ON source_run_consolidations
WHEN NOT EXISTS(SELECT 1 FROM source_runs s,source_runs t
 WHERE s.uuid=NEW.source_uuid AND t.uuid=NEW.target_uuid AND s.state='cancelled'
 AND s.revision=NEW.source_revision+1 AND t.revision=NEW.target_revision+1
 AND t.state IN ('queued','deferred'))
BEGIN SELECT RAISE(ABORT,'invalid source run consolidation'); END;
CREATE TRIGGER source_run_consolidation_immutable BEFORE UPDATE ON source_run_consolidations
BEGIN SELECT RAISE(ABORT,'source run consolidation is immutable'); END;
CREATE TRIGGER source_run_consolidation_retained BEFORE DELETE ON source_run_consolidations
BEGIN SELECT RAISE(ABORT,'source run consolidation is retained'); END;
UPDATE source_runs SET available_at_ms=max(available_at_ms,
 (SELECT ended_at_ms+2592000000 FROM source_run_attempts a WHERE a.run_uuid=source_runs.uuid AND a.fence=source_runs.fence)),
 revision=revision+1,updated_at_ms=max(updated_at_ms,CAST(unixepoch('now')*1000 AS INTEGER))
 WHERE state='queued' AND error_code='not_found' AND EXISTS(
 SELECT 1 FROM source_run_attempts a WHERE a.run_uuid=source_runs.uuid AND a.fence=source_runs.fence AND a.ended_at_ms IS NOT NULL);
INSERT INTO native_migration_history(version,name,details)
VALUES(1000135,'Reviewed source run consolidation with retained request coverage','{}');
