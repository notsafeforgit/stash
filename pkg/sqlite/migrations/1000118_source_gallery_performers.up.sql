-- Transaction-local work set, emptied by Database.Commit. Only affected
-- source galleries are recomputed, once per transaction, even for bulk edits.
-- Filter existing rows explicitly: an outer metadata UPSERT can override a
-- nested trigger INSERT OR IGNORE conflict policy.
CREATE TABLE source_gallery_performer_pending (
  gallery_id INTEGER PRIMARY KEY REFERENCES galleries(id) ON DELETE CASCADE
);

CREATE TRIGGER source_gallery_performers_image_insert AFTER INSERT ON performers_images
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM galleries_images m JOIN galleries g ON g.id=m.gallery_id
  WHERE m.image_id=NEW.image_id AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_image_delete AFTER DELETE ON performers_images
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM galleries_images m JOIN galleries g ON g.id=m.gallery_id
  WHERE m.image_id=OLD.image_id AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_image_update AFTER UPDATE ON performers_images
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM galleries_images m JOIN galleries g ON g.id=m.gallery_id
  WHERE m.image_id IN (OLD.image_id,NEW.image_id) AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_scene_insert AFTER INSERT ON performers_scenes
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM scenes_galleries m JOIN galleries g ON g.id=m.gallery_id
  WHERE m.scene_id=NEW.scene_id AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_scene_delete AFTER DELETE ON performers_scenes
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM scenes_galleries m JOIN galleries g ON g.id=m.gallery_id
  WHERE m.scene_id=OLD.scene_id AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_scene_update AFTER UPDATE ON performers_scenes
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM scenes_galleries m JOIN galleries g ON g.id=m.gallery_id
  WHERE m.scene_id IN (OLD.scene_id,NEW.scene_id) AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_image_member_insert AFTER INSERT ON galleries_images
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT id FROM galleries WHERE id=NEW.gallery_id AND origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=galleries.id);
END;
CREATE TRIGGER source_gallery_performers_image_member_delete AFTER DELETE ON galleries_images
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT id FROM galleries WHERE id=OLD.gallery_id AND origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=galleries.id);
END;
CREATE TRIGGER source_gallery_performers_image_member_update AFTER UPDATE ON galleries_images
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT id FROM galleries WHERE id IN (OLD.gallery_id,NEW.gallery_id) AND origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=galleries.id);
END;
CREATE TRIGGER source_gallery_performers_scene_member_insert AFTER INSERT ON scenes_galleries
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT id FROM galleries WHERE id=NEW.gallery_id AND origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=galleries.id);
END;
CREATE TRIGGER source_gallery_performers_scene_member_delete AFTER DELETE ON scenes_galleries
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT id FROM galleries WHERE id=OLD.gallery_id AND origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=galleries.id);
END;
CREATE TRIGGER source_gallery_performers_scene_member_update AFTER UPDATE ON scenes_galleries
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT id FROM galleries WHERE id IN (OLD.gallery_id,NEW.gallery_id) AND origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=galleries.id);
END;
CREATE TRIGGER source_gallery_performers_origin_update AFTER UPDATE OF origin ON galleries
WHEN NEW.origin='source' AND OLD.origin IS NOT NEW.origin
BEGIN
  INSERT INTO source_gallery_performer_pending SELECT NEW.id
  WHERE NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending WHERE gallery_id=NEW.id);
END;
-- Re-enabling inheritance on an explicitly edited gallery should take effect
-- immediately, without waiting for another member edit or scrape.
CREATE TRIGGER source_gallery_performers_choice_insert AFTER INSERT ON metadata_field_heads
WHEN NEW.field='performers'
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM archive_entities a JOIN galleries g ON g.id=a.gallery_id
  WHERE a.uuid=NEW.entity_uuid AND a.state='active' AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_performers_choice_update AFTER UPDATE OF decision_uuid ON metadata_field_heads
WHEN NEW.field='performers'
BEGIN
  INSERT INTO source_gallery_performer_pending
  SELECT DISTINCT g.id FROM archive_entities a JOIN galleries g ON g.id=a.gallery_id
  WHERE a.uuid=NEW.entity_uuid AND a.state='active' AND g.origin='source'
    AND NOT EXISTS(SELECT 1 FROM source_gallery_performer_pending q WHERE q.gallery_id=g.id);
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000118,'Source galleries inherit member performers','{}');
