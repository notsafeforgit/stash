-- Historical assertions have no authority to remove files or change selected metadata.
CREATE TABLE source_file_history (
 uuid TEXT PRIMARY KEY NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('metadata_edit','state_change','deduplication')),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 root_uuid TEXT NOT NULL,
 root_revision INTEGER NOT NULL,
 reference_namespace TEXT NOT NULL CHECK(length(reference_namespace) BETWEEN 1 AND 128),
 reference_value TEXT NOT NULL CHECK(length(reference_value) BETWEEN 1 AND 4096),
 source_time TEXT NOT NULL CHECK(length(source_time)<=64),
 origin TEXT NOT NULL CHECK(origin IN ('migration','ingest','review','scan')),
 observed_at DATETIME NOT NULL,
 details TEXT NOT NULL CHECK(json_valid(details) AND json_type(details)='object' AND length(CAST(details AS BLOB))<=65536),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 location_count INTEGER NOT NULL CHECK(location_count BETWEEN 1 AND 1024),
 edit_count INTEGER NOT NULL CHECK(edit_count BETWEEN 0 AND 256),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 CHECK((kind='metadata_edit' AND location_count=1 AND edit_count>0)
  OR (kind='state_change' AND location_count=1 AND edit_count=0)
  OR (kind='deduplication' AND location_count>=2 AND edit_count=0)),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision)
);
CREATE INDEX source_file_history_reference ON source_file_history(reference_namespace,reference_value,uuid);
CREATE INDEX source_file_history_collection ON source_file_history(collection_uuid,collection_revision,uuid);
CREATE TRIGGER source_file_history_immutable BEFORE UPDATE ON source_file_history
BEGIN SELECT RAISE(ABORT,'source file history is immutable'); END;

CREATE TABLE source_file_history_locations (
 history_uuid TEXT NOT NULL REFERENCES source_file_history(uuid),
 position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 1023),
 relative_path TEXT NOT NULL CHECK(length(relative_path) BETWEEN 1 AND 4096),
 archive_path TEXT CHECK(archive_path IS NULL OR length(archive_path) BETWEEN 1 AND 4096),
 observation_uuid TEXT REFERENCES source_file_observations(uuid),
 PRIMARY KEY(history_uuid,position)
);
CREATE INDEX source_file_history_observation ON source_file_history_locations(observation_uuid,history_uuid);
CREATE TRIGGER source_file_history_location_immutable BEFORE UPDATE ON source_file_history_locations
BEGIN SELECT RAISE(ABORT,'source file history locations are immutable'); END;
CREATE TRIGGER source_file_history_location_scope BEFORE INSERT ON source_file_history_locations
WHEN NOT EXISTS(SELECT 1 FROM source_file_history h WHERE h.uuid=NEW.history_uuid AND NEW.position<h.location_count
 AND (NEW.observation_uuid IS NOT NULL OR h.kind='deduplication')
 AND (NEW.observation_uuid IS NULL OR EXISTS(SELECT 1 FROM source_file_observations o WHERE o.uuid=NEW.observation_uuid
 AND o.collection_uuid=h.collection_uuid AND o.collection_revision=h.collection_revision
 AND o.root_uuid=h.root_uuid AND o.root_revision=h.root_revision
 AND o.relative_path=NEW.relative_path AND o.archive_path IS NEW.archive_path)))
BEGIN SELECT RAISE(ABORT,'source file history location is outside its scope'); END;

CREATE TABLE source_file_history_edits (
 history_uuid TEXT NOT NULL REFERENCES source_file_history(uuid),
 field TEXT NOT NULL CHECK(length(field) BETWEEN 1 AND 128),
 target_field TEXT NOT NULL CHECK(length(target_field)<=128),
 value_type TEXT NOT NULL CHECK(value_type IN ('string','date','urls','names','extension')),
 mode TEXT NOT NULL CHECK(mode IN ('set','inherit','unmapped')),
 value_json TEXT NOT NULL CHECK(json_valid(value_json) AND length(CAST(value_json AS BLOB))<=4194304),
 PRIMARY KEY(history_uuid,field),
 CHECK((mode='unmapped' AND target_field='' AND value_type='extension') OR (mode!='unmapped' AND target_field!='' AND value_type!='extension')),
 CHECK(mode!='inherit' OR value_json='null')
);
CREATE TRIGGER source_file_history_edit_immutable BEFORE UPDATE ON source_file_history_edits
BEGIN SELECT RAISE(ABORT,'source file edit history is immutable'); END;
CREATE TRIGGER source_file_history_edit_scope BEFORE INSERT ON source_file_history_edits
WHEN NOT EXISTS(SELECT 1 FROM source_file_history h WHERE h.uuid=NEW.history_uuid AND h.kind='metadata_edit'
 AND h.edit_count>(SELECT count(*) FROM source_file_history_edits e WHERE e.history_uuid=h.uuid))
