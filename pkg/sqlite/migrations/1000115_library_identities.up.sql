-- Complete durable identities for all library-facing records. Existing UUIDs,
-- redirects, local keys and incoming foreign-key references remain unchanged.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_library_identity_rows AS SELECT * FROM archive_entities;
CREATE TABLE native_library_identity_triggers AS SELECT sql FROM sqlite_schema WHERE type='trigger' AND tbl_name='archive_entities';
DROP TABLE archive_entities;

CREATE TABLE archive_entities (
  uuid TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  kind TEXT NOT NULL CHECK (kind IN ('performer', 'scene', 'image', 'file', 'gallery', 'tag', 'studio', 'group', 'folder', 'scene_marker', 'gallery_chapter', 'saved_filter')),
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
  folder_id INTEGER REFERENCES folders(id) ON UPDATE CASCADE,
  scene_marker_id INTEGER REFERENCES scene_markers(id) ON UPDATE CASCADE,
  gallery_chapter_id INTEGER REFERENCES galleries_chapters(id) ON UPDATE CASCADE,
  saved_filter_id INTEGER REFERENCES saved_filters(id) ON UPDATE CASCADE,
  original_id INTEGER CHECK (original_id > 0),
  redirect_to TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retired_at DATETIME,
  CHECK (redirect_to IS NULL OR redirect_to != uuid),
  CHECK (
    (state = 'active' AND redirect_to IS NULL AND retired_at IS NULL AND (
      (kind = 'performer' AND performer_id IS NOT NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'scene' AND performer_id IS NULL AND scene_id IS NOT NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'image' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NOT NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'file' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NOT NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'gallery' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NOT NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'tag' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NOT NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'studio' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NOT NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'group' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NOT NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'folder' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NOT NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'scene_marker' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NOT NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL) OR
      (kind = 'gallery_chapter' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NOT NULL AND saved_filter_id IS NULL) OR
      (kind = 'saved_filter' AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NOT NULL)
    )) OR
    (state IN ('redirected', 'deleted') AND retired_at IS NOT NULL
      AND performer_id IS NULL AND scene_id IS NULL AND image_id IS NULL AND file_id IS NULL AND gallery_id IS NULL AND tag_id IS NULL AND studio_id IS NULL AND group_id IS NULL AND folder_id IS NULL AND scene_marker_id IS NULL AND gallery_chapter_id IS NULL AND saved_filter_id IS NULL
      AND ((state = 'redirected' AND redirect_to IS NOT NULL) OR (state = 'deleted' AND redirect_to IS NULL)))
  )
);

