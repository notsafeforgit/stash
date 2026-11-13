-- Retain current field choices, not an edit log. Historical file-review receipts
-- still pin the exact decisions they reviewed; these are transaction evidence,
-- not a general field-history retention policy.
CREATE INDEX metadata_file_edit_keeps_decision ON metadata_file_edit_keeps(selected_decision_uuid)
WHERE selected_decision_uuid IS NOT NULL;

-- Delete dependent value/provenance rows with an obsolete choice. The foreign
-- keys still reject deletion of a selected choice or a reviewed receipt's proof.
CREATE TRIGGER metadata_field_decision_cleanup BEFORE DELETE ON metadata_field_decisions
BEGIN
  DELETE FROM metadata_field_references WHERE decision_uuid=OLD.uuid;
  DELETE FROM metadata_decision_policies WHERE decision_uuid=OLD.uuid;
  DELETE FROM metadata_decision_post_media WHERE decision_uuid=OLD.uuid;
END;
CREATE TRIGGER metadata_field_retire_superseded AFTER UPDATE OF decision_uuid ON metadata_field_heads
WHEN NEW.decision_uuid!=OLD.decision_uuid
BEGIN
  DELETE FROM metadata_field_decisions WHERE uuid=OLD.decision_uuid
    AND NOT EXISTS(SELECT 1 FROM metadata_file_edit_reviews WHERE decision_uuid=OLD.decision_uuid)
    AND NOT EXISTS(SELECT 1 FROM metadata_file_edit_keeps WHERE selected_decision_uuid=OLD.decision_uuid);
END;

-- Indexed dependent lookups keep the one-time cleanup linear in existing
-- choices. Current heads, UUIDs, revisions, source captures and receipts survive.
DELETE FROM metadata_field_decisions AS d
WHERE d.sealed=1
  AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h
    WHERE h.entity_uuid=d.entity_uuid AND h.field=d.field AND h.decision_uuid=d.uuid)
  AND NOT EXISTS(SELECT 1 FROM metadata_file_edit_reviews r WHERE r.decision_uuid=d.uuid)
  AND NOT EXISTS(SELECT 1 FROM metadata_file_edit_keeps k WHERE k.selected_decision_uuid=d.uuid);
INSERT INTO native_migration_history(version,name,details)
VALUES(1000141,'Keep current metadata choices without routine value history',
  json_object('retired_field_choices',changes()));

