CREATE TABLE catalog_attachment_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 policy TEXT NOT NULL CHECK(length(policy) BETWEEN 1 AND 128),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 preserved_records INTEGER NOT NULL DEFAULT 0 CHECK(preserved_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 unavailable_records INTEGER NOT NULL DEFAULT 0 CHECK(unavailable_records>=0),
 changed_selections INTEGER NOT NULL DEFAULT 0 CHECK(changed_selections BETWEEN 0 AND mapped_records),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(mapped_records+preserved_records+review_records+unavailable_records=processed_records),
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_attachment_import_guard BEFORE UPDATE ON catalog_attachment_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.mapped_records<OLD.mapped_records
 OR NEW.preserved_records<OLD.preserved_records OR NEW.review_records<OLD.review_records
 OR NEW.unavailable_records<OLD.unavailable_records OR NEW.changed_selections<OLD.changed_selections
BEGIN SELECT RAISE(ABORT,'catalog attachment import identity and completed progress are immutable'); END;

CREATE TABLE catalog_attachment_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_attachment_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 post_uuid TEXT REFERENCES source_posts(uuid),
 capture_uuid TEXT REFERENCES source_captures(uuid),
 manifest_uuid TEXT,
 selection_uuid TEXT,
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','preserved','review','unavailable')),
 reason TEXT NOT NULL CHECK(length(reason)<=128),
 selection_changed INTEGER NOT NULL DEFAULT 0 CHECK(selection_changed IN (0,1)),
 context_json TEXT NOT NULL CHECK(json_valid(context_json) AND json_type(context_json)='object' AND length(CAST(context_json AS BLOB))<=65536),
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_evidence_records(snapshot_uuid,ordinal),
 FOREIGN KEY(post_uuid,capture_uuid) REFERENCES source_captures(post_uuid,uuid),
 FOREIGN KEY(capture_uuid,manifest_uuid) REFERENCES source_capture_attachment_manifests(capture_uuid,manifest_uuid),
 FOREIGN KEY(post_uuid,selection_uuid) REFERENCES post_attachment_decisions(post_uuid,uuid),
 CHECK(capture_uuid IS NULL OR post_uuid IS NOT NULL),
 CHECK(capture_uuid IS NOT NULL OR (outcome='review' AND manifest_uuid IS NULL AND selection_uuid IS NULL)),
 CHECK(outcome NOT IN ('mapped','preserved') OR (manifest_uuid IS NOT NULL AND selection_uuid IS NOT NULL)),
 CHECK((outcome='mapped' AND reason='') OR (outcome!='mapped' AND reason!='')),
 CHECK(selection_changed=0 OR outcome='mapped')
);
CREATE INDEX catalog_attachment_review ON catalog_attachment_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_attachment_record_immutable BEFORE UPDATE ON catalog_attachment_records
BEGIN SELECT RAISE(ABORT,'catalog attachment import receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000035,'Catalog source attachment lists and retained selection outcomes','{}');
