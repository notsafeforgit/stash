-- Collection edits are captured once at commit, after all join changes.
-- Historical references use checked UUID foreign keys, including tombstones.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_metadata_decision_rows AS SELECT * FROM metadata_field_decisions;
CREATE TABLE native_metadata_decision_sequence AS SELECT seq FROM sqlite_sequence WHERE name='metadata_field_decisions';
DROP TABLE metadata_field_decisions;
DROP TABLE metadata_field_write_context;
DROP TRIGGER metadata_field_head_forward;
DROP TRIGGER metadata_field_head_scope;
CREATE TABLE metadata_field_decisions (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid TEXT NOT NULL UNIQUE DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))))
    CHECK (length(uuid)=36 AND uuid=lower(uuid)
      AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
      AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
      AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
      AND uuid!='00000000-0000-0000-0000-000000000000'),
  entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  field TEXT NOT NULL CHECK (field IN ('title','code','details','date','rating100','organized','director','production_date','photographer','urls','custom_fields','studio','performers','tags','groups')),
  mode TEXT NOT NULL CHECK (mode IN ('set','clear','inherit','preserved')),
  origin TEXT NOT NULL CHECK (origin IN ('library','review','migration','legacy','unattributed','source','policy','filename')),
  value_json TEXT NOT NULL CHECK (length(CAST(value_json AS BLOB)) <= 4194304 AND json_valid(value_json)),
  capture_uuid TEXT REFERENCES source_captures(uuid),
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),
  sealed BOOLEAN NOT NULL DEFAULT 1 CHECK (sealed IN (0,1)),
  reference_count INTEGER NOT NULL DEFAULT 0 CHECK (reference_count BETWEEN 0 AND 4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (entity_uuid, field, uuid),
  CHECK (mode!='preserved' OR origin IN ('legacy','unattributed','migration')),
  CHECK (origin NOT IN ('legacy','unattributed') OR mode='preserved'),
  CHECK (origin NOT IN ('source','policy','filename') OR mode='inherit'),
  CHECK (origin!='library' OR mode IN ('set','clear')),
  CHECK (origin!='source' OR capture_uuid IS NOT NULL),
  CHECK (origin!='filename' OR (field='title' AND capture_uuid IS NULL)),
  CHECK (field IN ('studio','performers','tags','groups') OR (sealed=1 AND reference_count=0)),
  CHECK (sealed=1 OR reference_count=0),
  CHECK (CASE
    WHEN field IN ('studio','performers','tags','groups') THEN json_type(value_json)='null'
    WHEN field='urls' THEN json_type(value_json)='array'
    WHEN field='custom_fields' THEN json_type(value_json)='object'
    WHEN field IN ('date','production_date') THEN json_type(value_json) IN ('null','text')
    WHEN field='rating100' THEN json_type(value_json) IN ('null','integer')
    WHEN field='organized' THEN json_type(value_json) IN ('true','false')
    ELSE json_type(value_json)='text' END),
  CHECK (mode!='clear' OR CASE
    WHEN field IN ('studio','performers','tags','groups') THEN json_type(value_json)='null'
    WHEN field='urls' THEN json_array_length(value_json)=0
    WHEN field='custom_fields' THEN value_json='{}'
    WHEN field IN ('date','production_date','rating100') THEN json_type(value_json)='null'
    WHEN field='organized' THEN json_type(value_json)='false'
    ELSE json_extract(value_json,'$')='' END)
);

