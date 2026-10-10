-- A GIF image and its converted video retain distinct, typed identities. Only
-- this explicit conversion record permits an image identity to resolve to a
-- scene; ordinary merges still require the same kind and never assert that
-- converted bytes are equal to the original bytes.
CREATE TABLE media_conversions (
  uuid TEXT PRIMARY KEY,
  image_uuid TEXT NOT NULL UNIQUE REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  scene_uuid TEXT NOT NULL UNIQUE REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  original_file_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  original_generation INTEGER NOT NULL CHECK(original_generation>0),
  file_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  generation INTEGER NOT NULL CHECK(generation>0),
  photographer TEXT NOT NULL,
  undated_o_count INTEGER NOT NULL CHECK(undated_o_count>=0),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK(image_uuid!=scene_uuid AND original_file_uuid!=file_uuid),
  FOREIGN KEY(file_uuid,generation) REFERENCES file_content_versions(file_uuid,generation)
) WITHOUT ROWID;

CREATE TRIGGER media_conversion_scope BEFORE INSERT ON media_conversions
BEGIN
  SELECT RAISE(ABORT,'conversion requires current image and scene identities') WHERE NOT EXISTS (
    SELECT 1 FROM archive_entities i JOIN archive_entities s
    WHERE i.uuid=NEW.image_uuid AND i.kind='image' AND i.state='active'
      AND s.uuid=NEW.scene_uuid AND s.kind='scene' AND s.state='active'
  );
  SELECT RAISE(ABORT,'conversion requires current original and verified final files') WHERE NOT EXISTS (
    SELECT 1 FROM archive_entities a JOIN files f ON f.id=a.file_id
    JOIN archive_entities b JOIN files v ON v.id=b.file_id
    JOIN images_files old ON old.file_id=f.id JOIN archive_entities i ON i.image_id=old.image_id
    JOIN scenes_files new ON new.file_id=v.id JOIN archive_entities s ON s.scene_id=new.scene_id
    WHERE a.uuid=NEW.original_file_uuid AND a.kind='file' AND a.state='active' AND f.generation=NEW.original_generation
      AND b.uuid=NEW.file_uuid AND b.kind='file' AND b.state='active' AND v.generation=NEW.generation
      AND i.uuid=NEW.image_uuid AND s.uuid=NEW.scene_uuid
  );
END;
CREATE TRIGGER media_conversion_immutable BEFORE UPDATE ON media_conversions
WHEN NEW.uuid!=OLD.uuid
  OR (NEW.image_uuid!=OLD.image_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.image_uuid))
  OR (NEW.scene_uuid!=OLD.scene_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.scene_uuid))
  OR (NEW.original_file_uuid!=OLD.original_file_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.original_file_uuid))
  OR NEW.original_generation!=OLD.original_generation
  OR (NEW.file_uuid!=OLD.file_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.file_uuid))
  OR NEW.generation!=OLD.generation OR NEW.photographer!=OLD.photographer
  OR NEW.undated_o_count!=OLD.undated_o_count OR NEW.created_at!=OLD.created_at
BEGIN SELECT RAISE(ABORT,'media conversion is immutable'); END;
CREATE TRIGGER media_conversion_retained BEFORE DELETE ON media_conversions
BEGIN SELECT RAISE(ABORT,'media conversion is retained'); END;

DROP TRIGGER archive_entity_redirect_update;
CREATE TRIGGER archive_entity_redirect_update BEFORE UPDATE OF redirect_to, kind ON archive_entities
WHEN NEW.redirect_to IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'archive identity redirect kinds differ')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid=NEW.redirect_to AND kind!=NEW.kind)
      AND NOT EXISTS (SELECT 1 FROM media_conversions WHERE image_uuid=NEW.uuid AND NEW.kind='image'
        AND (scene_uuid=NEW.redirect_to OR (scene_uuid=OLD.redirect_to AND NOT EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.redirect_to))));
  SELECT RAISE(ABORT, 'archive identity redirect cycle') WHERE EXISTS (
    WITH RECURSIVE chain(uuid,redirect_to) AS (
      SELECT uuid,redirect_to FROM archive_entities WHERE uuid=NEW.redirect_to
      UNION SELECT e.uuid,e.redirect_to FROM archive_entities e JOIN chain c ON e.uuid=c.redirect_to
    ) SELECT 1 FROM chain WHERE uuid=NEW.uuid
  );
END;

-- Replacing the physical gallery member carries its existing membership
-- intent through the redirect. It must not create a new manual inclusion that
-- would override the source's future membership decisions.
DROP TRIGGER source_gallery_scenes_galleries_insert;
CREATE TRIGGER source_gallery_scenes_galleries_insert BEFORE INSERT ON scenes_galleries
WHEN NOT EXISTS (
  SELECT 1 FROM media_conversions c
  JOIN archive_entities i ON i.uuid=c.image_uuid AND i.state='active'
  JOIN archive_entities s ON s.uuid=c.scene_uuid
  JOIN galleries_images gi ON gi.image_id=i.image_id
  WHERE s.scene_id=NEW.scene_id AND gi.gallery_id=NEW.gallery_id
)
BEGIN
  INSERT INTO gallery_membership_events(uuid,gallery_uuid,media_uuid,state,origin,post_uuid,selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab',(random() & 3)+1,1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6)))),g.uuid,m.uuid,'included',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END,w.post_uuid,w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id=g.gallery_id
  JOIN archive_entities m ON m.scene_id=NEW.scene_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid=g.uuid
  WHERE g.gallery_id=NEW.gallery_id AND
    (ga.origin='source' OR EXISTS(SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid=g.uuid));
END;

-- Image counters have no timestamps. Preserve the count without inventing
-- dates. Dated-event queries exclude these NULLs; count/reset include them.
CREATE TABLE scenes_o_dates_converted (
  scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
  o_date DATETIME
);
INSERT INTO scenes_o_dates_converted SELECT scene_id,o_date FROM scenes_o_dates;
DROP TABLE scenes_o_dates;
ALTER TABLE scenes_o_dates_converted RENAME TO scenes_o_dates;
CREATE INDEX index_scenes_o_dates ON scenes_o_dates(scene_id);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000109,'Verified image to video conversion with retained identities and undated counts','{}');
