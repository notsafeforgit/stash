-- Enrichment targets refer to retained post URLs and an exact source binding.
-- Execution attempts belong to the durable job layer, not these domain records.
CREATE TABLE enrichment_targets (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 url_uuid TEXT NOT NULL REFERENCES source_post_urls(uuid),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(typeof(collection_revision)='integer' AND collection_revision>0),
 policy TEXT NOT NULL CHECK(policy='gallery-dl-metadata-v1'),
 origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
 state TEXT NOT NULL CHECK(state IN ('held','pending','review','excluded','completed')),
 priority INTEGER NOT NULL CHECK(typeof(priority)='integer' AND priority BETWEEN 0 AND 100),
 not_before DATETIME NOT NULL,
 reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB))<=64 AND reason NOT GLOB '*[^a-z0-9_]*'
  AND (reason='' OR substr(reason,1,1) GLOB '[a-z]')),
 completion_uuid TEXT REFERENCES enrichment_completions(uuid) DEFERRABLE INITIALLY DEFERRED,
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 UNIQUE(post_uuid,url_uuid,collection_uuid,collection_revision,policy),
 UNIQUE(uuid,completion_uuid),
 CHECK((state='completed')=(completion_uuid IS NOT NULL)),
 CHECK(state NOT IN ('review','excluded') OR reason!=''),
 CHECK(state!='completed' OR reason='')
);
CREATE INDEX enrichment_targets_post ON enrichment_targets(post_uuid,uuid);
CREATE INDEX enrichment_targets_post_state ON enrichment_targets(post_uuid,state,uuid);
CREATE INDEX enrichment_targets_collection ON enrichment_targets(collection_uuid,uuid);
CREATE INDEX enrichment_targets_collection_state ON enrichment_targets(collection_uuid,state,uuid);
CREATE INDEX enrichment_targets_ready ON enrichment_targets(collection_uuid,priority DESC,not_before,uuid) WHERE state='pending';
CREATE TRIGGER enrichment_target_initial BEFORE INSERT ON enrichment_targets
WHEN NEW.revision!=1 OR NEW.state='completed' OR NEW.created_at!=NEW.updated_at
 OR NOT EXISTS(SELECT 1 FROM source_post_urls u JOIN source_posts p ON p.uuid=u.post_uuid
  WHERE u.uuid=NEW.url_uuid AND p.uuid=NEW.post_uuid AND p.state='active')
 OR (NEW.state='pending' AND NOT EXISTS(SELECT 1 FROM source_collections c
  JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision
  WHERE c.uuid=NEW.collection_uuid AND c.revision=NEW.collection_revision AND r.state='active'))
BEGIN SELECT RAISE(ABORT,'enrichment target requires retained post evidence and an eligible initial state'); END;
CREATE TRIGGER enrichment_target_identity BEFORE UPDATE ON enrichment_targets
WHEN NEW.uuid!=OLD.uuid OR NEW.post_uuid!=OLD.post_uuid OR NEW.url_uuid!=OLD.url_uuid
 OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
 OR NEW.policy!=OLD.policy OR NEW.origin!=OLD.origin OR NEW.created_at!=OLD.created_at
 OR NEW.revision!=OLD.revision+1 OR OLD.state='completed' OR NEW.updated_at<OLD.updated_at
BEGIN SELECT RAISE(ABORT,'enrichment target identity or transition changed'); END;

