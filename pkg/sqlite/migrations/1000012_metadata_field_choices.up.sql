-- Keep current library values in place. A single baseline row protects all
-- existing fields; their prior values enter history lazily on the first edit.
CREATE TABLE metadata_field_baselines (
  entity_uuid TEXT NOT NULL PRIMARY KEY REFERENCES archive_entities(uuid) ON UPDATE CASCADE
) WITHOUT ROWID;
INSERT INTO metadata_field_baselines(entity_uuid)
SELECT uuid FROM archive_entities WHERE kind IN ('scene','image','gallery') AND state='active';
CREATE TABLE metadata_field_decisions (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid TEXT NOT NULL UNIQUE DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))
    CHECK (length(uuid)=36 AND uuid=lower(uuid)
      AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
      AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
      AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
      AND uuid!='00000000-0000-0000-0000-000000000000'),
  entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  field TEXT NOT NULL CHECK (field IN ('title','code','details','date','rating100','organized','director','production_date','photographer')),
  mode TEXT NOT NULL CHECK (mode IN ('set','clear','inherit','preserved')),
  origin TEXT NOT NULL CHECK (origin IN ('library','review','migration','legacy','unattributed','source','policy','filename')),
  value_json TEXT NOT NULL CHECK (length(CAST(value_json AS BLOB)) <= 4194304 AND json_valid(value_json)),
  capture_uuid TEXT REFERENCES source_captures(uuid),
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (entity_uuid, field, uuid),
  CHECK (mode!='preserved' OR origin IN ('legacy','unattributed','migration')),
  CHECK (origin NOT IN ('legacy','unattributed') OR mode='preserved'),
  CHECK (origin NOT IN ('source','policy','filename') OR mode='inherit'),
  CHECK (origin!='library' OR mode IN ('set','clear')),
  CHECK (origin!='source' OR capture_uuid IS NOT NULL),
  CHECK (origin!='filename' OR (field='title' AND capture_uuid IS NULL)),
  CHECK (CASE
    WHEN field IN ('date','production_date') THEN json_type(value_json) IN ('null','text')
    WHEN field='rating100' THEN json_type(value_json) IN ('null','integer')
    WHEN field='organized' THEN json_type(value_json) IN ('true','false')
    ELSE json_type(value_json)='text' END),
  CHECK (mode!='clear' OR CASE
    WHEN field IN ('date','production_date','rating100') THEN json_type(value_json)='null'
    WHEN field='organized' THEN json_type(value_json)='false'
    ELSE json_extract(value_json,'$')='' END)
);
CREATE INDEX metadata_field_decisions_entity ON metadata_field_decisions(entity_uuid, field, sequence);
CREATE INDEX metadata_field_decisions_capture ON metadata_field_decisions(capture_uuid) WHERE capture_uuid IS NOT NULL;
CREATE TABLE metadata_field_heads (
  entity_uuid TEXT NOT NULL,
  field TEXT NOT NULL,
  decision_uuid TEXT NOT NULL,
  PRIMARY KEY (entity_uuid, field),
  FOREIGN KEY (entity_uuid, field, decision_uuid) REFERENCES metadata_field_decisions(entity_uuid, field, uuid) ON UPDATE CASCADE
) WITHOUT ROWID;
CREATE TABLE metadata_field_write_context (
  entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  field TEXT NOT NULL CHECK (field IN ('title','code','details','date','rating100','organized','director','production_date','photographer')),
  PRIMARY KEY (entity_uuid, field)
) WITHOUT ROWID;
CREATE TRIGGER metadata_field_decision_scope BEFORE INSERT ON metadata_field_decisions
BEGIN
  SELECT RAISE(ABORT, 'invalid metadata field entity or target') WHERE NOT EXISTS (
    SELECT 1 FROM archive_entities a WHERE a.uuid=NEW.entity_uuid AND a.state='active'
    AND a.kind IN ('scene','image','gallery')
    AND (NEW.field NOT IN ('director','production_date') OR a.kind='scene')
    AND (NEW.field!='photographer' OR a.kind IN ('image','gallery'))
  );
  SELECT RAISE(ABORT, 'metadata choice requires an active source capture') WHERE NEW.capture_uuid IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM source_captures c JOIN source_posts p ON p.uuid=c.post_uuid
    WHERE c.uuid=NEW.capture_uuid AND p.state='active'
  );
