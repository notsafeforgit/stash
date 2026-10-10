-- One selected cover can refer to either kind of gallery member. Copy existing
-- choices before installing change notifications, preserving their revisions.
CREATE TABLE gallery_covers (
  gallery_id INTEGER PRIMARY KEY REFERENCES galleries(id) ON DELETE CASCADE,
  media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE
);
CREATE INDEX gallery_covers_media ON gallery_covers(media_uuid);
INSERT INTO gallery_covers(gallery_id, media_uuid)
SELECT gi.gallery_id, a.uuid FROM galleries_images gi
JOIN archive_entities a ON a.image_id = gi.image_id WHERE gi.cover = 1;

DROP INDEX index_galleries_images_gallery_id_cover;
ALTER TABLE galleries_images DROP COLUMN cover;

CREATE TRIGGER gallery_cover_scope_insert BEFORE INSERT ON gallery_covers
WHEN NOT EXISTS (
  SELECT 1 FROM archive_entities a WHERE a.uuid = NEW.media_uuid AND a.state = 'active'
  AND ((a.kind = 'image' AND EXISTS (SELECT 1 FROM galleries_images gi WHERE gi.gallery_id = NEW.gallery_id AND gi.image_id = a.image_id))
    OR (a.kind = 'scene' AND EXISTS (SELECT 1 FROM scenes_galleries gs WHERE gs.gallery_id = NEW.gallery_id AND gs.scene_id = a.scene_id)))
)
BEGIN SELECT RAISE(ABORT, 'gallery cover must be an active gallery member'); END;
CREATE TRIGGER gallery_cover_scope_update BEFORE UPDATE ON gallery_covers
WHEN NOT EXISTS (
  SELECT 1 FROM archive_entities a WHERE a.uuid = NEW.media_uuid AND a.state = 'active'
  AND ((a.kind = 'image' AND EXISTS (SELECT 1 FROM galleries_images gi WHERE gi.gallery_id = NEW.gallery_id AND gi.image_id = a.image_id))
    OR (a.kind = 'scene' AND EXISTS (SELECT 1 FROM scenes_galleries gs WHERE gs.gallery_id = NEW.gallery_id AND gs.scene_id = a.scene_id)))
)
BEGIN SELECT RAISE(ABORT, 'gallery cover must be an active gallery member'); END;

CREATE TRIGGER gallery_cover_image_removed AFTER DELETE ON galleries_images
BEGIN
  DELETE FROM gallery_covers WHERE gallery_id = OLD.gallery_id
    AND media_uuid IN (SELECT uuid FROM archive_entities WHERE image_id = OLD.image_id);
END;
CREATE TRIGGER gallery_cover_scene_removed AFTER DELETE ON scenes_galleries
BEGIN
  DELETE FROM gallery_covers WHERE gallery_id = OLD.gallery_id
    AND media_uuid IN (SELECT uuid FROM archive_entities WHERE scene_id = OLD.scene_id);
END;
CREATE TRIGGER gallery_cover_image_replaced AFTER UPDATE OF gallery_id, image_id ON galleries_images
WHEN OLD.gallery_id != NEW.gallery_id OR OLD.image_id != NEW.image_id
BEGIN
  DELETE FROM gallery_covers WHERE gallery_id = OLD.gallery_id
    AND media_uuid IN (SELECT uuid FROM archive_entities WHERE image_id = OLD.image_id);
END;
CREATE TRIGGER gallery_cover_scene_replaced AFTER UPDATE OF gallery_id, scene_id ON scenes_galleries
WHEN OLD.gallery_id != NEW.gallery_id OR OLD.scene_id != NEW.scene_id
BEGIN
  DELETE FROM gallery_covers WHERE gallery_id = OLD.gallery_id
    AND media_uuid IN (SELECT uuid FROM archive_entities WHERE scene_id = OLD.scene_id);
END;
CREATE TRIGGER gallery_cover_media_retired AFTER UPDATE OF state ON archive_entities
WHEN NEW.state != 'active'
BEGIN
  -- A same-kind merge can retain the selected cover when its destination is
  -- also a member. An explicit membership removal must still clear it.
  UPDATE gallery_covers SET media_uuid = NEW.redirect_to
  WHERE media_uuid = NEW.uuid AND NEW.state = 'redirected' AND EXISTS (
    SELECT 1 FROM archive_entities a WHERE a.uuid = NEW.redirect_to AND a.state = 'active'
      AND ((a.kind = 'image' AND EXISTS (SELECT 1 FROM galleries_images gi WHERE gi.gallery_id = gallery_covers.gallery_id AND gi.image_id = a.image_id))
        OR (a.kind = 'scene' AND EXISTS (SELECT 1 FROM scenes_galleries gs WHERE gs.gallery_id = gallery_covers.gallery_id AND gs.scene_id = a.scene_id)))
  );
  DELETE FROM gallery_covers WHERE media_uuid = NEW.uuid;
END;

CREATE TRIGGER gallery_cover_changed_insert AFTER INSERT ON gallery_covers
BEGIN UPDATE galleries SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.gallery_id; END;
CREATE TRIGGER gallery_cover_changed_update AFTER UPDATE ON gallery_covers
BEGIN UPDATE galleries SET updated_at = CURRENT_TIMESTAMP WHERE id IN (OLD.gallery_id, NEW.gallery_id); END;
CREATE TRIGGER gallery_cover_changed_delete AFTER DELETE ON gallery_covers
BEGIN UPDATE galleries SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.gallery_id; END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000108,'Gallery covers selected from image or scene members','{}');
