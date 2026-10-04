-- Explicit library choices retain the exact historical edit and file match.
-- Catalog evidence remains immutable and does not select metadata by itself.
CREATE TABLE metadata_file_edit_reviews (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 decision_uuid TEXT NOT NULL UNIQUE REFERENCES metadata_field_decisions(uuid),
 history_uuid TEXT NOT NULL,
 source_field TEXT NOT NULL,
 match_uuid TEXT NOT NULL REFERENCES source_file_matches(uuid),
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=262144),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(history_uuid,source_field) REFERENCES source_file_history_edits(history_uuid,field)
);
CREATE INDEX metadata_file_edit_reviews_history ON metadata_file_edit_reviews(history_uuid,source_field,request_uuid);
CREATE TRIGGER metadata_file_edit_review_immutable BEFORE UPDATE ON metadata_file_edit_reviews
BEGIN SELECT RAISE(ABORT,'historical metadata review receipts are immutable'); END;
CREATE TRIGGER metadata_file_edit_review_scope BEFORE INSERT ON metadata_file_edit_reviews
WHEN NOT EXISTS(SELECT 1 FROM source_file_history_edits e
 JOIN source_file_history_locations l ON l.history_uuid=e.history_uuid
 JOIN source_file_matches m ON m.observation_uuid=l.observation_uuid
 JOIN metadata_field_decisions d ON d.uuid=NEW.decision_uuid
 JOIN archive_entities owner ON owner.uuid=d.entity_uuid AND owner.state='active'
 JOIN archive_entities f ON f.uuid=m.file_uuid AND f.kind='file' AND f.state='active'
 JOIN files current_file ON current_file.id=f.file_id AND current_file.generation=m.generation
 WHERE e.history_uuid=NEW.history_uuid AND e.field=NEW.source_field AND m.uuid=NEW.match_uuid
 AND e.mode!='unmapped' AND d.field=e.target_field AND d.mode=e.mode
 AND d.origin='review' AND d.capture_uuid IS NULL AND d.sealed=1
 AND (EXISTS(SELECT 1 FROM scenes_files sf WHERE sf.scene_id=owner.scene_id AND sf.file_id=f.file_id)
 OR EXISTS(SELECT 1 FROM images_files imf WHERE imf.image_id=owner.image_id AND imf.file_id=f.file_id)))
BEGIN SELECT RAISE(ABORT,'historical metadata review is outside its source scope'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000067,'Reviewed historical metadata choices and retry receipts','{}');
