-- Give metadata relationship targets durable identities before field decisions
-- begin referring to them. All existing UUIDs and incoming references survive.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_metadata_identity_rows AS SELECT * FROM archive_entities;
DROP TABLE archive_entities;

CREATE TABLE archive_entities (
  uuid TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  kind TEXT NOT NULL CHECK (kind IN ('performer', 'scene', 'image', 'file', 'gallery', 'tag', 'studio', 'group')),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'redirected', 'deleted')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  performer_id INTEGER REFERENCES performers(id) ON UPDATE CASCADE,
  scene_id INTEGER REFERENCES scenes(id) ON UPDATE CASCADE,
  image_id INTEGER REFERENCES images(id) ON UPDATE CASCADE,
  file_id INTEGER REFERENCES files(id) ON UPDATE CASCADE,
  gallery_id INTEGER REFERENCES galleries(id) ON UPDATE CASCADE,
  tag_id INTEGER REFERENCES tags(id) ON UPDATE CASCADE,
  studio_id INTEGER REFERENCES studios(id) ON UPDATE CASCADE,
  group_id INTEGER REFERENCES groups(id) ON UPDATE CASCADE,
  original_id INTEGER CHECK (original_id > 0),
  redirect_to TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retired_at DATETIME,
  CHECK (redirect_to IS NULL OR redirect_to != uuid),
  CHECK (
    (state = 'active' AND redirect_to IS NULL AND retired_at IS NULL AND (
      (kind = 'performer' AND performer_id IS NOT NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL) OR
      (kind = 'scene' AND performer_id IS NULL AND scene_id IS NOT NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL) OR
      (kind = 'image' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NOT NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL) OR
      (kind = 'file' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NOT NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL) OR
      (kind = 'gallery' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NOT NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL) OR
      (kind = 'tag' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NOT NULL AND studio_id IS NULL AND group_id IS NULL) OR
      (kind = 'studio' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NOT NULL AND group_id IS NULL) OR
      (kind = 'group' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NOT NULL)
    )) OR
    (state IN ('redirected', 'deleted') AND retired_at IS NOT NULL
      AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL
      AND ((state = 'redirected' AND redirect_to IS NOT NULL) OR (state = 'deleted' AND redirect_to IS NULL)))
  )
);

INSERT INTO archive_entities(uuid, kind, state, revision, performer_id, scene_id, image_id, file_id, gallery_id, original_id, redirect_to, created_at, retired_at)
SELECT uuid, kind, state, revision, performer_id, scene_id, image_id, file_id, gallery_id, original_id, redirect_to, created_at, retired_at FROM native_metadata_identity_rows;
DROP TABLE native_metadata_identity_rows;
CREATE INDEX archive_entities_redirect ON archive_entities(redirect_to) WHERE redirect_to IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_performer ON archive_entities(performer_id) WHERE performer_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_scene ON archive_entities(scene_id) WHERE scene_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_image ON archive_entities(image_id) WHERE image_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_file ON archive_entities(file_id) WHERE file_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_gallery ON archive_entities(gallery_id) WHERE gallery_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_tag ON archive_entities(tag_id) WHERE tag_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_studio ON archive_entities(studio_id) WHERE studio_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_group ON archive_entities(group_id) WHERE group_id IS NOT NULL;

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

