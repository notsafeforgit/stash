-- Keep immutable receipt identities and hashes; retire only proven redundant
-- JSON after every catalog consumer has completed without review. No payloads
-- are retired by the migration itself. Batches are explicit and transactional.
ALTER TABLE catalog_snapshot_records ADD COLUMN original_byte_count INTEGER;
-- Preserve the asset reference used by receipt validation after its surrounding
-- source JSON is retired. This value is copied exactly during the transition.
ALTER TABLE catalog_snapshot_records ADD COLUMN retained_asset_id TEXT;
CREATE VIEW catalog_retirable_snapshots AS
SELECT s.uuid FROM catalog_snapshots s
JOIN catalog_evidence_imports evidence ON evidence.snapshot_uuid=s.uuid
JOIN catalog_relations_imports relations ON relations.snapshot_uuid=s.uuid
JOIN catalog_publisher_imports publisher ON publisher.snapshot_uuid=s.uuid
JOIN catalog_attachment_imports attachment ON attachment.snapshot_uuid=s.uuid
JOIN catalog_media_imports media ON media.snapshot_uuid=s.uuid
JOIN catalog_membership_imports membership ON membership.snapshot_uuid=s.uuid
JOIN catalog_document_imports document ON document.snapshot_uuid=s.uuid
JOIN catalog_translation_imports translation ON translation.snapshot_uuid=s.uuid
JOIN catalog_enrichment_imports enrichment ON enrichment.snapshot_uuid=s.uuid
JOIN catalog_file_history_imports file_history ON file_history.snapshot_uuid=s.uuid
JOIN catalog_cleanup_imports cleanup ON cleanup.snapshot_uuid=s.uuid
WHERE s.state='received'
 AND evidence.state='mapped' AND evidence.review_records=0 AND evidence.processed_records=evidence.source_records
 AND relations.state='mapped' AND relations.review_records=0 AND relations.processed_records=relations.source_records
 AND publisher.state='mapped' AND publisher.review_records=0 AND publisher.processed_records=publisher.source_records
 AND attachment.state='mapped' AND attachment.review_records=0 AND attachment.processed_records=attachment.source_records
 AND media.state='mapped' AND media.review_records=0 AND media.processed_records=media.source_records
 AND membership.state='mapped' AND membership.review_records=0 AND membership.processed_records=membership.source_records
 AND document.state='mapped' AND document.review_records=0 AND document.processed_records=document.source_records
 AND translation.state='mapped' AND translation.review_records=0 AND translation.processed_records=translation.source_records
 AND enrichment.state='mapped' AND enrichment.review_records=0 AND enrichment.processed_records=enrichment.source_records
 AND file_history.state='mapped' AND file_history.review_records=0 AND file_history.processed_records=file_history.source_records
 AND cleanup.state='retained' AND cleanup.review_records=0 AND cleanup.processed_records=cleanup.source_records
 AND media.phase='complete' AND document.phase='complete';