END;
CREATE TRIGGER metadata_field_decision_immutable BEFORE UPDATE ON metadata_field_decisions
WHEN NEW.sequence!=OLD.sequence OR NEW.uuid!=OLD.uuid OR NEW.field!=OLD.field OR NEW.mode!=OLD.mode
  OR NEW.origin!=OLD.origin OR NEW.value_json!=OLD.value_json OR NEW.capture_uuid IS NOT OLD.capture_uuid
  OR NEW.reason!=OLD.reason OR NEW.created_at!=OLD.created_at
  OR (NEW.entity_uuid!=OLD.entity_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.entity_uuid))
BEGIN SELECT RAISE(ABORT, 'metadata field decisions are immutable'); END;
CREATE TRIGGER metadata_field_head_forward BEFORE UPDATE OF decision_uuid ON metadata_field_heads
WHEN NEW.decision_uuid!=OLD.decision_uuid
BEGIN
  SELECT RAISE(ABORT, 'metadata field head cannot move backwards')
    WHERE (SELECT sequence FROM metadata_field_decisions WHERE uuid=NEW.decision_uuid)
      <= (SELECT sequence FROM metadata_field_decisions WHERE uuid=OLD.decision_uuid);
END;
CREATE TRIGGER metadata_field_head_scope BEFORE UPDATE OF entity_uuid, field ON metadata_field_heads
WHEN NEW.field!=OLD.field OR (NEW.entity_uuid!=OLD.entity_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.entity_uuid))
BEGIN SELECT RAISE(ABORT, 'metadata field head scope is immutable'); END;
CREATE TRIGGER metadata_field_current AFTER INSERT ON metadata_field_decisions
BEGIN
  INSERT INTO metadata_field_heads(entity_uuid, field, decision_uuid) VALUES (NEW.entity_uuid, NEW.field, NEW.uuid)
    ON CONFLICT(entity_uuid, field) DO UPDATE SET decision_uuid=excluded.decision_uuid;
  UPDATE archive_entities SET revision=revision+1 WHERE uuid=NEW.entity_uuid;
END;