INSERT INTO metadata_field_decisions(sequence,uuid,entity_uuid,field,mode,origin,value_json,capture_uuid,reason,created_at) SELECT sequence,uuid,entity_uuid,field,mode,origin,value_json,capture_uuid,reason,created_at FROM native_metadata_decision_rows;
INSERT INTO sqlite_sequence(name,seq)
SELECT 'metadata_field_decisions',seq FROM native_metadata_decision_sequence
WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='metadata_field_decisions');
UPDATE sqlite_sequence SET seq=max(seq,coalesce((SELECT seq FROM native_metadata_decision_sequence),0))
WHERE name='metadata_field_decisions';
DROP TABLE native_metadata_decision_sequence;
DROP TABLE native_metadata_decision_rows;
CREATE INDEX metadata_field_decisions_entity ON metadata_field_decisions(entity_uuid,field,sequence);
CREATE INDEX metadata_field_decisions_capture ON metadata_field_decisions(capture_uuid) WHERE capture_uuid IS NOT NULL;
CREATE INDEX metadata_field_decisions_unsealed ON metadata_field_decisions(sequence) WHERE sealed=0;
CREATE TABLE metadata_field_write_context (
  entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  field TEXT NOT NULL CHECK (field IN ('title','code','details','date','rating100','organized','director','production_date','photographer','urls','custom_fields','studio','performers','tags','groups')),
  PRIMARY KEY (entity_uuid, field)
) WITHOUT ROWID;
CREATE TABLE metadata_field_references (
  decision_uuid TEXT NOT NULL REFERENCES metadata_field_decisions(uuid),
  position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 4095),
  target_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  scene_index INTEGER CHECK(scene_index IS NULL OR typeof(scene_index)='integer'),
  PRIMARY KEY(decision_uuid,position),
  UNIQUE(decision_uuid,target_uuid)
) WITHOUT ROWID;
CREATE INDEX metadata_field_references_target ON metadata_field_references(target_uuid);
CREATE INDEX metadata_groups_scenes_owner ON groups_scenes(scene_id,group_id);
CREATE TABLE metadata_field_pending (
  entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  field TEXT NOT NULL CHECK(field IN ('urls','custom_fields','studio','performers','tags','groups')),
  previous_json TEXT NOT NULL CHECK(json_valid(previous_json) AND length(CAST(previous_json AS BLOB))<=4194304),
  PRIMARY KEY(entity_uuid,field)
) WITHOUT ROWID;
CREATE TRIGGER metadata_field_decision_scope BEFORE INSERT ON metadata_field_decisions
BEGIN
  SELECT RAISE(ABORT, 'invalid metadata field entity or target') WHERE NOT EXISTS (
    SELECT 1 FROM archive_entities a WHERE a.uuid=NEW.entity_uuid AND a.state='active'
    AND a.kind IN ('scene','image','gallery')
    AND (NEW.field NOT IN ('director','production_date','groups') OR a.kind='scene')
    AND (NEW.field!='photographer' OR a.kind IN ('image','gallery'))
  );
  SELECT RAISE(ABORT, 'reference decisions must be built before sealing') WHERE NEW.field IN ('studio','performers','tags','groups') AND NEW.sealed!=0;
  SELECT RAISE(ABORT, 'metadata choice requires an active source capture') WHERE NEW.capture_uuid IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM source_captures c JOIN source_posts p ON p.uuid=c.post_uuid
    WHERE c.uuid=NEW.capture_uuid AND p.state='active'
  );
END;
CREATE TRIGGER metadata_field_decision_immutable BEFORE UPDATE ON metadata_field_decisions
WHEN (NEW.sealed!=OLD.sealed AND NOT (OLD.sealed=0 AND NEW.sealed=1)) OR (NEW.reference_count!=OLD.reference_count AND NOT (OLD.sealed=0 AND NEW.sealed=1)) OR NEW.sequence!=OLD.sequence OR NEW.uuid!=OLD.uuid OR NEW.field!=OLD.field OR NEW.mode!=OLD.mode
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
WHEN NEW.sealed=1
BEGIN
  INSERT INTO metadata_field_heads(entity_uuid, field, decision_uuid) VALUES (NEW.entity_uuid, NEW.field, NEW.uuid)
    ON CONFLICT(entity_uuid, field) DO UPDATE SET decision_uuid=excluded.decision_uuid;
  UPDATE archive_entities SET revision=revision+1 WHERE uuid=NEW.entity_uuid;
END;

CREATE TRIGGER metadata_reference_scope BEFORE INSERT ON metadata_field_references
BEGIN
  SELECT RAISE(ABORT, 'metadata reference is not a mutable typed decision') WHERE NOT EXISTS (
    SELECT 1 FROM metadata_field_decisions d JOIN archive_entities a ON a.uuid=NEW.target_uuid
    WHERE d.uuid=NEW.decision_uuid AND d.sealed=0 AND d.mode!='clear'
    AND ((d.field='studio' AND a.kind='studio' AND NEW.position=0 AND NEW.scene_index IS NULL)
      OR (d.field='performers' AND a.kind='performer' AND NEW.scene_index IS NULL)
      OR (d.field='tags' AND a.kind='tag' AND NEW.scene_index IS NULL)
      OR (d.field='groups' AND a.kind='group'))
  );
