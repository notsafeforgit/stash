-- Historical catalog groups are not scrape sources. Keep useful names and
-- studio attribution on the posts instead of presenting a disabled source.
CREATE TABLE source_post_import_labels (
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 kind TEXT NOT NULL CHECK(kind IN ('directory','subreddit')),
 label TEXT NOT NULL CHECK(length(CAST(label AS BLOB)) BETWEEN 1 AND 1024),
 namespace TEXT NOT NULL,
 PRIMARY KEY(post_uuid,kind,label,namespace)
) WITHOUT ROWID;

CREATE TABLE source_post_studios (
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 studio_id INTEGER NOT NULL REFERENCES studios(id) ON DELETE CASCADE,
 PRIMARY KEY(post_uuid,studio_id)
) WITHOUT ROWID;
CREATE INDEX source_post_studios_studio ON source_post_studios(studio_id,post_uuid);

ALTER TABLE catalog_snapshots ADD COLUMN memberships_compacted INTEGER NOT NULL DEFAULT 0
 CHECK(memberships_compacted IN (0,1));
CREATE TRIGGER catalog_membership_compaction_guard BEFORE UPDATE OF memberships_compacted ON catalog_snapshots
WHEN NEW.memberships_compacted<OLD.memberships_compacted
 OR (NEW.memberships_compacted=1 AND (NEW.state!='received'
  OR NOT EXISTS(SELECT 1 FROM catalog_membership_imports i WHERE i.snapshot_uuid=NEW.uuid AND i.state='mapped' AND i.review_records=0)
  OR EXISTS(SELECT 1 FROM catalog_membership_records r WHERE r.snapshot_uuid=NEW.uuid)
  OR EXISTS(SELECT 1 FROM catalog_snapshot_records r WHERE r.snapshot_uuid=NEW.uuid AND r.source_table='memberships')))
BEGIN SELECT RAISE(ABORT,'Membership inputs can only be discarded after a completed import'); END;

-- Keep FK checks bounded while removing completed import rows and groups.
CREATE INDEX catalog_membership_records_collection ON catalog_membership_records(collection_uuid);
CREATE INDEX catalog_membership_records_membership ON catalog_membership_records(membership_uuid);
CREATE INDEX translation_targets_collection ON translation_targets(collection_uuid);
CREATE INDEX automation_translation_records_collection ON automation_translation_records(collection_uuid);
CREATE INDEX automation_enrichment_records_collection ON automation_enrichment_records(collection_uuid);
CREATE INDEX automation_enrichment_records_alias ON automation_enrichment_records(catalog_snapshot_uuid,alias_ordinal);
CREATE INDEX automation_discovery_records_collection ON automation_discovery_records(collection_uuid);
CREATE INDEX ingest_receipts_collection ON ingest_receipts(collection_uuid);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000121,'Remove unused historical catalog groups after preserving post data','{}');
