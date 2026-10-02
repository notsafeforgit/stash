CREATE TABLE catalog_document_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision=1),
 policy TEXT NOT NULL CHECK(policy='catalog-documents-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 phase TEXT NOT NULL CHECK(phase IN ('sidecar_documents','sidecar_sources','sidecars','sidecar_heads','complete')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK(mapped_records+review_records=processed_records),
 CHECK((state='running')=(phase!='complete')),
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_document_import_guard BEFORE UPDATE ON catalog_document_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256
 OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
 OR NEW.policy!=OLD.policy OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
 OR (NEW.phase=OLD.phase AND NEW.last_ordinal<OLD.last_ordinal)
 OR (CASE NEW.phase WHEN 'sidecar_documents' THEN 0 WHEN 'sidecar_sources' THEN 1 WHEN 'sidecars' THEN 2 WHEN 'sidecar_heads' THEN 3 ELSE 4 END)
  <(CASE OLD.phase WHEN 'sidecar_documents' THEN 0 WHEN 'sidecar_sources' THEN 1 WHEN 'sidecars' THEN 2 WHEN 'sidecar_heads' THEN 3 ELSE 4 END)
BEGIN SELECT RAISE(ABORT,'catalog document binding and completed progress are immutable'); END;

CREATE TABLE catalog_document_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_document_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 document_uuid TEXT REFERENCES source_documents(uuid),
 source_uuid TEXT REFERENCES source_document_sources(uuid),
 post_uuid TEXT REFERENCES source_posts(uuid),
 claim_uuid TEXT REFERENCES source_document_head_claims(uuid),
 head_uuid TEXT REFERENCES source_document_head_decisions(uuid),
 selection_basis TEXT NOT NULL CHECK(selection_basis IN ('','explicit','legacy_fallback')),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL CHECK(length(reason)<=128),
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK(source_uuid IS NULL OR document_uuid IS NOT NULL),
 CHECK(claim_uuid IS NULL OR source_uuid IS NOT NULL),
 CHECK(head_uuid IS NULL OR claim_uuid IS NOT NULL),
 CHECK(selection_basis!='' OR (claim_uuid IS NULL AND head_uuid IS NULL)),
 CHECK(outcome!='mapped' OR document_uuid IS NOT NULL),
 CHECK(outcome!='mapped' OR selection_basis='' OR (claim_uuid IS NOT NULL AND head_uuid IS NOT NULL)),
 CHECK((outcome='mapped')=(reason=''))
);
CREATE INDEX catalog_document_review ON catalog_document_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_document_record_immutable BEFORE UPDATE ON catalog_document_records
BEGIN SELECT RAISE(ABORT,'catalog document import receipts are immutable'); END;

-- Preserve the historical reader's exact fallback ordering without rescanning
-- every source in the snapshot for each pathname.
CREATE INDEX catalog_snapshot_documents_path ON catalog_snapshot_records(
 snapshot_uuid,source_table,json_extract(data,'$.values.relpath'),
 json_extract(data,'$.values.captured_at') DESC,json_extract(data,'$.values.content_sha256') DESC
) WHERE source_table IN ('sidecars','sidecar_sources');

INSERT INTO native_migration_history(version,name,details)
VALUES(1000042,'Resumable retained catalog document import','{}');