INSERT INTO archive_entities(uuid,kind,state,revision,performer_id,scene_id,image_id,file_id,gallery_id,tag_id,studio_id,group_id,original_id,redirect_to,created_at,retired_at) SELECT uuid,kind,state,revision,performer_id,scene_id,image_id,file_id,gallery_id,tag_id,studio_id,group_id,original_id,redirect_to,created_at,retired_at FROM native_library_identity_rows;
DROP TABLE native_library_identity_rows;
CREATE INDEX archive_entities_redirect ON archive_entities(redirect_to) WHERE redirect_to IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_performer ON archive_entities(performer_id) WHERE performer_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_scene ON archive_entities(scene_id) WHERE scene_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_image ON archive_entities(image_id) WHERE image_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_file ON archive_entities(file_id) WHERE file_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_gallery ON archive_entities(gallery_id) WHERE gallery_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_tag ON archive_entities(tag_id) WHERE tag_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_studio ON archive_entities(studio_id) WHERE studio_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_group ON archive_entities(group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_folder ON archive_entities(folder_id) WHERE folder_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_scene_marker ON archive_entities(scene_marker_id) WHERE scene_marker_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_gallery_chapter ON archive_entities(gallery_chapter_id) WHERE gallery_chapter_id IS NOT NULL;
CREATE UNIQUE INDEX archive_entities_saved_filter ON archive_entities(saved_filter_id) WHERE saved_filter_id IS NOT NULL;

INSERT INTO archive_entities(kind,folder_id,original_id,created_at)
SELECT 'folder',id,id,created_at FROM folders;
CREATE TRIGGER archive_folder_created AFTER INSERT ON folders
BEGIN
  INSERT INTO archive_entities(kind,folder_id,original_id,created_at)
  VALUES ('folder',NEW.id,NEW.id,NEW.created_at);
END;
CREATE TRIGGER archive_folder_changed AFTER UPDATE ON folders
BEGIN UPDATE archive_entities SET revision=revision+1 WHERE folder_id=NEW.id; END;
CREATE TRIGGER archive_folder_deleted BEFORE DELETE ON folders
BEGIN
  UPDATE archive_entities SET state='deleted',revision=revision+1,
    folder_id=NULL,retired_at=CURRENT_TIMESTAMP WHERE folder_id=OLD.id;
END;

INSERT INTO archive_entities(kind,scene_marker_id,original_id,created_at)
SELECT 'scene_marker',id,id,created_at FROM scene_markers;
CREATE TRIGGER archive_scene_marker_created AFTER INSERT ON scene_markers
BEGIN
  INSERT INTO archive_entities(kind,scene_marker_id,original_id,created_at)
  VALUES ('scene_marker',NEW.id,NEW.id,NEW.created_at);
END;
CREATE TRIGGER archive_scene_marker_changed AFTER UPDATE ON scene_markers
BEGIN UPDATE archive_entities SET revision=revision+1 WHERE scene_marker_id=NEW.id; END;
CREATE TRIGGER archive_scene_marker_deleted BEFORE DELETE ON scene_markers
BEGIN
  UPDATE archive_entities SET state='deleted',revision=revision+1,
    scene_marker_id=NULL,retired_at=CURRENT_TIMESTAMP WHERE scene_marker_id=OLD.id;
END;

INSERT INTO archive_entities(kind,gallery_chapter_id,original_id,created_at)
SELECT 'gallery_chapter',id,id,created_at FROM galleries_chapters;
CREATE TRIGGER archive_gallery_chapter_created AFTER INSERT ON galleries_chapters
BEGIN
  INSERT INTO archive_entities(kind,gallery_chapter_id,original_id,created_at)
  VALUES ('gallery_chapter',NEW.id,NEW.id,NEW.created_at);
END;
CREATE TRIGGER archive_gallery_chapter_changed AFTER UPDATE ON galleries_chapters
BEGIN UPDATE archive_entities SET revision=revision+1 WHERE gallery_chapter_id=NEW.id; END;
CREATE TRIGGER archive_gallery_chapter_deleted BEFORE DELETE ON galleries_chapters
BEGIN
  UPDATE archive_entities SET state='deleted',revision=revision+1,
    gallery_chapter_id=NULL,retired_at=CURRENT_TIMESTAMP WHERE gallery_chapter_id=OLD.id;
END;

INSERT INTO archive_entities(kind,saved_filter_id,original_id,created_at)
SELECT 'saved_filter',id,id,CURRENT_TIMESTAMP FROM saved_filters;
CREATE TRIGGER archive_saved_filter_created AFTER INSERT ON saved_filters
BEGIN
  INSERT INTO archive_entities(kind,saved_filter_id,original_id,created_at)
  VALUES ('saved_filter',NEW.id,NEW.id,CURRENT_TIMESTAMP);
END;
CREATE TRIGGER archive_saved_filter_changed AFTER UPDATE ON saved_filters
BEGIN UPDATE archive_entities SET revision=revision+1 WHERE saved_filter_id=NEW.id; END;
CREATE TRIGGER archive_saved_filter_deleted BEFORE DELETE ON saved_filters
BEGIN
  UPDATE archive_entities SET state='deleted',revision=revision+1,
    saved_filter_id=NULL,retired_at=CURRENT_TIMESTAMP WHERE saved_filter_id=OLD.id;
END;

ALTER TABLE scene_cover_sources ADD COLUMN source_file_uuid TEXT REFERENCES archive_entities(uuid);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000115,'Complete public library identities','{}');
