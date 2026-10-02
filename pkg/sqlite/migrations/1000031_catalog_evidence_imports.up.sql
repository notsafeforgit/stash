CREATE TABLE catalog_evidence_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records BETWEEN 0 AND processed_records),
 capture_mappings INTEGER NOT NULL DEFAULT 0 CHECK(capture_mappings BETWEEN 0 AND processed_records),
 profile_mappings INTEGER NOT NULL DEFAULT 0 CHECK(profile_mappings BETWEEN 0 AND processed_records),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_evidence_import_guard BEFORE UPDATE ON catalog_evidence_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.review_records<OLD.review_records
 OR NEW.capture_mappings<OLD.capture_mappings OR NEW.profile_mappings<OLD.profile_mappings
BEGIN SELECT RAISE(ABORT,'catalog evidence import identity and completed progress are immutable'); END;
CREATE TABLE catalog_evidence_posts (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_evidence_imports(snapshot_uuid),
 source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array'),
 source_ordinal INTEGER NOT NULL CHECK(source_ordinal>0),
 data_sha256 TEXT NOT NULL CHECK(length(data_sha256)=64),
 post_uuid TEXT REFERENCES source_posts(uuid),
 basis TEXT NOT NULL,
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,source_key)
);
CREATE TRIGGER catalog_evidence_post_immutable BEFORE UPDATE ON catalog_evidence_posts
BEGIN SELECT RAISE(ABORT,'original catalog post mappings are immutable'); END;

CREATE TABLE catalog_evidence_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_evidence_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 source_table TEXT NOT NULL CHECK(source_table IN ('account_snapshots','posts','observations','observation_details')),
 source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array'),
 data_sha256 TEXT NOT NULL CHECK(length(data_sha256)=64),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','shared','review')),
 reason TEXT NOT NULL,
 post_uuid TEXT REFERENCES source_posts(uuid),
 capture_uuid TEXT REFERENCES source_captures(uuid),
 profile_hash TEXT REFERENCES source_profile_bodies(hash),
 header_sha256 TEXT CHECK(header_sha256 IS NULL OR length(header_sha256)=64),
 PRIMARY KEY(snapshot_uuid,ordinal)
);
CREATE INDEX catalog_evidence_review ON catalog_evidence_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_evidence_record_immutable BEFORE UPDATE ON catalog_evidence_records
BEGIN SELECT RAISE(ABORT,'catalog evidence import receipts are immutable'); END;

-- Dependency reads use the staged relation, never a full-library scan or an
-- expanding in-memory list of all captures attached to a post.
CREATE INDEX catalog_snapshot_observation_children ON catalog_snapshot_records(snapshot_uuid,json_extract(data,'$.values.observation_id'),ordinal)
 WHERE source_table='observation_details';
CREATE INDEX catalog_snapshot_post_urls ON catalog_snapshot_records(snapshot_uuid,json_extract(data,'$.values.post_key'),ordinal)
 WHERE source_table='post_urls';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000031,'Native post/profile/capture mappings from staged catalog evidence','{}');