CREATE TRIGGER metadata_scene_title_library AFTER UPDATE OF title ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='title')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'title', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.title, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='title')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.title, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'title', CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.title, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_code_library AFTER UPDATE OF code ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='code')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'code', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.code, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='code')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.code, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'code', CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.code, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_details_library AFTER UPDATE OF details ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='details')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'details', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.details, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='details')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.details, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'details', CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.details, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_date_library AFTER UPDATE OF date, date_precision ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='date')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'date', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(CASE WHEN OLD.date IS NULL THEN NULL ELSE substr(OLD.date, 1, CASE OLD.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='date')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(CASE WHEN OLD.date IS NULL THEN NULL ELSE substr(OLD.date, 1, CASE OLD.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'date', CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_rating100_library AFTER UPDATE OF rating ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='rating100')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'rating100', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(OLD.rating), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='rating100')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(OLD.rating)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'rating100', CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(NEW.rating)
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_organized_library AFTER UPDATE OF organized ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='organized')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'organized', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    CASE WHEN OLD.organized THEN 'true' ELSE 'false' END, 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='organized')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR CASE WHEN OLD.organized THEN 'true' ELSE 'false' END!='false');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'organized', 'set', 'library', CASE WHEN NEW.organized THEN 'true' ELSE 'false' END
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_director_library AFTER UPDATE OF director ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='director')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'director', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.director, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='director')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.director, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'director', CASE WHEN json_quote(coalesce(NEW.director, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.director, ''))
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_scene_production_date_library AFTER UPDATE OF production_date, production_date_precision ON scenes
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.scene_id=NEW.id AND w.field='production_date')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'production_date', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(CASE WHEN OLD.production_date IS NULL THEN NULL ELSE substr(OLD.production_date, 1, CASE OLD.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.scene_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='production_date')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(CASE WHEN OLD.production_date IS NULL THEN NULL ELSE substr(OLD.production_date, 1, CASE OLD.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'production_date', CASE WHEN json_quote(CASE WHEN NEW.production_date IS NULL THEN NULL ELSE substr(NEW.production_date, 1, CASE NEW.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.production_date IS NULL THEN NULL ELSE substr(NEW.production_date, 1, CASE NEW.production_date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE scene_id=NEW.id;
END;

CREATE TRIGGER metadata_image_title_library AFTER UPDATE OF title ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='title')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'title', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.title, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='title')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.title, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'title', CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.title, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_image_code_library AFTER UPDATE OF code ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='code')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'code', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.code, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='code')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.code, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'code', CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.code, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_image_details_library AFTER UPDATE OF details ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='details')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'details', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.details, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='details')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.details, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'details', CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.details, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_image_date_library AFTER UPDATE OF date, date_precision ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='date')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'date', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(CASE WHEN OLD.date IS NULL THEN NULL ELSE substr(OLD.date, 1, CASE OLD.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='date')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(CASE WHEN OLD.date IS NULL THEN NULL ELSE substr(OLD.date, 1, CASE OLD.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'date', CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_image_rating100_library AFTER UPDATE OF rating ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='rating100')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'rating100', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(OLD.rating), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='rating100')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(OLD.rating)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'rating100', CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(NEW.rating)
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_image_organized_library AFTER UPDATE OF organized ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='organized')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'organized', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    CASE WHEN OLD.organized THEN 'true' ELSE 'false' END, 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='organized')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR CASE WHEN OLD.organized THEN 'true' ELSE 'false' END!='false');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'organized', 'set', 'library', CASE WHEN NEW.organized THEN 'true' ELSE 'false' END
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_image_photographer_library AFTER UPDATE OF photographer ON images
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.image_id=NEW.id AND w.field='photographer')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'photographer', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.photographer, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.image_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='photographer')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.photographer, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'photographer', CASE WHEN json_quote(coalesce(NEW.photographer, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.photographer, ''))
  FROM archive_entities WHERE image_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_title_library AFTER UPDATE OF title ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='title')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'title', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.title, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='title')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.title, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'title', CASE WHEN json_quote(coalesce(NEW.title, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.title, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_code_library AFTER UPDATE OF code ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='code')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'code', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.code, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='code')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.code, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'code', CASE WHEN json_quote(coalesce(NEW.code, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.code, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_details_library AFTER UPDATE OF details ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='details')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'details', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.details, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='details')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.details, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'details', CASE WHEN json_quote(coalesce(NEW.details, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.details, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_date_library AFTER UPDATE OF date, date_precision ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='date')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'date', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(CASE WHEN OLD.date IS NULL THEN NULL ELSE substr(OLD.date, 1, CASE OLD.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='date')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(CASE WHEN OLD.date IS NULL THEN NULL ELSE substr(OLD.date, 1, CASE OLD.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'date', CASE WHEN json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(CASE WHEN NEW.date IS NULL THEN NULL ELSE substr(NEW.date, 1, CASE NEW.date_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_rating100_library AFTER UPDATE OF rating ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='rating100')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'rating100', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(OLD.rating), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='rating100')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(OLD.rating)!='null');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'rating100', CASE WHEN json_quote(NEW.rating)='null' THEN 'clear' ELSE 'set' END, 'library', json_quote(NEW.rating)
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_organized_library AFTER UPDATE OF organized ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='organized')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'organized', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    CASE WHEN OLD.organized THEN 'true' ELSE 'false' END, 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='organized')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR CASE WHEN OLD.organized THEN 'true' ELSE 'false' END!='false');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'organized', 'set', 'library', CASE WHEN NEW.organized THEN 'true' ELSE 'false' END
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

CREATE TRIGGER metadata_gallery_photographer_library AFTER UPDATE OF photographer ON galleries
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_write_context w JOIN archive_entities a ON a.uuid=w.entity_uuid
  WHERE a.gallery_id=NEW.id AND w.field='photographer')
BEGIN
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json, reason)
  SELECT a.uuid, 'photographer', 'preserved',
    CASE WHEN EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) THEN 'legacy' ELSE 'unattributed' END,
    json_quote(coalesce(OLD.photographer, '')), 'Preserved before the first recorded choice'
  FROM archive_entities a WHERE a.gallery_id=NEW.id
    AND NOT EXISTS(SELECT 1 FROM metadata_field_heads h WHERE h.entity_uuid=a.uuid AND h.field='photographer')
    AND (EXISTS(SELECT 1 FROM metadata_field_baselines b WHERE b.entity_uuid=a.uuid) OR json_quote(coalesce(OLD.photographer, ''))!='""');
  INSERT INTO metadata_field_decisions(entity_uuid, field, mode, origin, value_json)
  SELECT uuid, 'photographer', CASE WHEN json_quote(coalesce(NEW.photographer, ''))='""' THEN 'clear' ELSE 'set' END, 'library', json_quote(coalesce(NEW.photographer, ''))
  FROM archive_entities WHERE gallery_id=NEW.id;
END;

INSERT INTO native_migration_history(version, name, details) SELECT 1000012, 'Protected metadata field choices', json_object('preserved_entities', (SELECT count(*) FROM metadata_field_baselines));