INSERT INTO archive_entities(kind, tag_id, original_id, created_at)
SELECT 'tag', id, id, created_at FROM tags;
CREATE TRIGGER archive_tag_created AFTER INSERT ON tags
BEGIN
  INSERT INTO archive_entities(kind, tag_id, original_id, created_at)
  VALUES ('tag', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_tag_changed AFTER UPDATE ON tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id = NEW.id; END;
CREATE TRIGGER archive_tag_deleted BEFORE DELETE ON tags
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    tag_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE tag_id = OLD.id;
END;
INSERT INTO archive_entities(kind, studio_id, original_id, created_at)
SELECT 'studio', id, id, created_at FROM studios;
CREATE TRIGGER archive_studio_created AFTER INSERT ON studios
BEGIN
  INSERT INTO archive_entities(kind, studio_id, original_id, created_at)
  VALUES ('studio', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_studio_changed AFTER UPDATE ON studios
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id = NEW.id; END;
CREATE TRIGGER archive_studio_deleted BEFORE DELETE ON studios
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    studio_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE studio_id = OLD.id;
END;
INSERT INTO archive_entities(kind, group_id, original_id, created_at)
SELECT 'group', id, id, created_at FROM groups;
CREATE TRIGGER archive_group_created AFTER INSERT ON groups
BEGIN
  INSERT INTO archive_entities(kind, group_id, original_id, created_at)
  VALUES ('group', NEW.id, NEW.id, NEW.created_at);
END;
CREATE TRIGGER archive_group_changed AFTER UPDATE ON groups
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id = NEW.id; END;
CREATE TRIGGER archive_group_deleted BEFORE DELETE ON groups
BEGIN
  UPDATE archive_entities SET state = 'deleted', revision = revision + 1,
    group_id = NULL, retired_at = CURRENT_TIMESTAMP WHERE group_id = OLD.id;
END;
CREATE TRIGGER archive_tag_tag_aliases_insert AFTER INSERT ON tag_aliases
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (NEW.tag_id); END;
CREATE TRIGGER archive_tag_tag_aliases_delete AFTER DELETE ON tag_aliases
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.tag_id); END;
CREATE TRIGGER archive_tag_tag_aliases_update AFTER UPDATE ON tag_aliases
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.tag_id, NEW.tag_id); END;
CREATE TRIGGER archive_tag_tag_stash_ids_insert AFTER INSERT ON tag_stash_ids
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (NEW.tag_id); END;
CREATE TRIGGER archive_tag_tag_stash_ids_delete AFTER DELETE ON tag_stash_ids
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.tag_id); END;
CREATE TRIGGER archive_tag_tag_stash_ids_update AFTER UPDATE ON tag_stash_ids
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.tag_id, NEW.tag_id); END;
CREATE TRIGGER archive_tag_tag_custom_fields_insert AFTER INSERT ON tag_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (NEW.tag_id); END;
CREATE TRIGGER archive_tag_tag_custom_fields_delete AFTER DELETE ON tag_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.tag_id); END;
CREATE TRIGGER archive_tag_tag_custom_fields_update AFTER UPDATE ON tag_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.tag_id, NEW.tag_id); END;
CREATE TRIGGER archive_tag_tags_relations_insert AFTER INSERT ON tags_relations
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (NEW.parent_id, NEW.child_id); END;
CREATE TRIGGER archive_tag_tags_relations_delete AFTER DELETE ON tags_relations
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.parent_id, OLD.child_id); END;
CREATE TRIGGER archive_tag_tags_relations_update AFTER UPDATE ON tags_relations
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE tag_id IN (OLD.parent_id, OLD.child_id, NEW.parent_id, NEW.child_id); END;
CREATE TRIGGER archive_studio_studio_aliases_insert AFTER INSERT ON studio_aliases
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_aliases_delete AFTER DELETE ON studio_aliases
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id); END;
CREATE TRIGGER archive_studio_studio_aliases_update AFTER UPDATE ON studio_aliases
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id, NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_urls_insert AFTER INSERT ON studio_urls
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_urls_delete AFTER DELETE ON studio_urls
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id); END;
CREATE TRIGGER archive_studio_studio_urls_update AFTER UPDATE ON studio_urls
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id, NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_stash_ids_insert AFTER INSERT ON studio_stash_ids
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_stash_ids_delete AFTER DELETE ON studio_stash_ids
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id); END;
CREATE TRIGGER archive_studio_studio_stash_ids_update AFTER UPDATE ON studio_stash_ids
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id, NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_custom_fields_insert AFTER INSERT ON studio_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (NEW.studio_id); END;
CREATE TRIGGER archive_studio_studio_custom_fields_delete AFTER DELETE ON studio_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id); END;
CREATE TRIGGER archive_studio_studio_custom_fields_update AFTER UPDATE ON studio_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id, NEW.studio_id); END;
CREATE TRIGGER archive_studio_studios_tags_insert AFTER INSERT ON studios_tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (NEW.studio_id); END;
CREATE TRIGGER archive_studio_studios_tags_delete AFTER DELETE ON studios_tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id); END;
CREATE TRIGGER archive_studio_studios_tags_update AFTER UPDATE ON studios_tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE studio_id IN (OLD.studio_id, NEW.studio_id); END;
CREATE TRIGGER archive_group_group_urls_insert AFTER INSERT ON group_urls
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (NEW.group_id); END;
CREATE TRIGGER archive_group_group_urls_delete AFTER DELETE ON group_urls
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.group_id); END;
CREATE TRIGGER archive_group_group_urls_update AFTER UPDATE ON group_urls
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.group_id, NEW.group_id); END;
CREATE TRIGGER archive_group_group_custom_fields_insert AFTER INSERT ON group_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (NEW.group_id); END;
CREATE TRIGGER archive_group_group_custom_fields_delete AFTER DELETE ON group_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.group_id); END;
CREATE TRIGGER archive_group_group_custom_fields_update AFTER UPDATE ON group_custom_fields
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.group_id, NEW.group_id); END;
CREATE TRIGGER archive_group_groups_tags_insert AFTER INSERT ON groups_tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (NEW.group_id); END;
CREATE TRIGGER archive_group_groups_tags_delete AFTER DELETE ON groups_tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.group_id); END;
CREATE TRIGGER archive_group_groups_tags_update AFTER UPDATE ON groups_tags
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.group_id, NEW.group_id); END;
CREATE TRIGGER archive_group_groups_relations_insert AFTER INSERT ON groups_relations
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (NEW.containing_id, NEW.sub_id); END;
CREATE TRIGGER archive_group_groups_relations_delete AFTER DELETE ON groups_relations
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.containing_id, OLD.sub_id); END;
CREATE TRIGGER archive_group_groups_relations_update AFTER UPDATE ON groups_relations
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE group_id IN (OLD.containing_id, OLD.sub_id, NEW.containing_id, NEW.sub_id); END;

INSERT INTO native_migration_history(version, name, details)
SELECT 1000011, 'Portable metadata relationship identities', json_object('tags', (SELECT count(*) FROM tags), 'studios', (SELECT count(*) FROM studios), 'groups', (SELECT count(*) FROM groups));
