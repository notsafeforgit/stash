CREATE INDEX automation_translation_input ON automation_snapshot_records(snapshot_uuid,ordinal)
WHERE source_table IN ('translation_jobs','translation_targets');

CREATE TABLE automation_translation_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES automation_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 policy TEXT NOT NULL CHECK(policy='automation-translations-v1'),
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
CREATE TRIGGER automation_translation_import_guard BEFORE UPDATE ON automation_translation_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at OR OLD.state!='running'
 OR NEW.last_ordinal<OLD.last_ordinal OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'automation translation input and completed progress are immutable'); END;

CREATE TABLE automation_translation_records (
 snapshot_uuid TEXT NOT NULL REFERENCES automation_translation_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 job_ordinal INTEGER,
 request_uuid TEXT REFERENCES translation_requests(uuid),
 cache_uuid TEXT REFERENCES translation_cache(uuid),
 post_uuid TEXT REFERENCES source_posts(uuid),
 post_reference TEXT NOT NULL DEFAULT '' CHECK(length(post_reference) IN (0,69)),
 collection_uuid TEXT,
 collection_revision INTEGER CHECK(collection_revision IS NULL OR collection_revision=1),
 target_uuid TEXT REFERENCES translation_targets(uuid),
 target_revision INTEGER,
 evidence_uuid TEXT REFERENCES source_translation_evidence(uuid),
 disposition TEXT NOT NULL CHECK(disposition IN ('request','cache','english_original','held','completed','preserved','review')),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES automation_snapshot_records(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,job_ordinal) REFERENCES automation_translation_records(snapshot_uuid,ordinal),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(target_uuid,target_revision) REFERENCES translation_target_history(target_uuid,revision),
 CHECK((target_uuid IS NULL)=(target_revision IS NULL)),
 CHECK((collection_uuid IS NULL)=(collection_revision IS NULL)),
 CHECK((outcome='review')=(reason!='') AND (outcome='review')=(disposition='review')),
 CHECK(cache_uuid IS NULL OR request_uuid IS NOT NULL),
 CHECK(post_uuid IS NULL OR post_reference!=''),
 CHECK(target_uuid IS NULL OR (post_uuid IS NOT NULL AND request_uuid IS NOT NULL AND collection_uuid IS NOT NULL AND job_ordinal IS NOT NULL)),
 CHECK(evidence_uuid IS NULL OR (target_uuid IS NOT NULL AND disposition='completed')),
 CHECK(disposition NOT IN ('held','completed','preserved') OR target_uuid IS NOT NULL),
 CHECK(disposition NOT IN ('cache','english_original','completed') OR cache_uuid IS NOT NULL),
 CHECK(disposition NOT IN ('request','cache','english_original') OR (request_uuid IS NOT NULL AND job_ordinal IS NULL AND post_uuid IS NULL AND target_uuid IS NULL))
);
CREATE INDEX automation_translation_review ON automation_translation_records(snapshot_uuid,outcome,ordinal);
CREATE INDEX automation_translation_target ON automation_translation_records(target_uuid,target_revision) WHERE target_uuid IS NOT NULL;
CREATE TRIGGER automation_translation_record_immutable BEFORE UPDATE ON automation_translation_records
BEGIN SELECT RAISE(ABORT,'automation translation receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000047,'Map retained automation translations into historical completion and held native work','{}');
