CREATE UNIQUE INDEX source_captures_scope ON source_captures(post_uuid, uuid);

CREATE TABLE source_attachments (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  namespace TEXT NOT NULL CHECK (length(namespace) BETWEEN 1 AND 128),
  value TEXT NOT NULL CHECK (length(value) BETWEEN 1 AND 2048),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  UNIQUE (post_uuid, namespace, value),
  UNIQUE (post_uuid, uuid)
);

CREATE TABLE source_attachment_manifests (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  version TEXT NOT NULL,
  signature TEXT NOT NULL CHECK (length(signature) = 64 AND signature NOT GLOB '*[^0-9a-f]*'),
  complete INTEGER NOT NULL CHECK (complete IN (0, 1)),
  declared_album INTEGER NOT NULL CHECK (declared_album IN (0, 1)),
  expected_count INTEGER CHECK (expected_count BETWEEN 0 AND 1000000),
  entry_count INTEGER NOT NULL CHECK (entry_count BETWEEN 0 AND 4096),
  CHECK (expected_count IS NULL OR entry_count <= expected_count),
  CHECK (complete = 0 OR (expected_count IS NOT NULL AND expected_count = entry_count)),
  UNIQUE (post_uuid, signature),
  UNIQUE (post_uuid, uuid)
);
CREATE TABLE source_attachment_entries (
  manifest_uuid TEXT NOT NULL,
  post_uuid TEXT NOT NULL,
  position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 999999),
  attachment_uuid TEXT NOT NULL,
  media_kind TEXT NOT NULL CHECK (media_kind IN ('image', 'video', 'unknown')),
  PRIMARY KEY (manifest_uuid, position),
  UNIQUE (manifest_uuid, position, attachment_uuid),
  FOREIGN KEY (post_uuid, manifest_uuid) REFERENCES source_attachment_manifests(post_uuid, uuid),
  FOREIGN KEY (post_uuid, attachment_uuid) REFERENCES source_attachments(post_uuid, uuid)
) WITHOUT ROWID;
CREATE INDEX source_attachment_entries_attachment ON source_attachment_entries(attachment_uuid, manifest_uuid, position);
CREATE TABLE source_capture_attachment_manifests (
  capture_uuid TEXT NOT NULL PRIMARY KEY,
  post_uuid TEXT NOT NULL,
  manifest_uuid TEXT NOT NULL,
  UNIQUE (capture_uuid, manifest_uuid),
  FOREIGN KEY (post_uuid, capture_uuid) REFERENCES source_captures(post_uuid, uuid),
  FOREIGN KEY (post_uuid, manifest_uuid) REFERENCES source_attachment_manifests(post_uuid, uuid)
) WITHOUT ROWID;
CREATE INDEX source_capture_attachment_manifests_manifest ON source_capture_attachment_manifests(manifest_uuid, capture_uuid);

CREATE TABLE source_media_evidence (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  attachment_uuid TEXT NOT NULL,
  capture_uuid TEXT NOT NULL,
  manifest_uuid TEXT NOT NULL,
  position INTEGER NOT NULL,
  media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  file_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  basis TEXT NOT NULL CHECK (basis IN ('observed-file', 'verified-bytes', 'review', 'legacy')),
  details TEXT NOT NULL CHECK (length(details) <= 65536 AND json_valid(details) AND json_type(details) = 'object'),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (capture_uuid, manifest_uuid) REFERENCES source_capture_attachment_manifests(capture_uuid, manifest_uuid),
  FOREIGN KEY (manifest_uuid, position, attachment_uuid) REFERENCES source_attachment_entries(manifest_uuid, position, attachment_uuid)
);
CREATE INDEX source_media_evidence_attachment ON source_media_evidence(attachment_uuid, uuid);
CREATE INDEX source_media_evidence_candidates ON source_media_evidence(attachment_uuid, media_uuid);
CREATE INDEX source_media_evidence_capture ON source_media_evidence(capture_uuid, manifest_uuid);
CREATE INDEX source_media_evidence_entry ON source_media_evidence(manifest_uuid, position, attachment_uuid);
CREATE INDEX source_media_evidence_media ON source_media_evidence(media_uuid);
CREATE INDEX source_media_evidence_file ON source_media_evidence(file_uuid) WHERE file_uuid IS NOT NULL;

CREATE TABLE attachment_media_decisions (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  attachment_uuid TEXT NOT NULL REFERENCES source_attachments(uuid),
  revision INTEGER NOT NULL CHECK (revision > 0),
  state TEXT NOT NULL CHECK (state IN ('linked', 'unlinked', 'undecided')),
  media_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  origin TEXT NOT NULL CHECK (origin IN ('review', 'ingest', 'migration')),
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (attachment_uuid, revision),
  UNIQUE (attachment_uuid, uuid),
  CHECK ((state = 'linked' AND media_uuid IS NOT NULL) OR (state IN ('unlinked', 'undecided') AND media_uuid IS NULL))
);
CREATE INDEX attachment_media_decisions_media ON attachment_media_decisions(media_uuid) WHERE media_uuid IS NOT NULL;
CREATE TABLE attachment_media_links (
  attachment_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_attachments(uuid),
  decision_uuid TEXT NOT NULL,
  FOREIGN KEY (attachment_uuid, decision_uuid) REFERENCES attachment_media_decisions(attachment_uuid, uuid)
) WITHOUT ROWID;