END;
CREATE TRIGGER metadata_reference_immutable BEFORE UPDATE ON metadata_field_references
WHEN NEW.decision_uuid!=OLD.decision_uuid OR NEW.position!=OLD.position OR NEW.scene_index IS NOT OLD.scene_index
  OR (NEW.target_uuid!=OLD.target_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.target_uuid))
BEGIN SELECT RAISE(ABORT, 'metadata references are immutable'); END;
CREATE TRIGGER metadata_reference_seal BEFORE UPDATE OF sealed ON metadata_field_decisions
WHEN OLD.sealed=0 AND NEW.sealed=1
BEGIN
  SELECT RAISE(ABORT, 'metadata reference count differs') WHERE NEW.reference_count!=(SELECT count(*) FROM metadata_field_references WHERE decision_uuid=NEW.uuid);
  SELECT RAISE(ABORT, 'reference positions are not contiguous') WHERE EXISTS(
    SELECT 1 FROM metadata_field_references WHERE decision_uuid=NEW.uuid
    GROUP BY decision_uuid HAVING min(position)!=0 OR max(position)+1!=count(*)
  );
  SELECT RAISE(ABORT, 'cleared relationship contains references') WHERE NEW.mode='clear'
    AND EXISTS(SELECT 1 FROM metadata_field_references WHERE decision_uuid=NEW.uuid);
END;
CREATE TRIGGER metadata_reference_current AFTER UPDATE OF sealed ON metadata_field_decisions
WHEN OLD.sealed=0 AND NEW.sealed=1
BEGIN
  INSERT INTO metadata_field_heads(entity_uuid,field,decision_uuid) VALUES(NEW.entity_uuid,NEW.field,NEW.uuid)
    ON CONFLICT(entity_uuid,field) DO UPDATE SET decision_uuid=excluded.decision_uuid;
  UPDATE archive_entities SET revision=revision+1 WHERE uuid=NEW.entity_uuid;
