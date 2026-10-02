CREATE INDEX catalog_evidence_publisher_candidates ON catalog_evidence_records(snapshot_uuid,ordinal)
 WHERE source_table IN ('observations','observation_details') AND outcome!='shared';

CREATE TABLE catalog_publisher_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 policy TEXT NOT NULL CHECK(length(policy) BETWEEN 1 AND 128),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 linked_records INTEGER NOT NULL DEFAULT 0 CHECK(linked_records>=0),
 preserved_records INTEGER NOT NULL DEFAULT 0 CHECK(preserved_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 unavailable_records INTEGER NOT NULL DEFAULT 0 CHECK(unavailable_records>=0),
 created_accounts INTEGER NOT NULL DEFAULT 0 CHECK(created_accounts BETWEEN 0 AND linked_records),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(linked_records+preserved_records+review_records+unavailable_records=processed_records),
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_publisher_import_guard BEFORE UPDATE ON catalog_publisher_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.linked_records<OLD.linked_records
 OR NEW.preserved_records<OLD.preserved_records OR NEW.review_records<OLD.review_records
 OR NEW.unavailable_records<OLD.unavailable_records OR NEW.created_accounts<OLD.created_accounts
BEGIN SELECT RAISE(ABORT,'catalog publisher import identity and completed progress are immutable'); END;

CREATE TABLE catalog_publisher_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_publisher_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 capture_uuid TEXT REFERENCES source_captures(uuid),
 decision_uuid TEXT,
 outcome TEXT NOT NULL CHECK(outcome IN ('linked','preserved','review','unavailable')),
 reason TEXT NOT NULL CHECK(length(reason)<=128),
 created_account INTEGER NOT NULL DEFAULT 0 CHECK(created_account IN (0,1)),
 context_json TEXT NOT NULL CHECK(json_valid(context_json) AND json_type(context_json)='object' AND length(CAST(context_json AS BLOB))<=65536),
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_evidence_records(snapshot_uuid,ordinal),
 FOREIGN KEY(capture_uuid,decision_uuid) REFERENCES capture_publisher_decisions(capture_uuid,uuid),
 CHECK(capture_uuid IS NOT NULL OR (outcome='review' AND decision_uuid IS NULL)),
 CHECK(outcome NOT IN ('linked','preserved') OR decision_uuid IS NOT NULL),
 CHECK((outcome='linked' AND reason='') OR (outcome!='linked' AND reason!='')),
 CHECK(created_account=0 OR outcome='linked')
);
CREATE INDEX catalog_publisher_review ON catalog_publisher_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_publisher_record_immutable BEFORE UPDATE ON catalog_publisher_records
BEGIN SELECT RAISE(ABORT,'catalog publisher import receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000034,'Captured publisher mapping and retained review outcomes for catalog imports','{}');
