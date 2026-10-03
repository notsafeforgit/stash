-- Bind immutable work to the contacted service. Website credentials and
-- performer/account identity are not scheduling identities.
CREATE TABLE source_pacing (
  scope TEXT PRIMARY KEY NOT NULL CHECK(length(scope) BETWEEN 1 AND 300),
  available_at_ms INTEGER NOT NULL DEFAULT 0 CHECK(available_at_ms>=0),
  last_started_at_ms INTEGER NOT NULL DEFAULT 0 CHECK(last_started_at_ms>=0),
  reason TEXT NOT NULL DEFAULT '' CHECK(length(reason)<=128)
);
CREATE TABLE source_run_pacing (
  run_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_runs(uuid),
  scope TEXT NOT NULL REFERENCES source_pacing(scope)
);
CREATE INDEX source_run_pacing_scope ON source_run_pacing(scope,run_uuid);
CREATE TABLE enrichment_job_pacing (
  job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_job_targets(job_uuid),
  scope TEXT NOT NULL REFERENCES source_pacing(scope)
);
CREATE INDEX enrichment_job_pacing_scope ON enrichment_job_pacing(scope,job_uuid);
CREATE TABLE enrichment_attempt_pacing (
  job_uuid TEXT NOT NULL REFERENCES enrichment_job_pacing(job_uuid),
  fence INTEGER NOT NULL,
  scope TEXT NOT NULL REFERENCES source_pacing(scope),
  PRIMARY KEY(job_uuid,fence,scope),
  FOREIGN KEY(job_uuid,fence) REFERENCES archive_job_attempts(job_uuid,fence)
);
CREATE INDEX enrichment_attempt_pacing_scope ON enrichment_attempt_pacing(scope,job_uuid,fence);

INSERT INTO source_pacing(scope)
 SELECT DISTINCT source_scope_v1(c.target_url) FROM source_runs r
 JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision;
INSERT OR IGNORE INTO source_pacing(scope)
 SELECT DISTINCT source_scope_v1(u.url) FROM enrichment_job_targets b
 JOIN enrichment_targets t ON t.uuid=b.target_uuid JOIN source_post_urls u ON u.uuid=t.url_uuid;
INSERT INTO source_run_pacing(run_uuid,scope)
 SELECT r.uuid,source_scope_v1(c.target_url) FROM source_runs r
 JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision;
INSERT INTO enrichment_job_pacing(job_uuid,scope)
 SELECT b.job_uuid,source_scope_v1(u.url) FROM enrichment_job_targets b
 JOIN enrichment_targets t ON t.uuid=b.target_uuid JOIN source_post_urls u ON u.uuid=t.url_uuid;
INSERT INTO enrichment_attempt_pacing(job_uuid,fence,scope)
 SELECT a.job_uuid,a.fence,p.scope FROM archive_job_attempts a JOIN enrichment_job_pacing p ON p.job_uuid=a.job_uuid;
-- Preserve live ownership when upgrading an existing checkpointed attempt.
INSERT OR IGNORE INTO source_pacing(scope)
 SELECT source_scope_v1(json_extract(p.value,'$.url')) FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p;
INSERT OR IGNORE INTO enrichment_attempt_pacing(job_uuid,fence,scope)
 SELECT j.uuid,j.fence,source_scope_v1(json_extract(p.value,'$.url'))
 FROM archive_jobs j JOIN enrichment_checkpoints h ON h.job_uuid=j.uuid,json_each(h.body,'$.pending') p
 WHERE j.kind='post.enrich' AND j.state='running';
UPDATE source_pacing SET last_started_at_ms=max(
 coalesce((SELECT max(a.started_at_ms) FROM source_run_attempts a JOIN source_run_pacing p ON p.run_uuid=a.run_uuid WHERE p.scope=source_pacing.scope),0),
 coalesce((SELECT max(a.started_at_ms) FROM archive_job_attempts a JOIN enrichment_attempt_pacing p ON p.job_uuid=a.job_uuid AND p.fence=a.fence WHERE p.scope=source_pacing.scope),0));

CREATE TRIGGER source_run_pacing_bind AFTER INSERT ON source_runs
BEGIN
 INSERT OR IGNORE INTO source_pacing(scope)
  SELECT source_scope_v1(target_url) FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision;
 INSERT INTO source_run_pacing(run_uuid,scope)
  SELECT NEW.uuid,source_scope_v1(target_url) FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision;
END;
CREATE TRIGGER enrichment_job_pacing_bind AFTER INSERT ON enrichment_job_targets
BEGIN
 INSERT OR IGNORE INTO source_pacing(scope)
  SELECT source_scope_v1(u.url) FROM enrichment_targets t JOIN source_post_urls u ON u.uuid=t.url_uuid WHERE t.uuid=NEW.target_uuid;
 INSERT INTO enrichment_job_pacing(job_uuid,scope)
  SELECT NEW.job_uuid,source_scope_v1(u.url) FROM enrichment_targets t JOIN source_post_urls u ON u.uuid=t.url_uuid WHERE t.uuid=NEW.target_uuid;
END;
CREATE TRIGGER source_run_pacing_immutable BEFORE UPDATE ON source_run_pacing
BEGIN SELECT RAISE(ABORT,'source pacing binding is immutable'); END;
CREATE TRIGGER enrichment_job_pacing_immutable BEFORE UPDATE ON enrichment_job_pacing
BEGIN SELECT RAISE(ABORT,'source pacing binding is immutable'); END;
CREATE TRIGGER enrichment_attempt_pacing_bind AFTER INSERT ON archive_job_attempts
WHEN EXISTS(SELECT 1 FROM enrichment_job_pacing WHERE job_uuid=NEW.job_uuid)
BEGIN
 INSERT INTO enrichment_attempt_pacing(job_uuid,fence,scope)
  SELECT NEW.job_uuid,NEW.fence,scope FROM enrichment_job_pacing WHERE job_uuid=NEW.job_uuid;
 INSERT OR IGNORE INTO source_pacing(scope)
  SELECT source_scope_v1(json_extract(p.value,'$.url')) FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p WHERE h.job_uuid=NEW.job_uuid;
 INSERT OR IGNORE INTO enrichment_attempt_pacing(job_uuid,fence,scope)
  SELECT NEW.job_uuid,NEW.fence,source_scope_v1(json_extract(p.value,'$.url'))
  FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p WHERE h.job_uuid=NEW.job_uuid;
END;
CREATE TRIGGER enrichment_attempt_pacing_immutable BEFORE UPDATE ON enrichment_attempt_pacing
BEGIN SELECT RAISE(ABORT,'source attempt reservation is immutable'); END;
CREATE TRIGGER enrichment_attempt_pacing_current BEFORE INSERT ON enrichment_attempt_pacing
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=NEW.job_uuid AND j.kind='post.enrich' AND j.state='running' AND j.fence=NEW.fence)
BEGIN SELECT RAISE(ABORT,'source reservation requires the current enrichment attempt'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000054,'Shared download and enrichment source pacing','{}');
