CREATE TABLE automation_checkpoint_imports (
 snapshot_uuid TEXT PRIMARY KEY NOT NULL REFERENCES automation_enrichment_imports(snapshot_uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 policy TEXT NOT NULL CHECK(policy='legacy-enrichment-staging-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(mapped_records+review_records=processed_records),
 CHECK(state='running' OR processed_records=source_records),
 CHECK(state!='mapped' OR review_records=0),
 CHECK(state!='review' OR review_records>0)
);
CREATE TRIGGER automation_checkpoint_import_guard BEFORE UPDATE ON automation_checkpoint_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at OR OLD.state!='running'
 OR NEW.last_ordinal<OLD.last_ordinal OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'checkpoint import identity and completed progress are immutable'); END;

CREATE TABLE automation_checkpoint_bodies (
 hash TEXT PRIMARY KEY NOT NULL CHECK(length(hash)=64 AND hash NOT GLOB '*[^0-9a-f]*'),
 body TEXT NOT NULL CHECK(json_valid(body) AND json_type(body)='object' AND length(CAST(body AS BLOB))<=8388608)
);
CREATE TRIGGER automation_checkpoint_body_immutable BEFORE UPDATE ON automation_checkpoint_bodies
BEGIN SELECT RAISE(ABORT,'converted legacy checkpoint bodies are immutable'); END;

CREATE TABLE automation_checkpoint_records (
 snapshot_uuid TEXT NOT NULL REFERENCES automation_checkpoint_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64 AND source_sha256 NOT GLOB '*[^0-9a-f]*'),
 staged_sha256 TEXT CHECK(staged_sha256 IS NULL OR (length(staged_sha256)=64 AND staged_sha256 NOT GLOB '*[^0-9a-f]*')),
 body_sha256 TEXT REFERENCES automation_checkpoint_bodies(hash),
 record_count INTEGER NOT NULL CHECK(record_count BETWEEN 0 AND 256),
 pending_count INTEGER NOT NULL CHECK(pending_count BETWEEN 0 AND 256),
 unresolved_count INTEGER NOT NULL CHECK(unresolved_count BETWEEN 0 AND 256),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES automation_enrichment_records(snapshot_uuid,ordinal),
 CHECK((outcome='mapped')=(reason='')),
 CHECK((outcome='mapped')=(body_sha256 IS NOT NULL)),
 CHECK(outcome!='mapped' OR (staged_sha256 IS NOT NULL AND record_count>0)),
 CHECK(outcome!='review' OR (record_count=0 AND pending_count=0 AND unresolved_count=0))
);
CREATE TRIGGER automation_checkpoint_record_immutable BEFORE UPDATE ON automation_checkpoint_records
BEGIN SELECT RAISE(ABORT,'legacy checkpoint mappings are immutable'); END;
CREATE INDEX automation_checkpoint_review ON automation_checkpoint_records(snapshot_uuid,outcome,ordinal);
CREATE INDEX automation_checkpoint_sources ON automation_checkpoint_records(body_sha256,snapshot_uuid,ordinal);
CREATE INDEX automation_enrichment_staged_input ON automation_snapshot_records(snapshot_uuid,ordinal)
 WHERE source_table='enrichment_jobs' AND coalesce(json_type(data,'$.values.staged_json'),'null')!='null';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000060,'Retain converted legacy enrichment staging with original provenance','{}');