END;
CREATE TRIGGER metadata_head_sealed_insert BEFORE INSERT ON metadata_field_heads
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_decisions WHERE uuid=NEW.decision_uuid AND sealed=1)
BEGIN SELECT RAISE(ABORT, 'metadata head requires a sealed decision'); END;
CREATE TRIGGER metadata_head_sealed_update BEFORE UPDATE OF decision_uuid ON metadata_field_heads
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_decisions WHERE uuid=NEW.decision_uuid AND sealed=1)
BEGIN SELECT RAISE(ABORT, 'metadata head requires a sealed decision'); END;
CREATE VIEW metadata_collection_values AS
SELECT a.uuid AS entity_uuid,'studio' AS field,(SELECT json_quote(t.uuid) FROM scenes j LEFT JOIN archive_entities t ON t.studio_id=j.studio_id WHERE j.id=a.scene_id) AS value_json,EXISTS(SELECT 1 FROM scenes j LEFT JOIN archive_entities t ON t.studio_id=j.studio_id WHERE j.id=a.scene_id AND (j.studio_id IS NULL OR t.uuid IS NOT NULL)) AS references_valid FROM archive_entities a WHERE a.kind='scene' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'performers' AS field,(SELECT json_group_array(uuid) FROM (SELECT t.uuid FROM performers_scenes j LEFT JOIN archive_entities t ON t.performer_id=j.performer_id WHERE j.scene_id=a.scene_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM performers_scenes j LEFT JOIN archive_entities t ON t.performer_id=j.performer_id WHERE j.scene_id=a.scene_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='scene' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'tags' AS field,(SELECT json_group_array(uuid) FROM (SELECT t.uuid FROM scenes_tags j LEFT JOIN archive_entities t ON t.tag_id=j.tag_id WHERE j.scene_id=a.scene_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM scenes_tags j LEFT JOIN archive_entities t ON t.tag_id=j.tag_id WHERE j.scene_id=a.scene_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='scene' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'urls' AS field,(SELECT json_group_array(url) FROM (SELECT url FROM scene_urls WHERE scene_id=a.scene_id ORDER BY position)) AS value_json,1 AS references_valid FROM archive_entities a WHERE a.kind='scene' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'custom_fields' AS field,(SELECT json_group_object(field,json(encoded)) FROM (SELECT field, CASE typeof(value) WHEN 'real' THEN printf('%!.17g',value) ELSE json_quote(value) END AS encoded FROM scene_custom_fields WHERE scene_id=a.scene_id ORDER BY field)) AS value_json,1 AS references_valid FROM archive_entities a WHERE a.kind='scene' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'groups' AS field,(SELECT json_group_array(json(value)) FROM (SELECT json_object('uuid',t.uuid,'scene_index',j.scene_index) AS value FROM groups_scenes j LEFT JOIN archive_entities t ON t.group_id=j.group_id WHERE j.scene_id=a.scene_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM groups_scenes j LEFT JOIN archive_entities t ON t.group_id=j.group_id WHERE j.scene_id=a.scene_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='scene' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'studio' AS field,(SELECT json_quote(t.uuid) FROM images j LEFT JOIN archive_entities t ON t.studio_id=j.studio_id WHERE j.id=a.image_id) AS value_json,EXISTS(SELECT 1 FROM images j LEFT JOIN archive_entities t ON t.studio_id=j.studio_id WHERE j.id=a.image_id AND (j.studio_id IS NULL OR t.uuid IS NOT NULL)) AS references_valid FROM archive_entities a WHERE a.kind='image' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'performers' AS field,(SELECT json_group_array(uuid) FROM (SELECT t.uuid FROM performers_images j LEFT JOIN archive_entities t ON t.performer_id=j.performer_id WHERE j.image_id=a.image_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM performers_images j LEFT JOIN archive_entities t ON t.performer_id=j.performer_id WHERE j.image_id=a.image_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='image' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'tags' AS field,(SELECT json_group_array(uuid) FROM (SELECT t.uuid FROM images_tags j LEFT JOIN archive_entities t ON t.tag_id=j.tag_id WHERE j.image_id=a.image_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM images_tags j LEFT JOIN archive_entities t ON t.tag_id=j.tag_id WHERE j.image_id=a.image_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='image' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'urls' AS field,(SELECT json_group_array(url) FROM (SELECT url FROM image_urls WHERE image_id=a.image_id ORDER BY position)) AS value_json,1 AS references_valid FROM archive_entities a WHERE a.kind='image' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'custom_fields' AS field,(SELECT json_group_object(field,json(encoded)) FROM (SELECT field, CASE typeof(value) WHEN 'real' THEN printf('%!.17g',value) ELSE json_quote(value) END AS encoded FROM image_custom_fields WHERE image_id=a.image_id ORDER BY field)) AS value_json,1 AS references_valid FROM archive_entities a WHERE a.kind='image' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'studio' AS field,(SELECT json_quote(t.uuid) FROM galleries j LEFT JOIN archive_entities t ON t.studio_id=j.studio_id WHERE j.id=a.gallery_id) AS value_json,EXISTS(SELECT 1 FROM galleries j LEFT JOIN archive_entities t ON t.studio_id=j.studio_id WHERE j.id=a.gallery_id AND (j.studio_id IS NULL OR t.uuid IS NOT NULL)) AS references_valid FROM archive_entities a WHERE a.kind='gallery' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'performers' AS field,(SELECT json_group_array(uuid) FROM (SELECT t.uuid FROM performers_galleries j LEFT JOIN archive_entities t ON t.performer_id=j.performer_id WHERE j.gallery_id=a.gallery_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM performers_galleries j LEFT JOIN archive_entities t ON t.performer_id=j.performer_id WHERE j.gallery_id=a.gallery_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='gallery' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'tags' AS field,(SELECT json_group_array(uuid) FROM (SELECT t.uuid FROM galleries_tags j LEFT JOIN archive_entities t ON t.tag_id=j.tag_id WHERE j.gallery_id=a.gallery_id ORDER BY t.uuid)) AS value_json,NOT EXISTS(SELECT 1 FROM galleries_tags j LEFT JOIN archive_entities t ON t.tag_id=j.tag_id WHERE j.gallery_id=a.gallery_id AND t.uuid IS NULL) AS references_valid FROM archive_entities a WHERE a.kind='gallery' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'urls' AS field,(SELECT json_group_array(url) FROM (SELECT url FROM gallery_urls WHERE gallery_id=a.gallery_id ORDER BY position)) AS value_json,1 AS references_valid FROM archive_entities a WHERE a.kind='gallery' AND a.state='active'
UNION ALL
SELECT a.uuid AS entity_uuid,'custom_fields' AS field,(SELECT json_group_object(field,json(encoded)) FROM (SELECT field, CASE typeof(value) WHEN 'real' THEN printf('%!.17g',value) ELSE json_quote(value) END AS encoded FROM gallery_custom_fields WHERE gallery_id=a.gallery_id ORDER BY field)) AS value_json,1 AS references_valid FROM archive_entities a WHERE a.kind='gallery' AND a.state='active';
CREATE TRIGGER metadata_scene_studio_update BEFORE UPDATE OF studio_id ON scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'studio',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='studio'
  WHERE (a.scene_id=OLD.id OR a.scene_id=NEW.id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='studio');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.id OR scene_id=NEW.id);
END;
CREATE TRIGGER metadata_scene_performers_insert BEFORE INSERT ON performers_scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_performers_delete BEFORE DELETE ON performers_scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.scene_id=OLD.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id);
END;
CREATE TRIGGER metadata_scene_performers_update BEFORE UPDATE ON performers_scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.scene_id=OLD.scene_id OR a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id OR scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_tags_insert BEFORE INSERT ON scenes_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_tags_delete BEFORE DELETE ON scenes_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.scene_id=OLD.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id);
END;
CREATE TRIGGER metadata_scene_tags_update BEFORE UPDATE ON scenes_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.scene_id=OLD.scene_id OR a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id OR scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_urls_insert BEFORE INSERT ON scene_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_urls_delete BEFORE DELETE ON scene_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.scene_id=OLD.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id);
END;
CREATE TRIGGER metadata_scene_urls_update BEFORE UPDATE ON scene_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.scene_id=OLD.scene_id OR a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id OR scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_custom_fields_insert BEFORE INSERT ON scene_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_custom_fields_delete BEFORE DELETE ON scene_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.scene_id=OLD.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id);
END;
CREATE TRIGGER metadata_scene_custom_fields_update BEFORE UPDATE ON scene_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.scene_id=OLD.scene_id OR a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id OR scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_groups_insert BEFORE INSERT ON groups_scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'groups',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='groups'
  WHERE (a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='groups')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='groups');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_scene_groups_delete BEFORE DELETE ON groups_scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'groups',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='groups'
  WHERE (a.scene_id=OLD.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='groups')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='groups');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id);
END;
CREATE TRIGGER metadata_scene_groups_update BEFORE UPDATE ON groups_scenes
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'groups',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='groups'
  WHERE (a.scene_id=OLD.scene_id OR a.scene_id=NEW.scene_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='groups')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='groups');
  UPDATE archive_entities SET revision=revision+1 WHERE (scene_id=OLD.scene_id OR scene_id=NEW.scene_id);
END;
CREATE TRIGGER metadata_image_studio_update BEFORE UPDATE OF studio_id ON images
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'studio',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='studio'
  WHERE (a.image_id=OLD.id OR a.image_id=NEW.id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='studio');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.id OR image_id=NEW.id);
END;
CREATE TRIGGER metadata_image_performers_insert BEFORE INSERT ON performers_images
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_performers_delete BEFORE DELETE ON performers_images
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.image_id=OLD.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id);
END;
CREATE TRIGGER metadata_image_performers_update BEFORE UPDATE ON performers_images
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.image_id=OLD.image_id OR a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id OR image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_tags_insert BEFORE INSERT ON images_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_tags_delete BEFORE DELETE ON images_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.image_id=OLD.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id);
END;
CREATE TRIGGER metadata_image_tags_update BEFORE UPDATE ON images_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.image_id=OLD.image_id OR a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id OR image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_urls_insert BEFORE INSERT ON image_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_urls_delete BEFORE DELETE ON image_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.image_id=OLD.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id);
END;
CREATE TRIGGER metadata_image_urls_update BEFORE UPDATE ON image_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.image_id=OLD.image_id OR a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id OR image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_custom_fields_insert BEFORE INSERT ON image_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_image_custom_fields_delete BEFORE DELETE ON image_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.image_id=OLD.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id);
END;
CREATE TRIGGER metadata_image_custom_fields_update BEFORE UPDATE ON image_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.image_id=OLD.image_id OR a.image_id=NEW.image_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (image_id=OLD.image_id OR image_id=NEW.image_id);
END;
CREATE TRIGGER metadata_gallery_studio_update BEFORE UPDATE OF studio_id ON galleries
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'studio',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='studio'
  WHERE (a.gallery_id=OLD.id OR a.gallery_id=NEW.id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='studio');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.id OR gallery_id=NEW.id);
