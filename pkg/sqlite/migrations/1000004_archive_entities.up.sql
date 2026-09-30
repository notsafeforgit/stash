-- Portable identities are separate from content fingerprints and local row IDs.
-- Typed foreign keys preserve the existing media model. Redirects and deletion
-- tombstones outlive their local rows; a reused integer ID gets a new UUID.
CREATE TABLE archive_entities (
  uuid TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  kind TEXT NOT NULL CHECK (kind IN ('performer', 'scene', 'image', 'file')),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'redirected', 'deleted')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  performer_id INTEGER REFERENCES performers(id) ON UPDATE CASCADE,
  scene_id INTEGER REFERENCES scenes(id) ON UPDATE CASCADE,
  image_id INTEGER REFERENCES images(id) ON UPDATE CASCADE,
  file_id INTEGER REFERENCES files(id) ON UPDATE CASCADE,
  original_id INTEGER CHECK (original_id > 0),
  redirect_to TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retired_at DATETIME,
  CHECK (redirect_to IS NULL OR redirect_to != uuid),
  CHECK (
    (state = 'active' AND redirect_to IS NULL AND retired_at IS NULL AND (
      (kind = 'performer' AND performer_id IS NOT NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL) OR
      (kind = 'scene' AND performer_id IS NULL AND scene_id IS NOT NULL AND image_id IS NULL AND file_id IS NULL) OR
      (kind = 'image' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NOT NULL AND file_id IS NULL) OR
      (kind = 'file' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NOT NULL)
    )) OR
    (state IN ('redirected', 'deleted') AND retired_at IS NOT NULL
      AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL
      AND ((state = 'redirected' AND redirect_to IS NOT NULL) OR (state = 'deleted' AND redirect_to IS NULL)))
  )
);
CREATE INDEX archive_entities_redirect ON archive_entities(redirect_to) WHERE redirect_to IS NOT NULL;

CREATE UNIQUE INDEX archive_entities_performer ON archive_entities(performer_id) WHERE performer_id IS NOT NULL;
INSERT INTO archive_entities(kind, performer_id, original_id, created_at)
SELECT 'performer', id, id, created_at FROM performers;
CREATE TRIGGER archive_performer_created AFTER INSERT ON performers
BEGIN
  INSERT INTO archive_entities(kind, performer_id, original_id, created_at)
  VALUES ('performer', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_performer_changed AFTER UPDATE ON performers
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE performer_id = NEW.id;
END;
CREATE TRIGGER archive_performer_deleted BEFORE DELETE ON performers
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    performer_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE performer_id = OLD.id;
END;

CREATE UNIQUE INDEX archive_entities_scene ON archive_entities(scene_id) WHERE scene_id IS NOT NULL;
INSERT INTO archive_entities(kind, scene_id, original_id, created_at)
SELECT 'scene', id, id, created_at FROM scenes;
CREATE TRIGGER archive_scene_created AFTER INSERT ON scenes
BEGIN
  INSERT INTO archive_entities(kind, scene_id, original_id, created_at)
  VALUES ('scene', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_scene_changed AFTER UPDATE ON scenes
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE scene_id = NEW.id;
END;
CREATE TRIGGER archive_scene_deleted BEFORE DELETE ON scenes
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    scene_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE scene_id = OLD.id;
END;

CREATE UNIQUE INDEX archive_entities_image ON archive_entities(image_id) WHERE image_id IS NOT NULL;
INSERT INTO archive_entities(kind, image_id, original_id, created_at)
SELECT 'image', id, id, created_at FROM images;
CREATE TRIGGER archive_image_created AFTER INSERT ON images
BEGIN
  INSERT INTO archive_entities(kind, image_id, original_id, created_at)
  VALUES ('image', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_image_changed AFTER UPDATE ON images
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE image_id = NEW.id;
END;
CREATE TRIGGER archive_image_deleted BEFORE DELETE ON images
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    image_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE image_id = OLD.id;
END;

CREATE UNIQUE INDEX archive_entities_file ON archive_entities(file_id) WHERE file_id IS NOT NULL;
INSERT INTO archive_entities(kind, file_id, original_id, created_at)
SELECT 'file', id, id, created_at FROM files;
CREATE TRIGGER archive_file_created AFTER INSERT ON files
BEGIN
  INSERT INTO archive_entities(kind, file_id, original_id, created_at)
  VALUES ('file', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_file_changed AFTER UPDATE ON files
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE file_id = NEW.id;
END;
CREATE TRIGGER archive_file_deleted BEFORE DELETE ON files
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    file_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE file_id = OLD.id;
END;

CREATE TRIGGER archive_performer_name_insert AFTER INSERT ON performer_names
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE performer_id = NEW.performer_id;
END;

CREATE TRIGGER archive_performer_name_delete AFTER DELETE ON performer_names
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE performer_id = OLD.performer_id;
END;

CREATE TRIGGER archive_performer_name_update AFTER UPDATE ON performer_names
BEGIN
  UPDATE archive_entities SET revision = revision + 1 WHERE performer_id = NEW.performer_id;
END;

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
INSERT INTO native_migration_history(version, name, details)
SELECT 1000004, 'Portable archive identities and durable redirects',
  json_object('entities', count(*)) FROM archive_entities;
