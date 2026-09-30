-- Extend the shared identity registry without changing any existing UUID.
-- Copy/drop/recreate preserves incoming foreign keys and their original names.
-- Deferred checks remain enabled until this migration transaction commits.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_gallery_identity_rows AS SELECT * FROM archive_entities;
DROP TABLE archive_entities;

CREATE TABLE archive_entities (
  uuid TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  kind TEXT NOT NULL CHECK (kind IN ('performer', 'scene', 'image', 'file', 'gallery')),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'redirected', 'deleted')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  performer_id INTEGER REFERENCES performers(id) ON UPDATE CASCADE,
  scene_id INTEGER REFERENCES scenes(id) ON UPDATE CASCADE,
  image_id INTEGER REFERENCES images(id) ON UPDATE CASCADE,
  file_id INTEGER REFERENCES files(id) ON UPDATE CASCADE,
  gallery_id INTEGER REFERENCES galleries(id) ON UPDATE CASCADE,
  original_id INTEGER CHECK (original_id > 0),
  redirect_to TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retired_at DATETIME,
  CHECK (redirect_to IS NULL OR redirect_to != uuid),
  CHECK (
    (state = 'active' AND redirect_to IS NULL AND retired_at IS NULL AND (
      (kind = 'performer' AND performer_id IS NOT NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL) OR
      (kind = 'scene' AND performer_id IS NULL AND scene_id IS NOT NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL) OR
      (kind = 'image' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NOT NULL AND file_id IS NULL AND gallery_id IS NULL) OR
      (kind = 'file' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NOT NULL AND gallery_id IS NULL) OR
      (kind = 'gallery' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NOT NULL)
    )) OR
    (state IN ('redirected', 'deleted') AND retired_at IS NOT NULL
      AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL
      AND ((state = 'redirected' AND redirect_to IS NOT NULL) OR (state = 'deleted' AND redirect_to IS NULL)))
  )
);

INSERT INTO archive_entities(uuid, kind, state, revision, performer_id, scene_id, image_id, file_id, original_id, redirect_to, created_at, retired_at)
SELECT uuid, kind, state, revision, performer_id, scene_id, image_id, file_id, original_id, redirect_to, created_at, retired_at FROM native_gallery_identity_rows;
DROP TABLE native_gallery_identity_rows;
CREATE INDEX archive_entities_redirect ON archive_entities(redirect_to) WHERE redirect_to IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_performer ON archive_entities(performer_id) WHERE performer_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_scene ON archive_entities(scene_id) WHERE scene_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_image ON archive_entities(image_id) WHERE image_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_file ON archive_entities(file_id) WHERE file_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_gallery ON archive_entities(gallery_id) WHERE gallery_id IS NOT NULL;

CREATE TRIGGER archive_entity_redirect_insert BEFORE INSERT ON archive_entities
WHEN NEW.redirect_to IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'archive identity redirect kinds differ')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.redirect_to AND kind != NEW.kind);
  SELECT RAISE(ABORT, 'archive identity redirect cycle') WHERE EXISTS (
    WITH RECURSIVE chain(uuid, redirect_to) AS (
      SELECT uuid, redirect_to FROM archive_entities WHERE uuid = NEW.redirect_to
      UNION
      SELECT e.uuid, e.redirect_to FROM archive_entities e JOIN chain c ON e.uuid = c.redirect_to
    ) SELECT 1 FROM chain WHERE uuid = NEW.uuid
  );
END;

CREATE TRIGGER archive_entity_redirect_update BEFORE UPDATE OF redirect_to, kind ON archive_entities
WHEN NEW.redirect_to IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'archive identity redirect kinds differ')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.redirect_to AND kind != NEW.kind);
  SELECT RAISE(ABORT, 'archive identity redirect cycle') WHERE EXISTS (
    WITH RECURSIVE chain(uuid, redirect_to) AS (
      SELECT uuid, redirect_to FROM archive_entities WHERE uuid = NEW.redirect_to
      UNION
      SELECT e.uuid, e.redirect_to FROM archive_entities e JOIN chain c ON e.uuid = c.redirect_to
    ) SELECT 1 FROM chain WHERE uuid = NEW.uuid
  );
END;

CREATE TRIGGER archive_entity_kind_immutable BEFORE UPDATE OF kind ON archive_entities
WHEN NEW.kind != OLD.kind
BEGIN SELECT RAISE(ABORT, 'archive identity kind is immutable'); END;
CREATE TRIGGER archive_entity_no_resurrection BEFORE UPDATE OF state ON archive_entities
WHEN OLD.state != 'active' AND NEW.state != OLD.state
BEGIN SELECT RAISE(ABORT, 'retired archive identity state is immutable'); END;

INSERT INTO archive_entities(kind, gallery_id, original_id, created_at)
SELECT 'gallery', id, id, created_at FROM galleries;
CREATE TRIGGER archive_gallery_created AFTER INSERT ON galleries
BEGIN
  INSERT INTO archive_entities(kind, gallery_id, original_id, created_at)
  VALUES ('gallery', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_gallery_changed AFTER UPDATE ON galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.id;
END;
CREATE TRIGGER archive_gallery_deleted BEFORE DELETE ON galleries
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    gallery_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE gallery_id = OLD.id;
END;
CREATE TRIGGER archive_gallery_galleries_images_insert AFTER INSERT ON galleries_images
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_images_delete AFTER DELETE ON galleries_images
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_images_update AFTER UPDATE ON galleries_images
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_scenes_galleries_insert AFTER INSERT ON scenes_galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_scenes_galleries_delete AFTER DELETE ON scenes_galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_scenes_galleries_update AFTER UPDATE ON scenes_galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_galleries_files_insert AFTER INSERT ON galleries_files
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_files_delete AFTER DELETE ON galleries_files
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_files_update AFTER UPDATE ON galleries_files
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_performers_galleries_insert AFTER INSERT ON performers_galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_performers_galleries_delete AFTER DELETE ON performers_galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_performers_galleries_update AFTER UPDATE ON performers_galleries
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_galleries_tags_insert AFTER INSERT ON galleries_tags
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_tags_delete AFTER DELETE ON galleries_tags
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_tags_update AFTER UPDATE ON galleries_tags
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_gallery_urls_insert AFTER INSERT ON gallery_urls
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_gallery_urls_delete AFTER DELETE ON gallery_urls
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_gallery_urls_update AFTER UPDATE ON gallery_urls
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_gallery_custom_fields_insert AFTER INSERT ON gallery_custom_fields
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_gallery_custom_fields_delete AFTER DELETE ON gallery_custom_fields
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_gallery_custom_fields_update AFTER UPDATE ON gallery_custom_fields
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;
CREATE TRIGGER archive_gallery_galleries_chapters_insert AFTER INSERT ON galleries_chapters
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_chapters_delete AFTER DELETE ON galleries_chapters
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = OLD.gallery_id;
END;
CREATE TRIGGER archive_gallery_galleries_chapters_update AFTER UPDATE ON galleries_chapters
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id IN (OLD.gallery_id, NEW.gallery_id);
END;

INSERT INTO native_migration_history(version, name, details)
SELECT 1000007, 'Portable gallery identities and membership revisions',
  json_object('galleries', count(*)) FROM galleries;