END;
CREATE TRIGGER metadata_gallery_performers_insert BEFORE INSERT ON performers_galleries
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_performers_delete BEFORE DELETE ON performers_galleries
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.gallery_id=OLD.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id);
END;
CREATE TRIGGER metadata_gallery_performers_update BEFORE UPDATE ON performers_galleries
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (a.gallery_id=OLD.gallery_id OR a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id OR gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_tags_insert BEFORE INSERT ON galleries_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_tags_delete BEFORE DELETE ON galleries_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.gallery_id=OLD.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id);
END;
CREATE TRIGGER metadata_gallery_tags_update BEFORE UPDATE ON galleries_tags
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (a.gallery_id=OLD.gallery_id OR a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id OR gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_urls_insert BEFORE INSERT ON gallery_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_urls_delete BEFORE DELETE ON gallery_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.gallery_id=OLD.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id);
END;
CREATE TRIGGER metadata_gallery_urls_update BEFORE UPDATE ON gallery_urls
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'urls',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='urls'
  WHERE (a.gallery_id=OLD.gallery_id OR a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='urls')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='urls');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id OR gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_custom_fields_insert BEFORE INSERT ON gallery_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_gallery_custom_fields_delete BEFORE DELETE ON gallery_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.gallery_id=OLD.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id);
