CREATE TABLE translation_requests (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 original_text TEXT NOT NULL CHECK(length(CAST(original_text AS BLOB))<=4194304),
 original_sha256 TEXT NOT NULL CHECK(length(original_sha256)=64 AND original_sha256 NOT GLOB '*[^0-9a-f]*'),
 target_language TEXT NOT NULL CHECK(length(CAST(target_language AS BLOB)) BETWEEN 1 AND 128),
 policy TEXT NOT NULL CHECK(policy='translate-shell-bing-text-v1'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TRIGGER translation_request_immutable BEFORE UPDATE ON translation_requests
BEGIN SELECT RAISE(ABORT,'translation request identity is immutable'); END;

CREATE TABLE translation_cache (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 request_uuid TEXT NOT NULL UNIQUE REFERENCES translation_requests(uuid),
 status TEXT NOT NULL CHECK(status IN ('translated','unchanged','no_text')),
 translation_uuid TEXT REFERENCES source_translations(uuid),
 captured_at TEXT NOT NULL CHECK(length(CAST(captured_at AS BLOB))<=64),
 origin TEXT NOT NULL CHECK(origin IN ('worker','migration','review')),
 recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK((status='no_text')=(translation_uuid IS NULL))
);
CREATE TRIGGER translation_cache_immutable BEFORE UPDATE ON translation_cache
BEGIN SELECT RAISE(ABORT,'translation cache outcomes are immutable'); END;
CREATE TRIGGER translation_cache_scope BEFORE INSERT ON translation_cache
WHEN NEW.translation_uuid IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM source_translations s JOIN translation_requests r ON r.uuid=NEW.request_uuid
 WHERE s.uuid=NEW.translation_uuid AND s.original_text IS r.original_text
 AND s.original_sha256 IS r.original_sha256 AND s.target_language IS r.target_language
 AND (NEW.status!='unchanged' OR s.translated_text=r.original_text))
BEGIN SELECT RAISE(ABORT,'translation cache input or output scope does not match'); END;

CREATE TABLE translation_targets (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 request_uuid TEXT NOT NULL REFERENCES translation_requests(uuid),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 collection_uuid TEXT,
 collection_revision INTEGER CHECK(collection_revision IS NULL OR collection_revision>0),
 field TEXT NOT NULL CHECK(field IN ('title','caption')),
 origin TEXT NOT NULL CHECK(origin IN ('capture','migration','review')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
 state TEXT NOT NULL CHECK(state IN ('held','pending','completed','review')),
 priority INTEGER NOT NULL CHECK(typeof(priority)='integer' AND priority BETWEEN 0 AND 100),
 not_before DATETIME NOT NULL,
 cache_uuid TEXT REFERENCES translation_cache(uuid),
 evidence_uuid TEXT REFERENCES source_translation_evidence(uuid),
 reason TEXT NOT NULL DEFAULT '' CHECK(reason IN ('','post_forgotten')),
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK((collection_uuid IS NULL)=(collection_revision IS NULL)),
 CHECK((state='review')=(reason!='')),
 CHECK(state!='completed' OR cache_uuid IS NOT NULL),
 CHECK(state='completed' OR evidence_uuid IS NULL),
 CHECK(state NOT IN ('held','pending') OR cache_uuid IS NULL)
);
CREATE INDEX translation_targets_request ON translation_targets(request_uuid,uuid);
CREATE INDEX translation_targets_post ON translation_targets(post_uuid,uuid);
CREATE INDEX translation_targets_request_state ON translation_targets(request_uuid,state,uuid);
CREATE INDEX translation_targets_post_state ON translation_targets(post_uuid,state,uuid);
CREATE INDEX translation_targets_ready ON translation_targets(priority DESC,not_before,uuid) WHERE state='pending';
CREATE TRIGGER translation_target_initial BEFORE INSERT ON translation_targets
WHEN NEW.revision!=1 OR NEW.state NOT IN ('held','pending') OR NEW.cache_uuid IS NOT NULL OR NEW.evidence_uuid IS NOT NULL OR NEW.reason!=''
BEGIN SELECT RAISE(ABORT,'translation targets require initial pending or held work'); END;
CREATE TRIGGER translation_target_active BEFORE INSERT ON translation_targets
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'forgotten post cannot receive translation work'); END;
CREATE TRIGGER translation_target_identity BEFORE UPDATE ON translation_targets
WHEN NEW.uuid!=OLD.uuid OR NEW.request_uuid!=OLD.request_uuid OR NEW.post_uuid!=OLD.post_uuid
 OR NEW.collection_uuid IS NOT OLD.collection_uuid OR NEW.collection_revision IS NOT OLD.collection_revision
 OR NEW.field!=OLD.field OR NEW.origin!=OLD.origin OR NEW.created_at!=OLD.created_at
 OR NEW.revision!=OLD.revision+1 OR OLD.state IN ('completed','review') OR NEW.updated_at<OLD.updated_at
BEGIN SELECT RAISE(ABORT,'translation target identity or transition changed'); END;
CREATE TRIGGER translation_target_scope BEFORE UPDATE ON translation_targets
WHEN (NEW.cache_uuid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM translation_cache c WHERE c.uuid=NEW.cache_uuid AND c.request_uuid=NEW.request_uuid))
 OR (NEW.state='completed' AND NOT EXISTS(SELECT 1 FROM translation_cache c WHERE c.uuid=NEW.cache_uuid
  AND ((c.status='no_text' AND NEW.evidence_uuid IS NULL) OR EXISTS(SELECT 1 FROM source_translation_evidence e
   WHERE e.uuid=NEW.evidence_uuid AND e.post_uuid=NEW.post_uuid AND e.translation_uuid=c.translation_uuid
   AND e.collection_uuid IS NEW.collection_uuid AND e.collection_revision IS NEW.collection_revision))))
BEGIN SELECT RAISE(ABORT,'translation target completion scope does not match'); END;

CREATE TABLE translation_target_history (
 target_uuid TEXT NOT NULL REFERENCES translation_targets(uuid),
 revision INTEGER NOT NULL CHECK(revision>0),
 state TEXT NOT NULL CHECK(state IN ('held','pending','completed','review')),
 priority INTEGER NOT NULL CHECK(priority BETWEEN 0 AND 100),
 not_before DATETIME NOT NULL,
 cache_uuid TEXT REFERENCES translation_cache(uuid),
 evidence_uuid TEXT REFERENCES source_translation_evidence(uuid),
 reason TEXT NOT NULL,
 recorded_at DATETIME NOT NULL,
 PRIMARY KEY(target_uuid,revision)
);
CREATE TRIGGER translation_target_history_immutable BEFORE UPDATE ON translation_target_history
BEGIN SELECT RAISE(ABORT,'translation target history is immutable'); END;
CREATE TRIGGER translation_target_history_insert AFTER INSERT ON translation_targets
BEGIN
 INSERT INTO translation_target_history VALUES(NEW.uuid,NEW.revision,NEW.state,NEW.priority,NEW.not_before,NEW.cache_uuid,NEW.evidence_uuid,NEW.reason,NEW.updated_at);
END;
CREATE TRIGGER translation_target_history_update AFTER UPDATE ON translation_targets
BEGIN
 INSERT INTO translation_target_history VALUES(NEW.uuid,NEW.revision,NEW.state,NEW.priority,NEW.not_before,NEW.cache_uuid,NEW.evidence_uuid,NEW.reason,NEW.updated_at);
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000044,'Shared translation requests, cache outcomes and durable post targets','{}');
