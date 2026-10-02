-- Catalog appearances can identify a post and local media without retaining
-- an ordered source attachment or the capture that established the association.
-- Preserve that evidence without manufacturing a manifest or capture.
CREATE TABLE source_media_evidence_next (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  attachment_uuid TEXT,
  capture_uuid TEXT,
  manifest_uuid TEXT,
  position INTEGER,
  media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  file_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  basis TEXT NOT NULL CHECK (basis IN ('observed-file', 'verified-bytes', 'review', 'legacy')),
  details TEXT NOT NULL CHECK (length(details) <= 65536 AND json_valid(details) AND json_type(details) = 'object'),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ((manifest_uuid IS NULL) = (position IS NULL)),
  CHECK ((manifest_uuid IS NOT NULL) = (attachment_uuid IS NOT NULL AND capture_uuid IS NOT NULL)),
  CHECK (basis NOT IN ('observed-file','verified-bytes')
    OR (attachment_uuid IS NOT NULL AND capture_uuid IS NOT NULL AND file_uuid IS NOT NULL)),
  FOREIGN KEY (post_uuid, attachment_uuid) REFERENCES source_attachments(post_uuid, uuid),
  FOREIGN KEY (post_uuid, capture_uuid) REFERENCES source_captures(post_uuid, uuid),
  FOREIGN KEY (capture_uuid, manifest_uuid) REFERENCES source_capture_attachment_manifests(capture_uuid, manifest_uuid),
  FOREIGN KEY (manifest_uuid, position, attachment_uuid) REFERENCES source_attachment_entries(manifest_uuid, position, attachment_uuid)
);
INSERT INTO source_media_evidence_next
  (uuid,post_uuid,attachment_uuid,capture_uuid,manifest_uuid,position,media_uuid,file_uuid,basis,details,created_at)
SELECT e.uuid,a.post_uuid,e.attachment_uuid,e.capture_uuid,e.manifest_uuid,e.position,e.media_uuid,e.file_uuid,e.basis,e.details,e.created_at
FROM source_media_evidence e LEFT JOIN source_attachments a ON a.uuid=e.attachment_uuid;
DROP TABLE source_media_evidence;
ALTER TABLE source_media_evidence_next RENAME TO source_media_evidence;

CREATE INDEX source_media_evidence_post ON source_media_evidence(post_uuid, uuid);
CREATE INDEX source_media_evidence_attachment ON source_media_evidence(attachment_uuid, uuid);
CREATE INDEX source_media_evidence_candidates ON source_media_evidence(attachment_uuid, media_uuid);
CREATE INDEX source_media_evidence_capture ON source_media_evidence(capture_uuid, manifest_uuid);
CREATE INDEX source_media_evidence_entry ON source_media_evidence(manifest_uuid, position, attachment_uuid);
CREATE INDEX source_media_evidence_media ON source_media_evidence(media_uuid);
CREATE INDEX source_media_evidence_file ON source_media_evidence(file_uuid) WHERE file_uuid IS NOT NULL;

CREATE TRIGGER source_media_evidence_kind_insert BEFORE INSERT ON source_media_evidence
BEGIN
  SELECT RAISE(ABORT, 'source media evidence requires a scene or image identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid=NEW.media_uuid AND kind NOT IN ('scene','image'));
  SELECT RAISE(ABORT, 'source media evidence file requires a file identity')
    WHERE NEW.file_uuid IS NOT NULL AND EXISTS (SELECT 1 FROM archive_entities WHERE uuid=NEW.file_uuid AND kind!='file');
END;
CREATE TRIGGER source_media_evidence_kind_update BEFORE UPDATE OF media_uuid,file_uuid ON source_media_evidence
BEGIN
  SELECT RAISE(ABORT, 'source media evidence requires a scene or image identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid=NEW.media_uuid AND kind NOT IN ('scene','image'));
  SELECT RAISE(ABORT, 'source media evidence file requires a file identity')
    WHERE NEW.file_uuid IS NOT NULL AND EXISTS (SELECT 1 FROM archive_entities WHERE uuid=NEW.file_uuid AND kind!='file');
END;
CREATE TRIGGER source_media_evidence_immutable BEFORE UPDATE ON source_media_evidence
WHEN NEW.uuid!=OLD.uuid OR NEW.post_uuid!=OLD.post_uuid
  OR NEW.attachment_uuid IS NOT OLD.attachment_uuid OR NEW.capture_uuid IS NOT OLD.capture_uuid
  OR NEW.manifest_uuid IS NOT OLD.manifest_uuid OR NEW.position IS NOT OLD.position OR NEW.basis!=OLD.basis
  OR NEW.details!=OLD.details OR NEW.created_at!=OLD.created_at
  OR (NEW.media_uuid!=OLD.media_uuid AND EXISTS (SELECT 1 FROM archive_entities WHERE uuid=OLD.media_uuid))
  OR (NEW.file_uuid IS NOT OLD.file_uuid AND (NEW.file_uuid IS NULL OR OLD.file_uuid IS NULL OR EXISTS (SELECT 1 FROM archive_entities WHERE uuid=OLD.file_uuid)))
BEGIN SELECT RAISE(ABORT, 'source media evidence is immutable'); END;
CREATE TRIGGER source_media_evidence_active_post BEFORE INSERT ON source_media_evidence
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT, 'source post has been forgotten'); END;
CREATE TRIGGER source_media_evidence_revision AFTER INSERT ON source_media_evidence
BEGIN
  UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid;
  UPDATE source_attachments SET revision=revision+1 WHERE uuid=NEW.attachment_uuid;
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000036,'Post media evidence without invented capture or attachment order','{}');