CREATE TABLE enrichment_completions (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 target_uuid TEXT NOT NULL UNIQUE REFERENCES enrichment_targets(uuid),
 expected_revision INTEGER NOT NULL CHECK(typeof(expected_revision)='integer' AND expected_revision>0),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 capture_count INTEGER NOT NULL CHECK(typeof(capture_count)='integer' AND capture_count BETWEEN 1 AND 1024),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(target_uuid,uuid) REFERENCES enrichment_targets(uuid,completion_uuid) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER enrichment_completion_immutable BEFORE UPDATE ON enrichment_completions
BEGIN SELECT RAISE(ABORT,'enrichment completion evidence is immutable'); END;
CREATE TRIGGER enrichment_completion_scope BEFORE INSERT ON enrichment_completions
WHEN NOT EXISTS(SELECT 1 FROM enrichment_targets t WHERE t.uuid=NEW.target_uuid
 AND t.revision=NEW.expected_revision AND t.state='pending' AND t.not_before<=NEW.created_at AND t.updated_at<=NEW.created_at)
BEGIN SELECT RAISE(ABORT,'enrichment completion requires the pending target revision'); END;
CREATE TABLE enrichment_completion_captures (
 completion_uuid TEXT NOT NULL REFERENCES enrichment_completions(uuid),
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 PRIMARY KEY(completion_uuid,capture_uuid)
);
CREATE TRIGGER enrichment_completion_capture_immutable BEFORE UPDATE ON enrichment_completion_captures
BEGIN SELECT RAISE(ABORT,'enrichment completion capture evidence is immutable'); END;
CREATE TRIGGER enrichment_completion_capture_scope BEFORE INSERT ON enrichment_completion_captures
WHEN NOT EXISTS(SELECT 1 FROM enrichment_completions e JOIN enrichment_targets t ON t.uuid=e.target_uuid
 JOIN source_captures c ON c.uuid=NEW.capture_uuid AND c.post_uuid=t.post_uuid
 JOIN source_collection_captures b ON b.capture_uuid=c.uuid AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision
 WHERE e.uuid=NEW.completion_uuid AND c.origin IN ('gallery-dl','gallery-dl-enrichment')
 AND t.state='pending' AND t.revision=e.expected_revision
 AND e.capture_count>(SELECT count(*) FROM enrichment_completion_captures WHERE completion_uuid=e.uuid))
BEGIN SELECT RAISE(ABORT,'enrichment completion capture is outside its post or collection scope'); END;
CREATE TRIGGER enrichment_target_scope BEFORE UPDATE ON enrichment_targets
WHEN (NEW.state IN ('pending','completed') AND (NOT EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state='active')
 OR NOT EXISTS(SELECT 1 FROM source_collections c JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision
  WHERE c.uuid=NEW.collection_uuid AND c.revision=NEW.collection_revision AND r.state='active')))
 OR (NEW.state='completed' AND (OLD.state!='pending' OR NOT EXISTS(SELECT 1 FROM enrichment_completions e
  WHERE e.uuid=NEW.completion_uuid AND e.target_uuid=NEW.uuid AND e.expected_revision=OLD.revision
  AND e.created_at=NEW.updated_at AND NEW.not_before<=NEW.updated_at
  AND e.capture_count=(SELECT count(*) FROM enrichment_completion_captures WHERE completion_uuid=e.uuid))))
BEGIN SELECT RAISE(ABORT,'enrichment completion or active source scope does not match'); END;

CREATE TABLE enrichment_target_history (
 target_uuid TEXT NOT NULL REFERENCES enrichment_targets(uuid),
 revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision>0),
 state TEXT NOT NULL CHECK(state IN ('held','pending','review','excluded','completed')),
 priority INTEGER NOT NULL CHECK(typeof(priority)='integer' AND priority BETWEEN 0 AND 100),
 not_before DATETIME NOT NULL,
 reason TEXT NOT NULL,
 completion_uuid TEXT REFERENCES enrichment_completions(uuid),
 recorded_at DATETIME NOT NULL,
 PRIMARY KEY(target_uuid,revision)
);
CREATE TRIGGER enrichment_target_history_immutable BEFORE UPDATE ON enrichment_target_history
BEGIN SELECT RAISE(ABORT,'enrichment target history is immutable'); END;
CREATE TRIGGER enrichment_target_history_insert AFTER INSERT ON enrichment_targets
BEGIN
 INSERT INTO enrichment_target_history VALUES(NEW.uuid,NEW.revision,NEW.state,NEW.priority,NEW.not_before,NEW.reason,NEW.completion_uuid,NEW.updated_at);
END;
CREATE TRIGGER enrichment_target_history_update AFTER UPDATE ON enrichment_targets
BEGIN
 INSERT INTO enrichment_target_history VALUES(NEW.uuid,NEW.revision,NEW.state,NEW.priority,NEW.not_before,NEW.reason,NEW.completion_uuid,NEW.updated_at);
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000050,'Revisioned post enrichment targets and retained completion evidence','{}');
