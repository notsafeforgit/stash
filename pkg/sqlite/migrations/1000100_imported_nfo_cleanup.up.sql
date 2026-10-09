-- Completed imports may discard NFO source inputs after their domain values
-- are materialized. This is one flag per catalog, not a per-document ledger.
ALTER TABLE catalog_snapshots ADD COLUMN nfo_compacted INTEGER NOT NULL DEFAULT 0
 CHECK(nfo_compacted IN (0,1));

CREATE VIEW catalog_document_pending_imports AS
 SELECT i.* FROM catalog_document_imports i
 JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid WHERE s.nfo_compacted=0;

CREATE TRIGGER catalog_nfo_compaction_guard BEFORE UPDATE OF nfo_compacted ON catalog_snapshots
WHEN NEW.nfo_compacted<OLD.nfo_compacted
 OR (NEW.nfo_compacted=1 AND (NEW.state!='received'
  OR NOT EXISTS(SELECT 1 FROM catalog_document_imports i WHERE i.snapshot_uuid=NEW.uuid AND i.state='mapped' AND i.phase='complete')
  OR EXISTS(SELECT 1 FROM catalog_document_records r WHERE r.snapshot_uuid=NEW.uuid)
  OR EXISTS(SELECT 1 FROM catalog_snapshot_records r WHERE r.snapshot_uuid=NEW.uuid AND r.source_table IN ('sidecars','sidecar_documents','sidecar_sources','sidecar_heads'))))
BEGIN SELECT RAISE(ABORT,'NFO inputs can only be discarded after a completed import'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000100,'Discard redundant NFO inputs after catalog materialization','{}');
