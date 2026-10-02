CREATE TABLE source_translations (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 original_sha256 TEXT CHECK(original_sha256 IS NULL OR (length(original_sha256)=64 AND original_sha256 NOT GLOB '*[^0-9a-f]*')),
 original_text TEXT CHECK(original_text IS NULL OR length(CAST(original_text AS BLOB))<=4194304),
 translated_text TEXT NOT NULL CHECK(length(CAST(translated_text AS BLOB))<=4194304),
 source_language TEXT CHECK(source_language IS NULL OR length(CAST(source_language AS BLOB))<=128),
 target_language TEXT CHECK(target_language IS NULL OR length(CAST(target_language AS BLOB))<=128),
 provider TEXT CHECK(provider IS NULL OR length(CAST(provider AS BLOB))<=1024),
 CHECK((original_text IS NULL)=(original_sha256 IS NULL))
);
CREATE TRIGGER source_translation_immutable BEFORE UPDATE ON source_translations
BEGIN SELECT RAISE(ABORT,'retained translation results are immutable'); END;

CREATE TABLE source_translation_evidence (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 translation_uuid TEXT NOT NULL REFERENCES source_translations(uuid),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 collection_uuid TEXT,
 collection_revision INTEGER CHECK(collection_revision IS NULL OR collection_revision>0),
 provenance TEXT NOT NULL CHECK(length(CAST(provenance AS BLOB))<=4096),
 declared_input_hash TEXT CHECK(declared_input_hash IS NULL OR length(CAST(declared_input_hash AS BLOB))<=4096),
 input_hash_algorithm TEXT NOT NULL CHECK(length(CAST(input_hash_algorithm AS BLOB))<=128),
 captured_at TEXT NOT NULL CHECK(length(CAST(captured_at AS BLOB))<=64),
 origin TEXT NOT NULL CHECK(origin IN ('migration','capture','review','worker')),
 details TEXT NOT NULL CHECK(json_valid(details) AND json_type(details)='object' AND length(CAST(details AS BLOB))<=65536),
 recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK((collection_uuid IS NULL)=(collection_revision IS NULL)),
 CHECK((declared_input_hash IS NULL)=(input_hash_algorithm=''))
);
CREATE INDEX source_translation_evidence_post ON source_translation_evidence(post_uuid,uuid);
CREATE INDEX source_translation_evidence_result ON source_translation_evidence(translation_uuid,post_uuid,uuid);
CREATE INDEX source_translation_evidence_collection ON source_translation_evidence(collection_uuid,collection_revision,uuid);
CREATE TRIGGER source_translation_evidence_immutable BEFORE UPDATE ON source_translation_evidence
BEGIN SELECT RAISE(ABORT,'retained translation evidence is immutable'); END;
CREATE TRIGGER source_translation_evidence_active BEFORE INSERT ON source_translation_evidence
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'forgotten source post cannot receive translations'); END;
CREATE TRIGGER source_translation_evidence_revision AFTER INSERT ON source_translation_evidence
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid; END;

CREATE TABLE catalog_translation_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision=1),
 policy TEXT NOT NULL CHECK(policy='catalog-translations-v1'),
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
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_translation_import_guard BEFORE UPDATE ON catalog_translation_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'catalog translation import identity and completed progress are immutable'); END;

CREATE TABLE catalog_translation_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_translation_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 post_uuid TEXT REFERENCES source_posts(uuid),
 translation_uuid TEXT REFERENCES source_translations(uuid),
 evidence_uuid TEXT REFERENCES source_translation_evidence(uuid),
 input_hash_state TEXT NOT NULL CHECK(input_hash_state IN ('','missing','verified','unverifiable','mismatch')),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK((outcome='mapped')=(reason='')),
 CHECK(outcome!='mapped' OR (evidence_uuid IS NOT NULL AND input_hash_state IN ('missing','verified','unverifiable'))),
 CHECK(evidence_uuid IS NULL OR (post_uuid IS NOT NULL AND translation_uuid IS NOT NULL))
);
CREATE INDEX catalog_translation_review ON catalog_translation_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_translation_record_immutable BEFORE UPDATE ON catalog_translation_records
BEGIN SELECT RAISE(ABORT,'catalog translation import receipts are immutable'); END;


INSERT INTO native_migration_history(version,name,details)
VALUES(1000043,'Shared translation results, post provenance and resumable catalog import','{}');
