-- A reviewed historical value may be declined without changing the selected
-- library field, its protection, or its provenance. Exact receipts survive
-- later field edits, file deletion and identity adoption.
CREATE TABLE metadata_file_edit_keeps (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 entity_revision INTEGER NOT NULL CHECK(entity_revision>0),
 history_uuid TEXT NOT NULL,
 source_field TEXT NOT NULL,
 match_uuid TEXT NOT NULL REFERENCES source_file_matches(uuid),
 selected_decision_uuid TEXT REFERENCES metadata_field_decisions(uuid),
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=262144),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(history_uuid,source_field) REFERENCES source_file_history_edits(history_uuid,field)
);
CREATE INDEX metadata_file_edit_keeps_history ON metadata_file_edit_keeps(history_uuid,source_field,entity_uuid);
CREATE INDEX metadata_file_edit_keeps_entity ON metadata_file_edit_keeps(entity_uuid,request_uuid);
CREATE TRIGGER metadata_file_edit_keep_immutable BEFORE UPDATE ON metadata_file_edit_keeps
WHEN NEW.request_uuid!=OLD.request_uuid OR NEW.entity_revision!=OLD.entity_revision
 OR NEW.history_uuid!=OLD.history_uuid OR NEW.source_field!=OLD.source_field OR NEW.match_uuid!=OLD.match_uuid
 OR NEW.selected_decision_uuid IS NOT OLD.selected_decision_uuid OR NEW.request_json!=OLD.request_json
 OR NEW.signature!=OLD.signature OR NEW.created_at!=OLD.created_at
 OR (NEW.entity_uuid!=OLD.entity_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.entity_uuid))
BEGIN SELECT RAISE(ABORT,'historical metadata keep receipts are immutable'); END;
CREATE TRIGGER metadata_file_edit_keep_request_distinct BEFORE INSERT ON metadata_file_edit_keeps
WHEN EXISTS(SELECT 1 FROM metadata_file_edit_reviews WHERE request_uuid=NEW.request_uuid)
BEGIN SELECT RAISE(ABORT,'historical metadata request already has an apply receipt'); END;
CREATE TRIGGER metadata_file_edit_apply_request_distinct BEFORE INSERT ON metadata_file_edit_reviews
WHEN EXISTS(SELECT 1 FROM metadata_file_edit_keeps WHERE request_uuid=NEW.request_uuid)
BEGIN SELECT RAISE(ABORT,'historical metadata request already has a keep receipt'); END;
CREATE TRIGGER metadata_file_edit_keep_scope BEFORE INSERT ON metadata_file_edit_keeps
WHEN NOT EXISTS(SELECT 1 FROM source_file_history_edits e
 JOIN source_file_history_locations l ON l.history_uuid=e.history_uuid
 JOIN source_file_matches m ON m.observation_uuid=l.observation_uuid
 JOIN archive_entities owner ON owner.uuid=NEW.entity_uuid AND owner.state='active' AND owner.revision=NEW.entity_revision
 JOIN archive_entities f ON f.uuid=m.file_uuid AND f.kind='file' AND f.state='active'
 JOIN files current_file ON current_file.id=f.file_id AND current_file.generation=m.generation
 WHERE e.history_uuid=NEW.history_uuid AND e.field=NEW.source_field AND m.uuid=NEW.match_uuid
 AND (SELECT decision_uuid FROM metadata_field_heads WHERE entity_uuid=owner.uuid AND field=e.target_field) IS NEW.selected_decision_uuid
 AND (EXISTS(SELECT 1 FROM scenes_files sf WHERE sf.scene_id=owner.scene_id AND sf.file_id=f.file_id)
 OR EXISTS(SELECT 1 FROM images_files imf WHERE imf.image_id=owner.image_id AND imf.file_id=f.file_id)))
BEGIN SELECT RAISE(ABORT,'historical metadata keep is outside its reviewed scope'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000094,'Reviewed retention of current metadata without field mutations','{}');
