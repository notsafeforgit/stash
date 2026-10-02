CREATE TABLE catalog_media_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 root_uuid TEXT NOT NULL,
 root_revision INTEGER NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
 library_root_path TEXT NOT NULL CHECK(length(library_root_path) BETWEEN 1 AND 4096),
 policy TEXT NOT NULL CHECK(policy='catalog-media-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 phase TEXT NOT NULL CHECK(phase IN ('assets','files','appearances','complete')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 unavailable_records INTEGER NOT NULL DEFAULT 0 CHECK(unavailable_records>=0),
 matched_files INTEGER NOT NULL DEFAULT 0 CHECK(matched_files BETWEEN 0 AND processed_records),
 media_associations INTEGER NOT NULL DEFAULT 0 CHECK(media_associations BETWEEN 0 AND processed_records),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision),
 CHECK(mapped_records+review_records+unavailable_records=processed_records),
 CHECK((state='running')=(phase!='complete')),
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_media_import_guard BEFORE UPDATE ON catalog_media_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256
 OR NEW.root_uuid!=OLD.root_uuid OR NEW.root_revision!=OLD.root_revision
 OR NEW.collection_revision!=OLD.collection_revision OR NEW.library_root_path!=OLD.library_root_path
 OR NEW.policy!=OLD.policy OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
 OR NEW.unavailable_records<OLD.unavailable_records OR NEW.matched_files<OLD.matched_files
 OR NEW.media_associations<OLD.media_associations
 OR (NEW.phase=OLD.phase AND NEW.last_ordinal<OLD.last_ordinal)
 OR (OLD.phase='files' AND NEW.phase='assets')
 OR (OLD.phase='appearances' AND NEW.phase NOT IN ('appearances','complete'))
BEGIN SELECT RAISE(ABORT,'catalog media binding and completed progress are immutable'); END;

CREATE TABLE catalog_media_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_media_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 claim_uuid TEXT REFERENCES source_content_claims(uuid),
 observation_uuid TEXT REFERENCES source_file_observations(uuid),
 match_uuid TEXT REFERENCES source_file_matches(uuid),
 post_uuid TEXT REFERENCES source_posts(uuid),
 post_file_uuid TEXT REFERENCES source_post_file_evidence(uuid),
 media_evidence_uuid TEXT REFERENCES source_media_evidence(uuid),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review','unavailable')),
 reason TEXT NOT NULL CHECK(length(reason)<=128),
 context_json TEXT NOT NULL CHECK(json_valid(context_json) AND json_type(context_json)='object' AND length(CAST(context_json AS BLOB))<=65536),
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK(match_uuid IS NULL OR observation_uuid IS NOT NULL),
 CHECK(post_file_uuid IS NULL OR (post_uuid IS NOT NULL AND observation_uuid IS NOT NULL)),
 CHECK(media_evidence_uuid IS NULL OR (post_uuid IS NOT NULL AND match_uuid IS NOT NULL)),
 CHECK((outcome='mapped' AND reason='') OR (outcome!='mapped' AND reason!=''))
);
CREATE INDEX catalog_media_review ON catalog_media_records(snapshot_uuid,outcome,ordinal);
CREATE INDEX catalog_media_claim ON catalog_media_records(snapshot_uuid,claim_uuid,ordinal) WHERE observation_uuid IS NOT NULL;
CREATE INDEX catalog_snapshot_record_phase ON catalog_snapshot_records(snapshot_uuid,source_table,ordinal);
CREATE TRIGGER catalog_media_record_immutable BEFORE UPDATE ON catalog_media_records
BEGIN SELECT RAISE(ABORT,'catalog media import receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000038,'Reviewed catalog asset, file and appearance import','{}');