CREATE VIEW catalog_retirable_payloads AS
SELECT r.snapshot_uuid,r.ordinal FROM catalog_snapshot_records r
JOIN catalog_retirable_snapshots s ON s.uuid=r.snapshot_uuid
WHERE r.original_byte_count IS NULL AND (
 (r.source_table IN ('account_snapshots','observations','observation_details') AND EXISTS(
  SELECT 1 FROM catalog_evidence_records e WHERE e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
   AND e.source_table=r.source_table AND e.source_key=r.source_key AND e.data_sha256=r.data_sha256 AND e.outcome IN ('mapped','shared')))
 OR (r.source_table IN ('assets','files','appearances') AND EXISTS(
  SELECT 1 FROM catalog_media_records m WHERE m.snapshot_uuid=r.snapshot_uuid AND m.ordinal=r.ordinal AND m.outcome='mapped'))
);
CREATE TABLE catalog_payload_retirements (
 snapshot_uuid TEXT PRIMARY KEY NOT NULL REFERENCES catalog_snapshots(uuid),
 policy TEXT NOT NULL CHECK(policy='catalog-payload-retention-v1'),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 state TEXT NOT NULL CHECK(state IN ('running','complete')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 retired_records INTEGER NOT NULL DEFAULT 0 CHECK(retired_records>=0),
 released_bytes INTEGER NOT NULL DEFAULT 0 CHECK(released_bytes>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TRIGGER catalog_payload_retirement_begin BEFORE INSERT ON catalog_payload_retirements
WHEN NEW.state!='running' OR NEW.last_ordinal!=0 OR NEW.retired_records!=0 OR NEW.released_bytes!=0
 OR NOT EXISTS(SELECT 1 FROM catalog_retirable_snapshots WHERE uuid=NEW.snapshot_uuid)
 OR NEW.manifest_sha256 IS NOT (SELECT manifest_sha256 FROM catalog_snapshots WHERE uuid=NEW.snapshot_uuid)
BEGIN SELECT RAISE(ABORT,'catalog payload retirement requires completed imports'); END;
CREATE TRIGGER catalog_payload_retirement_progress BEFORE UPDATE ON catalog_payload_retirements
WHEN NEW.snapshot_uuid IS NOT OLD.snapshot_uuid OR NEW.policy IS NOT OLD.policy
 OR NEW.manifest_sha256 IS NOT OLD.manifest_sha256 OR NEW.created_at IS NOT OLD.created_at
 OR OLD.state='complete' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.retired_records<OLD.retired_records OR NEW.released_bytes<OLD.released_bytes
 OR NEW.last_ordinal>(SELECT received_records FROM catalog_snapshots WHERE uuid=NEW.snapshot_uuid)
 OR (NEW.last_ordinal>OLD.last_ordinal AND EXISTS(SELECT 1 FROM catalog_retirable_payloads
     WHERE snapshot_uuid=NEW.snapshot_uuid AND ordinal>OLD.last_ordinal AND ordinal<=NEW.last_ordinal))
 OR (NEW.state='complete' AND (NEW.last_ordinal!=(SELECT received_records FROM catalog_snapshots WHERE uuid=NEW.snapshot_uuid)
     OR EXISTS(SELECT 1 FROM catalog_retirable_payloads WHERE snapshot_uuid=NEW.snapshot_uuid)))
BEGIN SELECT RAISE(ABORT,'invalid catalog payload retirement checkpoint'); END;
CREATE TRIGGER catalog_payload_retirement_retained BEFORE DELETE ON catalog_payload_retirements
BEGIN SELECT RAISE(ABORT,'catalog payload retirement receipts are retained'); END;
DROP TRIGGER catalog_snapshot_record_immutable;
CREATE TRIGGER catalog_snapshot_record_immutable BEFORE UPDATE ON catalog_snapshot_records
WHEN NOT (
 OLD.original_byte_count IS NULL AND NEW.original_byte_count IS OLD.byte_count
 AND NEW.retained_asset_id IS json_extract(OLD.data,'$.values.asset_id')
 AND NEW.data='{}' AND NEW.byte_count=2
 AND NEW.snapshot_uuid IS OLD.snapshot_uuid AND NEW.ordinal IS OLD.ordinal
 AND NEW.chunk_index IS OLD.chunk_index AND NEW.source_table IS OLD.source_table
 AND NEW.source_key IS OLD.source_key AND NEW.data_sha256 IS OLD.data_sha256
 AND EXISTS(SELECT 1 FROM catalog_payload_retirements p WHERE p.snapshot_uuid=OLD.snapshot_uuid
   AND p.state='running' AND p.last_ordinal<OLD.ordinal)
 AND EXISTS(SELECT 1 FROM catalog_retirable_payloads p WHERE p.snapshot_uuid=OLD.snapshot_uuid AND p.ordinal=OLD.ordinal)
)
BEGIN SELECT RAISE(ABORT,'staged catalog source records are immutable'); END;
CREATE TRIGGER catalog_payload_retirement_count AFTER UPDATE OF original_byte_count ON catalog_snapshot_records
BEGIN UPDATE catalog_payload_retirements SET retired_records=retired_records+1,
 released_bytes=released_bytes+NEW.original_byte_count-NEW.byte_count WHERE snapshot_uuid=NEW.snapshot_uuid; END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000132,'Resumable retention of consumed catalog payloads with durable receipts','{}');
