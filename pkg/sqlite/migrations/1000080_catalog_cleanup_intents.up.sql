-- Historical cleanup is held evidence, not an instruction to delete a file,
-- cancel a current target, or forget a post recreated after the original prune.
CREATE TABLE source_cleanup_intents (
 uuid TEXT PRIMARY KEY NOT NULL,
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision=1),
 kind TEXT NOT NULL CHECK(kind='background_targets'),
 state TEXT NOT NULL CHECK(state='held'),
 reference_namespace TEXT NOT NULL CHECK(length(CAST(reference_namespace AS BLOB)) BETWEEN 1 AND 256),
 reference_value TEXT NOT NULL CHECK(length(CAST(reference_value AS BLOB)) BETWEEN 1 AND 4096),
 requested_at TEXT NOT NULL CHECK(length(CAST(requested_at AS BLOB)) BETWEEN 1 AND 64),
 origin TEXT NOT NULL CHECK(origin='migration'),
 recorded_at TEXT NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_cleanup_intents_collection ON source_cleanup_intents(collection_uuid,uuid);
CREATE TRIGGER source_cleanup_intent_immutable BEFORE UPDATE ON source_cleanup_intents
BEGIN SELECT RAISE(ABORT,'source cleanup intent is immutable'); END;
CREATE TRIGGER source_cleanup_intent_scope BEFORE INSERT ON source_cleanup_intents
WHEN NOT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision AND origin='migration')
BEGIN SELECT RAISE(ABORT,'historical cleanup requires its original migrated collection'); END;

CREATE TABLE catalog_cleanup_imports (
 snapshot_uuid TEXT PRIMARY KEY NOT NULL REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision=1),
 policy TEXT NOT NULL CHECK(policy='catalog-cleanup-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','retained','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 held_records INTEGER NOT NULL DEFAULT 0 CHECK(held_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK(held_records+review_records=processed_records),
 CHECK(state='running' OR processed_records=source_records),
 CHECK(state!='retained' OR review_records=0),
 CHECK(state!='review' OR review_records>0)
);
CREATE TRIGGER catalog_cleanup_import_guard BEFORE UPDATE ON catalog_cleanup_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.held_records<OLD.held_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'catalog cleanup import identity and completed progress are immutable'); END;

CREATE TABLE catalog_cleanup_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_cleanup_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 intent_uuid TEXT REFERENCES source_cleanup_intents(uuid),
 outcome TEXT NOT NULL CHECK(outcome IN ('held','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK((outcome='held')=(reason='')),
 CHECK((outcome='held')=(intent_uuid IS NOT NULL))
);
CREATE INDEX catalog_cleanup_review ON catalog_cleanup_records(snapshot_uuid,outcome,ordinal);
CREATE UNIQUE INDEX catalog_cleanup_intent_source ON catalog_cleanup_records(intent_uuid) WHERE intent_uuid IS NOT NULL;
CREATE TRIGGER catalog_cleanup_record_immutable BEFORE UPDATE ON catalog_cleanup_records
BEGIN SELECT RAISE(ABORT,'catalog cleanup import records are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000080,'Retained catalog cleanup intent with held native records','{}');