BEGIN SELECT RAISE(ABORT,'source metadata choice requires its history event'); END;

CREATE TABLE source_file_history_states (
 history_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_file_history(uuid),
 old_state TEXT NOT NULL CHECK(old_state IN ('present','missing','pending','deduplicated')),
 new_state TEXT NOT NULL CHECK(new_state IN ('present','missing','pending','deduplicated')),
 reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 4096)
);
CREATE TRIGGER source_file_history_state_immutable BEFORE UPDATE ON source_file_history_states
BEGIN SELECT RAISE(ABORT,'source file state history is immutable'); END;
CREATE TRIGGER source_file_history_state_scope BEFORE INSERT ON source_file_history_states
WHEN NOT EXISTS(SELECT 1 FROM source_file_history WHERE uuid=NEW.history_uuid AND kind='state_change')
BEGIN SELECT RAISE(ABORT,'source state change requires its history event'); END;

CREATE TABLE source_file_history_deduplications (
 history_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_file_history(uuid),
 content_claim_uuid TEXT NOT NULL REFERENCES source_content_claims(uuid),
 stage TEXT NOT NULL CHECK(stage IN ('prepared','finished')),
 survivor_path TEXT CHECK(survivor_path IS NULL OR length(survivor_path) BETWEEN 1 AND 4096),
 CHECK((stage='prepared' AND survivor_path IS NULL) OR (stage='finished' AND survivor_path IS NOT NULL))
);
CREATE INDEX source_file_history_claim ON source_file_history_deduplications(content_claim_uuid,history_uuid);
CREATE TRIGGER source_file_history_deduplication_immutable BEFORE UPDATE ON source_file_history_deduplications
BEGIN SELECT RAISE(ABORT,'source deduplication history is immutable'); END;
CREATE TRIGGER source_file_history_deduplication_scope BEFORE INSERT ON source_file_history_deduplications
WHEN NOT EXISTS(SELECT 1 FROM source_file_history h JOIN source_content_claims c
 ON c.collection_uuid=h.collection_uuid AND c.collection_revision=h.collection_revision
 WHERE h.uuid=NEW.history_uuid AND h.kind='deduplication' AND c.uuid=NEW.content_claim_uuid)
BEGIN SELECT RAISE(ABORT,'source deduplication claim is outside its history scope'); END;

CREATE TABLE catalog_file_history_imports (
 snapshot_uuid TEXT PRIMARY KEY NOT NULL REFERENCES catalog_media_imports(snapshot_uuid),
 manifest_sha256 TEXT NOT NULL,
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 root_uuid TEXT NOT NULL,
 root_revision INTEGER NOT NULL,
 policy TEXT NOT NULL CHECK(policy='catalog-file-history-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records>=0 AND processed_records<=source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(processed_records=mapped_records+review_records),
 CHECK(state='running' OR processed_records=source_records),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision)
);
CREATE TRIGGER catalog_file_history_import_guard BEFORE UPDATE ON catalog_file_history_imports
WHEN OLD.state!='running' OR NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256
 OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
 OR NEW.root_uuid!=OLD.root_uuid OR NEW.root_revision!=OLD.root_revision OR NEW.policy!=OLD.policy
 OR NEW.created_at!=OLD.created_at OR NEW.source_records!=OLD.source_records
 OR NEW.last_ordinal<OLD.last_ordinal OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'catalog file history progress cannot regress or change scope'); END;

CREATE TABLE catalog_file_history_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_file_history_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL,
 history_uuid TEXT REFERENCES source_file_history(uuid),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK((outcome='mapped' AND history_uuid IS NOT NULL AND reason='') OR (outcome='review' AND reason!=''))
);
CREATE INDEX catalog_file_history_review ON catalog_file_history_records(snapshot_uuid,outcome,ordinal);
CREATE UNIQUE INDEX catalog_file_history_event ON catalog_file_history_records(history_uuid) WHERE history_uuid IS NOT NULL;
CREATE TRIGGER catalog_file_history_record_immutable BEFORE UPDATE ON catalog_file_history_records
BEGIN SELECT RAISE(ABORT,'catalog file history receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000066,'Native source file edits, state and deduplication history with bounded catalog import','{}');
