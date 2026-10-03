CREATE TABLE source_enrichment_receipts (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision=1),
 origin TEXT NOT NULL CHECK(origin='migration'),
 policy TEXT NOT NULL CHECK(policy='catalog-enrichment-v1'),
 source_version INTEGER NOT NULL CHECK(source_version=1),
 completed_at TEXT NOT NULL CHECK(length(CAST(completed_at AS BLOB)) BETWEEN 1 AND 64),
 attachment_links_enriched INTEGER NOT NULL CHECK(attachment_links_enriched>=0),
 unresolved_children INTEGER NOT NULL CHECK(unresolved_children>=0),
 recorded_at TEXT NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_enrichment_receipts_post ON source_enrichment_receipts(post_uuid,uuid);
CREATE INDEX source_enrichment_receipts_collection ON source_enrichment_receipts(collection_uuid,collection_revision,post_uuid,uuid);
CREATE TRIGGER source_enrichment_receipt_immutable BEFORE UPDATE ON source_enrichment_receipts
BEGIN SELECT RAISE(ABORT,'historical enrichment receipts are immutable'); END;
CREATE TRIGGER source_enrichment_receipt_scope BEFORE INSERT ON source_enrichment_receipts
WHEN NOT EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state='active')
 OR NOT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision AND origin='migration')
BEGIN SELECT RAISE(ABORT,'historical enrichment receipt requires an active post and migrated collection'); END;

CREATE TABLE catalog_enrichment_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision=1),
 policy TEXT NOT NULL CHECK(policy='catalog-enrichment-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK(mapped_records+review_records=processed_records),
 CHECK(state='running' OR processed_records=source_records),
 CHECK(state!='mapped' OR review_records=0),
 CHECK(state!='review' OR review_records>0)
);
CREATE TRIGGER catalog_enrichment_import_guard BEFORE UPDATE ON catalog_enrichment_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'catalog enrichment import identity and completed progress are immutable'); END;

CREATE TABLE catalog_enrichment_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_enrichment_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 post_uuid TEXT REFERENCES source_posts(uuid),
 receipt_uuid TEXT REFERENCES source_enrichment_receipts(uuid),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK((outcome='mapped')=(reason='')),
 CHECK((outcome='mapped')=(receipt_uuid IS NOT NULL)),
 CHECK(receipt_uuid IS NULL OR post_uuid IS NOT NULL)
);
CREATE INDEX catalog_enrichment_review ON catalog_enrichment_records(snapshot_uuid,outcome,ordinal);
CREATE INDEX catalog_enrichment_receipt_sources ON catalog_enrichment_records(receipt_uuid,snapshot_uuid,ordinal);
CREATE TRIGGER catalog_enrichment_record_immutable BEFORE UPDATE ON catalog_enrichment_records
BEGIN SELECT RAISE(ABORT,'catalog enrichment import records are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000057,'Historical enrichment receipts and resumable catalog import','{}');
