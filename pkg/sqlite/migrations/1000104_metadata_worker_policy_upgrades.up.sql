-- Authorize an exact compatible profile repair without rewriting admissions.
CREATE TABLE metadata_worker_policy_upgrades (
  id INTEGER PRIMARY KEY,
  request_uuid TEXT NOT NULL UNIQUE CHECK(length(request_uuid)=36),
  kind TEXT NOT NULL CHECK(kind IN ('post.enrich','account.list_page','post.verify_candidate')),
  original_policy_sha256 TEXT NOT NULL CHECK(length(original_policy_sha256)=64 AND original_policy_sha256 NOT GLOB '*[^0-9a-f]*'),
  expected_policy_sha256 TEXT NOT NULL CHECK(length(expected_policy_sha256)=64 AND expected_policy_sha256 NOT GLOB '*[^0-9a-f]*'),
  policy_sha256 TEXT NOT NULL CHECK(length(policy_sha256)=64 AND policy_sha256 NOT GLOB '*[^0-9a-f]*' AND policy_sha256!=expected_policy_sha256),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1024),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>0)
);
CREATE INDEX metadata_worker_policy_head ON metadata_worker_policy_upgrades(kind,original_policy_sha256,id DESC);
CREATE TRIGGER metadata_worker_policy_valid BEFORE INSERT ON metadata_worker_policy_upgrades
WHEN NEW.expected_policy_sha256!=coalesce((SELECT policy_sha256 FROM metadata_worker_policy_upgrades
  WHERE kind=NEW.kind AND original_policy_sha256=NEW.original_policy_sha256 ORDER BY id DESC LIMIT 1),NEW.original_policy_sha256)
 OR NEW.created_at_ms<coalesce((SELECT created_at_ms FROM metadata_worker_policy_upgrades
  WHERE kind=NEW.kind AND original_policy_sha256=NEW.original_policy_sha256 ORDER BY id DESC LIMIT 1),0)
BEGIN SELECT RAISE(ABORT,'metadata worker policy changed'); END;
CREATE TRIGGER metadata_worker_policy_immutable BEFORE UPDATE ON metadata_worker_policy_upgrades
BEGIN SELECT RAISE(ABORT,'metadata worker policy approvals are immutable'); END;
CREATE TRIGGER metadata_worker_policy_retained BEFORE DELETE ON metadata_worker_policy_upgrades
BEGIN SELECT RAISE(ABORT,'metadata worker policy approvals are retained'); END;

CREATE TABLE metadata_worker_attempt_policies (
  job_uuid TEXT NOT NULL,
  fence INTEGER NOT NULL CHECK(fence>0),
  original_policy_sha256 TEXT NOT NULL CHECK(length(original_policy_sha256)=64 AND original_policy_sha256 NOT GLOB '*[^0-9a-f]*'),
  policy_sha256 TEXT NOT NULL CHECK(length(policy_sha256)=64 AND policy_sha256 NOT GLOB '*[^0-9a-f]*'),
  approval_uuid TEXT REFERENCES metadata_worker_policy_upgrades(request_uuid),
  PRIMARY KEY(job_uuid,fence),
  FOREIGN KEY(job_uuid,fence) REFERENCES archive_job_attempts(job_uuid,fence)
);
CREATE TRIGGER metadata_worker_attempt_policy_valid BEFORE INSERT ON metadata_worker_attempt_policies
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN archive_job_attempts a ON a.job_uuid=j.uuid AND a.fence=j.fence
  WHERE j.uuid=NEW.job_uuid AND j.fence=NEW.fence AND j.state='running' AND a.outcome='running'
  AND j.kind IN ('post.enrich','account.list_page','post.verify_candidate')
  AND NEW.original_policy_sha256=CASE WHEN j.kind='account.list_page'
    THEN (SELECT json_extract(d.definition,'$.policy_sha256') FROM discovery_listings d WHERE d.uuid=json_extract(j.arguments,'$.listing_uuid'))
    ELSE json_extract(j.arguments,'$.policy_sha256') END
  AND NEW.approval_uuid IS (SELECT p.request_uuid FROM metadata_worker_policy_upgrades p
    WHERE p.kind=j.kind AND p.original_policy_sha256=NEW.original_policy_sha256 ORDER BY p.id DESC LIMIT 1)
  AND ((NEW.approval_uuid IS NULL AND NEW.policy_sha256=NEW.original_policy_sha256)
    OR EXISTS(SELECT 1 FROM metadata_worker_policy_upgrades p WHERE p.request_uuid=NEW.approval_uuid
       AND p.kind=j.kind AND p.original_policy_sha256=NEW.original_policy_sha256 AND p.policy_sha256=NEW.policy_sha256
       AND p.created_at_ms<=a.started_at_ms)))
BEGIN SELECT RAISE(ABORT,'invalid metadata worker attempt policy'); END;
CREATE TRIGGER metadata_worker_attempt_policy_immutable BEFORE UPDATE ON metadata_worker_attempt_policies
BEGIN SELECT RAISE(ABORT,'metadata worker attempts are immutable'); END;
CREATE TRIGGER metadata_worker_attempt_policy_retained BEFORE DELETE ON metadata_worker_attempt_policies
BEGIN SELECT RAISE(ABORT,'metadata worker attempts are retained'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000104,'Reviewed metadata worker repairs with retained attempt policies','{}');