END;
CREATE TRIGGER metadata_gallery_custom_fields_update BEFORE UPDATE ON gallery_custom_fields
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'custom_fields',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='custom_fields'
  WHERE (a.gallery_id=OLD.gallery_id OR a.gallery_id=NEW.gallery_id) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='custom_fields')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='custom_fields');
  UPDATE archive_entities SET revision=revision+1 WHERE (gallery_id=OLD.gallery_id OR gallery_id=NEW.gallery_id);
END;
CREATE TRIGGER metadata_reference_retiring BEFORE UPDATE OF state ON archive_entities
WHEN OLD.state='active' AND NEW.state!='active' AND OLD.kind IN ('performer','tag','studio','group')
BEGIN
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'studio',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='studio'
  WHERE (OLD.kind='studio' AND a.scene_id IN (SELECT id FROM scenes WHERE studio_id=OLD.studio_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='studio');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (OLD.kind='performer' AND a.scene_id IN (SELECT scene_id FROM performers_scenes WHERE performer_id=OLD.performer_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (OLD.kind='tag' AND a.scene_id IN (SELECT scene_id FROM scenes_tags WHERE tag_id=OLD.tag_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'groups',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='groups'
  WHERE (OLD.kind='group' AND a.scene_id IN (SELECT scene_id FROM groups_scenes WHERE group_id=OLD.group_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='groups')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='groups');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'studio',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='studio'
  WHERE (OLD.kind='studio' AND a.image_id IN (SELECT id FROM images WHERE studio_id=OLD.studio_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='studio');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (OLD.kind='performer' AND a.image_id IN (SELECT image_id FROM performers_images WHERE performer_id=OLD.performer_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (OLD.kind='tag' AND a.image_id IN (SELECT image_id FROM images_tags WHERE tag_id=OLD.tag_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'studio',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='studio'
  WHERE (OLD.kind='studio' AND a.gallery_id IN (SELECT id FROM galleries WHERE studio_id=OLD.studio_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='studio');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'performers',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='performers'
  WHERE (OLD.kind='performer' AND a.gallery_id IN (SELECT gallery_id FROM performers_galleries WHERE performer_id=OLD.performer_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='performers');
  INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
  SELECT a.uuid,'tags',v.value_json FROM archive_entities a
  JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field='tags'
  WHERE (OLD.kind='tag' AND a.gallery_id IN (SELECT gallery_id FROM galleries_tags WHERE tag_id=OLD.tag_id)) AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='tags')
    AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field='tags');
END;
INSERT INTO native_migration_history(version,name,details) VALUES(1000013,'Typed metadata relationships and coalesced collection choices','{}');