CREATE TRIGGER source_attachment_manifests_immutable BEFORE UPDATE ON source_attachment_manifests
BEGIN SELECT RAISE(ABORT, 'source attachment evidence is immutable'); END;

CREATE TRIGGER source_attachment_entries_immutable BEFORE UPDATE ON source_attachment_entries
BEGIN SELECT RAISE(ABORT, 'source attachment evidence is immutable'); END;

CREATE TRIGGER source_capture_attachment_manifests_immutable BEFORE UPDATE ON source_capture_attachment_manifests
BEGIN SELECT RAISE(ABORT, 'source attachment evidence is immutable'); END;

CREATE TRIGGER source_attachment_identity_immutable BEFORE UPDATE OF uuid, post_uuid, namespace, value ON source_attachments
BEGIN SELECT RAISE(ABORT, 'source attachment identity is immutable'); END;

CREATE TRIGGER source_attachments_active_post BEFORE INSERT ON source_attachments
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot receive attachments'); END;

CREATE TRIGGER source_attachment_manifests_active_post BEFORE INSERT ON source_attachment_manifests
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot receive attachments'); END;

CREATE TRIGGER source_capture_attachment_manifests_active_post BEFORE INSERT ON source_capture_attachment_manifests
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot receive attachments'); END;

CREATE TRIGGER source_media_evidence_kind_insert BEFORE INSERT ON source_media_evidence
BEGIN
  SELECT RAISE(ABORT, 'attachment media requires a scene or image identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.media_uuid AND kind NOT IN ('scene', 'image'));
  SELECT RAISE(ABORT, 'attachment file requires a file identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.file_uuid AND kind != 'file');
END;

CREATE TRIGGER source_media_evidence_kind_update BEFORE UPDATE ON source_media_evidence
BEGIN
  SELECT RAISE(ABORT, 'attachment media requires a scene or image identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.media_uuid AND kind NOT IN ('scene', 'image'));
  SELECT RAISE(ABORT, 'attachment file requires a file identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.file_uuid AND kind != 'file');
END;

CREATE TRIGGER attachment_media_decisions_kind_insert BEFORE INSERT ON attachment_media_decisions
BEGIN
  SELECT RAISE(ABORT, 'attachment media requires a scene or image identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.media_uuid AND kind NOT IN ('scene', 'image'));
END;

CREATE TRIGGER attachment_media_decisions_kind_update BEFORE UPDATE ON attachment_media_decisions
BEGIN
  SELECT RAISE(ABORT, 'attachment media requires a scene or image identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.media_uuid AND kind NOT IN ('scene', 'image'));
END;

CREATE TRIGGER source_media_evidence_immutable BEFORE UPDATE ON source_media_evidence
WHEN NEW.uuid != OLD.uuid OR NEW.attachment_uuid != OLD.attachment_uuid OR NEW.capture_uuid != OLD.capture_uuid
  OR NEW.manifest_uuid != OLD.manifest_uuid OR NEW.position != OLD.position OR NEW.basis != OLD.basis
  OR NEW.details != OLD.details OR NEW.created_at != OLD.created_at
  OR (NEW.media_uuid != OLD.media_uuid AND EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.media_uuid))
  OR (NEW.file_uuid IS NOT OLD.file_uuid AND (NEW.file_uuid IS NULL OR OLD.file_uuid IS NULL OR EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.file_uuid)))
BEGIN SELECT RAISE(ABORT, 'source media evidence is immutable'); END;
CREATE TRIGGER attachment_media_decision_immutable BEFORE UPDATE ON attachment_media_decisions
WHEN NEW.uuid != OLD.uuid OR NEW.attachment_uuid != OLD.attachment_uuid OR NEW.revision != OLD.revision
  OR NEW.state != OLD.state OR NEW.origin != OLD.origin OR NEW.reason != OLD.reason OR NEW.created_at != OLD.created_at
  OR (NEW.media_uuid IS NOT OLD.media_uuid AND (NEW.media_uuid IS NULL OR OLD.media_uuid IS NULL OR EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.media_uuid)))
BEGIN SELECT RAISE(ABORT, 'attachment media decisions are immutable'); END;
CREATE TRIGGER attachment_media_head_forward BEFORE UPDATE OF decision_uuid ON attachment_media_links
WHEN NEW.decision_uuid != OLD.decision_uuid
BEGIN
  SELECT RAISE(ABORT, 'attachment media head cannot move backwards')
    WHERE (SELECT revision FROM attachment_media_decisions WHERE uuid = NEW.decision_uuid)
       <= (SELECT revision FROM attachment_media_decisions WHERE uuid = OLD.decision_uuid);
END;
INSERT INTO native_migration_history(version, name, details)
VALUES (1000008, 'Ordered source attachments and audited library media associations', '{}');
