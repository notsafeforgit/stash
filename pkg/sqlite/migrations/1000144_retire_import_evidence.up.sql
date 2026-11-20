-- Retire the remaining frozen import evidence. Schema 1000142 stopped every
-- writer of the per-row enrichment receipts and the catalog file history; no
-- request has read them since. Completions keep their recorded receipt UUID,
-- which their digest covers, as plain evidence without the foreign key. The
-- historical file-edit review and keep workflow was only reachable from file
-- history edits, and none were imported. Lookup values for legacy-only posts
-- (enrichment_identity_lookups) are still used and stay.
PRAGMA defer_foreign_keys=ON;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000144,'Retire the frozen import evidence',json_object(
 'enrichment_receipts',(SELECT count(*) FROM source_enrichment_receipts),
 'receipt_completions',(SELECT count(*) FROM enrichment_completions WHERE legacy_receipt_uuid IS NOT NULL),
 'file_histories',(SELECT count(*) FROM source_file_history),
 'file_history_locations',(SELECT count(*) FROM source_file_history_locations),
 'file_history_deduplications',(SELECT count(*) FROM source_file_history_deduplications),
 'file_history_edits',(SELECT count(*) FROM source_file_history_edits),
 'file_history_states',(SELECT count(*) FROM source_file_history_states),
 'file_edit_reviews',(SELECT count(*) FROM metadata_file_edit_reviews),
 'file_edit_keeps',(SELECT count(*) FROM metadata_file_edit_keeps)));

-- Superseded field decisions no longer need protection for file-edit evidence.
DROP TRIGGER metadata_field_retire_superseded;
CREATE TRIGGER metadata_field_retire_superseded AFTER UPDATE OF decision_uuid ON metadata_field_heads
WHEN NEW.decision_uuid!=OLD.decision_uuid
BEGIN
  DELETE FROM metadata_field_decisions WHERE uuid=OLD.decision_uuid;
END;

DROP TABLE metadata_file_edit_keeps;
DROP TABLE metadata_file_edit_reviews;
DROP TABLE source_file_history_edits;
DROP TABLE source_file_history_states;
DROP TABLE source_file_history_deduplications;
DROP TABLE source_file_history_locations;
DROP TABLE source_file_history;

-- Rebuild completions without the receipt reference. Every other constraint,
-- the row identities and the referencing tables are unchanged.
CREATE TABLE native_completion_rows AS SELECT rowid AS original_rowid, * FROM enrichment_completions;
DROP TABLE enrichment_completions;
CREATE TABLE enrichment_completions (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 target_uuid TEXT NOT NULL UNIQUE REFERENCES enrichment_targets(uuid),
 expected_revision INTEGER NOT NULL CHECK(typeof(expected_revision)='integer' AND expected_revision>0),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 capture_count INTEGER NOT NULL CHECK(typeof(capture_count)='integer' AND capture_count BETWEEN 0 AND 1024),
 legacy_receipt_uuid TEXT CHECK(legacy_receipt_uuid IS NULL OR (length(legacy_receipt_uuid)=36 AND legacy_receipt_uuid=lower(legacy_receipt_uuid)
  AND substr(legacy_receipt_uuid,9,1)='-' AND substr(legacy_receipt_uuid,14,1)='-' AND substr(legacy_receipt_uuid,19,1)='-' AND substr(legacy_receipt_uuid,24,1)='-'
  AND length(replace(legacy_receipt_uuid,'-',''))=32 AND replace(legacy_receipt_uuid,'-','') NOT GLOB '*[^0-9a-f]*')),
 legacy_capture_uuid TEXT REFERENCES source_captures(uuid),
 created_at DATETIME NOT NULL,
 CHECK((legacy_receipt_uuid IS NULL AND legacy_capture_uuid IS NULL AND capture_count>=1)
  OR (legacy_receipt_uuid IS NOT NULL AND legacy_capture_uuid IS NULL AND capture_count=0)
  OR (legacy_receipt_uuid IS NULL AND legacy_capture_uuid IS NOT NULL AND capture_count=0)),
 FOREIGN KEY(target_uuid,uuid) REFERENCES enrichment_targets(uuid,completion_uuid) DEFERRABLE INITIALLY DEFERRED
);
INSERT INTO enrichment_completions(rowid,uuid,target_uuid,expected_revision,request_digest,capture_count,legacy_receipt_uuid,legacy_capture_uuid,created_at)
SELECT original_rowid,uuid,target_uuid,expected_revision,request_digest,capture_count,legacy_receipt_uuid,legacy_capture_uuid,created_at
FROM native_completion_rows ORDER BY original_rowid;
DROP TABLE native_completion_rows;
CREATE INDEX enrichment_completion_legacy_capture ON enrichment_completions(legacy_capture_uuid) WHERE legacy_capture_uuid IS NOT NULL;
CREATE TRIGGER enrichment_completion_immutable BEFORE UPDATE ON enrichment_completions
BEGIN SELECT RAISE(ABORT,'enrichment completion evidence is immutable'); END;
-- A new receipt-backed completion has no receipt to prove its scope.
CREATE TRIGGER enrichment_completion_scope BEFORE INSERT ON enrichment_completions
WHEN NOT EXISTS(SELECT 1 FROM enrichment_targets t WHERE t.uuid=NEW.target_uuid AND t.revision=NEW.expected_revision AND t.updated_at<=NEW.created_at
 AND ((NEW.legacy_receipt_uuid IS NULL AND NEW.legacy_capture_uuid IS NULL AND t.state='pending' AND t.not_before<=NEW.created_at)
  OR (NEW.legacy_receipt_uuid IS NULL AND NEW.legacy_capture_uuid IS NOT NULL
   AND t.origin='migration' AND t.state='held' AND t.collection_revision=1
   AND EXISTS(SELECT 1 FROM source_collection_revisions c WHERE c.collection_uuid=t.collection_uuid AND c.revision=1 AND c.origin='migration')
   AND EXISTS(SELECT 1 FROM source_captures c JOIN source_collection_captures b ON b.capture_uuid=c.uuid
     WHERE c.uuid=NEW.legacy_capture_uuid AND c.post_uuid=t.post_uuid AND c.origin='gallery-dl'
     AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision))))
BEGIN SELECT RAISE(ABORT,'enrichment completion requires its exact target revision and proof scope'); END;

DROP TABLE source_enrichment_receipts;
