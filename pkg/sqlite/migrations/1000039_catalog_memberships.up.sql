CREATE TABLE source_collection_post_evidence (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('capture','migration','review')),
 basis TEXT NOT NULL CHECK(length(basis) BETWEEN 1 AND 128),
 observed_at DATETIME NOT NULL,
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object'),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_collection_post_evidence_post ON source_collection_post_evidence(post_uuid,uuid);
CREATE INDEX source_collection_post_evidence_collection ON source_collection_post_evidence(collection_uuid,uuid);
CREATE TRIGGER source_collection_post_evidence_immutable BEFORE UPDATE ON source_collection_post_evidence
BEGIN SELECT RAISE(ABORT,'source collection post evidence is immutable'); END;
CREATE TRIGGER source_collection_post_evidence_active_post BEFORE INSERT ON source_collection_post_evidence
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'source post has been forgotten'); END;
CREATE TRIGGER source_collection_post_evidence_revision AFTER INSERT ON source_collection_post_evidence
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid; END;

-- One historical collection key across the same catalog registry identifies
-- one group, even when its posts were held in several downloaded catalogs.
CREATE TABLE catalog_membership_groups (
 source_uuid TEXT NOT NULL REFERENCES catalog_registry_imports(source_uuid),
 source_key TEXT NOT NULL CHECK(length(CAST(source_key AS BLOB)) BETWEEN 1 AND 4096),
 source_kind TEXT NOT NULL CHECK(length(source_kind) BETWEEN 1 AND 128),
 source_label TEXT NOT NULL CHECK(length(CAST(source_label AS BLOB)) BETWEEN 1 AND 1024),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 first_snapshot_uuid TEXT NOT NULL REFERENCES catalog_snapshots(uuid),
 PRIMARY KEY(source_uuid,source_key),
 UNIQUE(collection_uuid),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE TRIGGER catalog_membership_group_immutable BEFORE UPDATE ON catalog_membership_groups
BEGIN SELECT RAISE(ABORT,'catalog membership group mappings are immutable'); END;

CREATE TABLE catalog_membership_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 policy TEXT NOT NULL CHECK(policy='catalog-membership-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(mapped_records+review_records=processed_records),
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_membership_import_guard BEFORE UPDATE ON catalog_membership_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'catalog membership import identity and completed progress are immutable'); END;

CREATE TABLE catalog_membership_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_membership_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 post_uuid TEXT REFERENCES source_posts(uuid),
 collection_uuid TEXT REFERENCES source_collections(uuid),
 membership_uuid TEXT REFERENCES source_collection_post_evidence(uuid),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK((outcome='mapped')=(reason='')),
 CHECK((outcome='mapped')=(membership_uuid IS NOT NULL)),
 CHECK(membership_uuid IS NULL OR (post_uuid IS NOT NULL AND collection_uuid IS NOT NULL))
);
CREATE INDEX catalog_membership_review ON catalog_membership_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_membership_record_immutable BEFORE UPDATE ON catalog_membership_records
BEGIN SELECT RAISE(ABORT,'catalog membership import receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000039,'Native post collection memberships and historical catalog grouping import','{}');
