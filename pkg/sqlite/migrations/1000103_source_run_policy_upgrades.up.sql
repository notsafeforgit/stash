-- Worker repairs can be explicitly approved without rewriting caller tickets,
-- covered windows, prior attempts or their original policy identity.
CREATE TABLE source_run_policy_upgrades (
  request_uuid TEXT PRIMARY KEY CHECK(length(request_uuid)=36),
  run_uuid TEXT NOT NULL REFERENCES source_runs(uuid),
  expected_revision INTEGER NOT NULL CHECK(expected_revision>0),
  effective_after_fence INTEGER NOT NULL CHECK(effective_after_fence>=0),
  expected_policy_sha256 TEXT NOT NULL CHECK(length(expected_policy_sha256)=64 AND expected_policy_sha256 NOT GLOB '*[^0-9a-f]*'),
  policy_sha256 TEXT NOT NULL CHECK(length(policy_sha256)=64 AND policy_sha256 NOT GLOB '*[^0-9a-f]*' AND policy_sha256!=expected_policy_sha256),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1024),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>0),
  UNIQUE(run_uuid,expected_revision)
);
CREATE INDEX source_run_policy_upgrade_fence ON source_run_policy_upgrades(run_uuid,effective_after_fence,expected_revision);
CREATE TRIGGER source_run_policy_upgrade_valid BEFORE INSERT ON source_run_policy_upgrades
WHEN NOT EXISTS(SELECT 1 FROM source_runs r WHERE r.uuid=NEW.run_uuid AND r.state IN ('queued','deferred')
  AND r.revision=NEW.expected_revision AND r.fence=NEW.effective_after_fence
  AND NEW.created_at_ms>=r.updated_at_ms
  AND NEW.expected_policy_sha256=coalesce((SELECT p.policy_sha256 FROM source_run_policy_upgrades p
    WHERE p.run_uuid=r.uuid ORDER BY p.expected_revision DESC LIMIT 1),r.policy_sha256))
BEGIN SELECT RAISE(ABORT,'source policy upgrade requires current inactive work'); END;
CREATE TRIGGER source_run_policy_upgrade_revision AFTER INSERT ON source_run_policy_upgrades
BEGIN
  UPDATE source_runs SET revision=revision+1,updated_at_ms=NEW.created_at_ms WHERE uuid=NEW.run_uuid;
END;
CREATE TRIGGER source_run_policy_upgrade_immutable BEFORE UPDATE ON source_run_policy_upgrades
BEGIN SELECT RAISE(ABORT,'source policy upgrades are immutable'); END;
CREATE TRIGGER source_run_policy_upgrade_retained BEFORE DELETE ON source_run_policy_upgrades
BEGIN SELECT RAISE(ABORT,'source policy upgrades are retained'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000103,'Reviewed worker policy upgrades for pending source runs','{}');