DROP TRIGGER metadata_scene_title_library;
CREATE TRIGGER metadata_scene_title_library AFTER UPDATE OF title ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='title')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='title'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.title, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'title', CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.title, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_code_library;
CREATE TRIGGER metadata_scene_code_library AFTER UPDATE OF code ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='code')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='code'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.code, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'code', CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.code, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_details_library;
CREATE TRIGGER metadata_scene_details_library AFTER UPDATE OF details ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='details')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='details'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.details, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'details', CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.details, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_date_library;
CREATE TRIGGER metadata_scene_date_library AFTER UPDATE OF date, date_precision ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='date')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='date'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'date', CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_rating100_library;
CREATE TRIGGER metadata_scene_rating100_library AFTER UPDATE OF rating ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='rating100')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='rating100'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(NEW.rating) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'rating100', CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(NEW.rating)
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_organized_library;
CREATE TRIGGER metadata_scene_organized_library AFTER UPDATE OF organized ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='organized')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='organized'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode='set' AND d.origin='library'
    AND d.value_json=CASE WHEN NEW.organized THEN 'true' ELSE 'false' END AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'organized', 'set', 'library', CASE WHEN NEW.organized THEN 'true' ELSE 'false' END
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_director_library;
CREATE TRIGGER metadata_scene_director_library AFTER UPDATE OF director ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='director')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='director'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.director, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.director, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'director', CASE WHEN json_quote(coalesce(NEW.director, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.director, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_scene_production_date_library;
CREATE TRIGGER metadata_scene_production_date_library AFTER UPDATE OF production_date, production_date_precision ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='production_date')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='production_date'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.scene_id=NEW.id AND d.mode=CASE WHEN json_quote(CASE WHEN NEW.production_date IS NULL THEN NULL ELSE substr(NEW.production_date, 1, CASE NEW.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(CASE WHEN NEW.production_date IS NULL THEN NULL ELSE substr(NEW.production_date, 1, CASE NEW.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'production_date', CASE WHEN json_quote(CASE WHEN NEW.production_date IS NULL THEN NULL ELSE substr(NEW.production_date, 1, CASE NEW.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.production_date IS NULL THEN NULL ELSE substr(NEW.production_date, 1, CASE NEW.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE scene_id=NEW.id;
END;

DROP TRIGGER metadata_image_title_library;
CREATE TRIGGER metadata_image_title_library AFTER UPDATE OF title ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='title')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='title'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.title, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'title', CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.title, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_image_code_library;
CREATE TRIGGER metadata_image_code_library AFTER UPDATE OF code ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='code')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='code'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.code, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'code', CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.code, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_image_details_library;
CREATE TRIGGER metadata_image_details_library AFTER UPDATE OF details ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='details')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='details'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.details, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'details', CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.details, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_image_date_library;
CREATE TRIGGER metadata_image_date_library AFTER UPDATE OF date, date_precision ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='date')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='date'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode=CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'date', CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_image_rating100_library;
CREATE TRIGGER metadata_image_rating100_library AFTER UPDATE OF rating ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='rating100')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='rating100'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode=CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(NEW.rating) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'rating100', CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(NEW.rating)
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_image_organized_library;
CREATE TRIGGER metadata_image_organized_library AFTER UPDATE OF organized ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='organized')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='organized'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode='set' AND d.origin='library'
    AND d.value_json=CASE WHEN NEW.organized THEN 'true' ELSE 'false' END AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'organized', 'set', 'library', CASE WHEN NEW.organized THEN 'true' ELSE 'false' END
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_image_photographer_library;
CREATE TRIGGER metadata_image_photographer_library AFTER UPDATE OF photographer ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='photographer')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='photographer'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.image_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.photographer, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.photographer, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'photographer', CASE WHEN json_quote(coalesce(NEW.photographer, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.photographer, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_title_library;
CREATE TRIGGER metadata_gallery_title_library AFTER UPDATE OF title ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='title')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='title'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.title, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'title', CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.title, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_code_library;
CREATE TRIGGER metadata_gallery_code_library AFTER UPDATE OF code ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='code')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='code'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.code, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'code', CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.code, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_details_library;
CREATE TRIGGER metadata_gallery_details_library AFTER UPDATE OF details ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='details')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='details'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.details, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'details', CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.details, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_date_library;
CREATE TRIGGER metadata_gallery_date_library AFTER UPDATE OF date, date_precision ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='date')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='date'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode=CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'date', CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_rating100_library;
CREATE TRIGGER metadata_gallery_rating100_library AFTER UPDATE OF rating ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='rating100')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='rating100'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode=CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(NEW.rating) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'rating100', CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(NEW.rating)
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_organized_library;
CREATE TRIGGER metadata_gallery_organized_library AFTER UPDATE OF organized ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='organized')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='organized'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode='set' AND d.origin='library'
    AND d.value_json=CASE WHEN NEW.organized THEN 'true' ELSE 'false' END AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'organized', 'set', 'library', CASE WHEN NEW.organized THEN 'true' ELSE 'false' END
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

DROP TRIGGER metadata_gallery_photographer_library;
CREATE TRIGGER metadata_gallery_photographer_library AFTER UPDATE OF photographer ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='photographer')
AND NOT EXISTS(SELECT 1 FROM archive_entities a
  JOIN metadata_field_heads h ON h.entity_uuid=a.uuid AND h.field='photographer'
  JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid
  WHERE a.gallery_id=NEW.id AND d.mode=CASE WHEN json_quote(coalesce(NEW.photographer, ''))='""' THEN 'clear' ELSE 'set' END AND d.origin='library'
    AND d.value_json=json_quote(coalesce(NEW.photographer, '')) AND d.capture_uuid IS NULL AND d.reason='')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'photographer', CASE WHEN json_quote(coalesce(NEW.photographer, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.photographer, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;
