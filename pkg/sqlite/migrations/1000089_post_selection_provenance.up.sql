-- Selections may use original evidence from any member of a consolidated post.
-- Capture/manifest ownership remains on the immutable source rows. Preserve
-- historical decisions and incoming (post_uuid, decision_uuid) references.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_selection_decision_rows AS SELECT rowid AS original_rowid,* FROM post_attachment_decisions;
CREATE TABLE native_selection_manifest_rows AS SELECT * FROM post_attachment_decision_manifests;
DROP TABLE post_attachment_decision_manifests;
DROP TABLE post_attachment_decisions;

CREATE TABLE post_attachment_decisions (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  revision INTEGER NOT NULL CHECK (revision > 0),
  mode TEXT NOT NULL CHECK (mode IN ('automatic', 'pinned', 'disabled')),
  origin TEXT NOT NULL CHECK (origin IN ('ingest', 'review', 'migration')),
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),
  capture_uuid TEXT,
  manifest_count INTEGER NOT NULL CHECK (manifest_count BETWEEN 0 AND 4099),
  signature TEXT NOT NULL CHECK (length(signature) = 64 AND signature NOT GLOB '*[^0-9a-f]*'),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (post_uuid, revision),
  UNIQUE (post_uuid, uuid),
  FOREIGN KEY (capture_uuid) REFERENCES source_captures(uuid),
  CHECK ((mode = 'disabled' AND capture_uuid IS NULL AND manifest_count = 0)
      OR (mode = 'pinned' AND capture_uuid IS NOT NULL AND manifest_count = 1)
      OR (mode = 'automatic' AND capture_uuid IS NOT NULL AND manifest_count > 0)),
  CHECK (origin != 'ingest' OR mode = 'automatic')
);
CREATE INDEX post_attachment_decisions_capture ON post_attachment_decisions(post_uuid, capture_uuid);
CREATE TABLE post_attachment_decision_manifests (
  post_uuid TEXT NOT NULL,
  decision_uuid TEXT NOT NULL,
  manifest_uuid TEXT NOT NULL,
  PRIMARY KEY (decision_uuid, manifest_uuid),
  FOREIGN KEY (post_uuid, decision_uuid) REFERENCES post_attachment_decisions(post_uuid, uuid),
  FOREIGN KEY (manifest_uuid) REFERENCES source_attachment_manifests(uuid)
) WITHOUT ROWID;
CREATE INDEX post_attachment_decision_manifests_source ON post_attachment_decision_manifests(post_uuid, manifest_uuid);

INSERT INTO post_attachment_decisions(rowid,uuid,post_uuid,revision,mode,origin,reason,capture_uuid,manifest_count,signature,created_at)
 SELECT original_rowid,uuid,post_uuid,revision,mode,origin,reason,capture_uuid,manifest_count,signature,created_at FROM native_selection_decision_rows;
INSERT INTO post_attachment_decision_manifests SELECT * FROM native_selection_manifest_rows;
DROP TABLE native_selection_manifest_rows;
DROP TABLE native_selection_decision_rows;
CREATE TRIGGER post_attachment_decision_immutable BEFORE UPDATE ON post_attachment_decisions
BEGIN SELECT RAISE(ABORT, 'attachment selection decisions are immutable'); END;
CREATE TRIGGER post_attachment_decision_manifest_immutable BEFORE UPDATE ON post_attachment_decision_manifests
BEGIN SELECT RAISE(ABORT, 'attachment selection evidence is immutable'); END;
CREATE TRIGGER post_attachment_decision_active_post BEFORE INSERT ON post_attachment_decisions
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot select attachments'); END;

CREATE TRIGGER post_attachment_decision_capture_scope BEFORE INSERT ON post_attachment_decisions
WHEN NEW.capture_uuid IS NOT NULL AND NOT EXISTS (
 SELECT 1 FROM source_captures c JOIN source_post_identities owner ON owner.post_uuid=c.post_uuid
 JOIN source_post_identities selected ON selected.post_uuid=NEW.post_uuid AND selected.canonical_uuid=owner.canonical_uuid
 WHERE c.uuid=NEW.capture_uuid)
BEGIN SELECT RAISE(ABORT,'attachment selection capture belongs to another post identity'); END;
CREATE TRIGGER post_attachment_decision_manifest_scope BEFORE INSERT ON post_attachment_decision_manifests
WHEN NOT EXISTS (
 SELECT 1 FROM source_attachment_manifests m JOIN source_post_identities owner ON owner.post_uuid=m.post_uuid
 JOIN source_post_identities selected ON selected.post_uuid=NEW.post_uuid AND selected.canonical_uuid=owner.canonical_uuid
 WHERE m.uuid=NEW.manifest_uuid)
BEGIN SELECT RAISE(ABORT,'attachment selection evidence belongs to another post identity'); END;
INSERT INTO native_migration_history(version,name,details)
 VALUES(1000089,'Select original attachment evidence across consolidated post identities','{}');
