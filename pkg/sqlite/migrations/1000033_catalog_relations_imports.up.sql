CREATE TABLE catalog_relations_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES catalog_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 unassigned_records INTEGER NOT NULL DEFAULT 0 CHECK(unassigned_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(mapped_records+review_records+unassigned_records=processed_records),
 CHECK(state='running' OR processed_records=source_records)
);
CREATE TRIGGER catalog_relations_import_guard BEFORE UPDATE ON catalog_relations_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at
 OR OLD.state!='running' OR NEW.last_ordinal<OLD.last_ordinal
 OR NEW.processed_records<OLD.processed_records OR NEW.mapped_records<OLD.mapped_records
 OR NEW.review_records<OLD.review_records OR NEW.unassigned_records<OLD.unassigned_records
BEGIN SELECT RAISE(ABORT,'catalog relationship import identity and completed progress are immutable'); END;

CREATE TABLE catalog_relation_records (
 snapshot_uuid TEXT NOT NULL REFERENCES catalog_relations_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 source_table TEXT NOT NULL CHECK(source_table IN ('accounts','handles','posts','post_urls','post_aliases')),
 source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array'),
 data_sha256 TEXT NOT NULL CHECK(length(data_sha256)=64),
 source_values TEXT NOT NULL CHECK(length(CAST(source_values AS BLOB))<=16777216 AND json_valid(source_values) AND json_type(source_values)='object'),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review','unassigned')),
 reason TEXT NOT NULL,
 account_uuid TEXT REFERENCES source_accounts(uuid),
 identifier_uuid TEXT REFERENCES source_account_identifiers(uuid),
 identifier_evidence_key TEXT,
 post_uuid TEXT REFERENCES source_posts(uuid),
 url_evidence_uuid TEXT REFERENCES source_post_url_evidence(uuid),
 post_identifier_evidence_uuid TEXT REFERENCES source_post_identifier_evidence(uuid),
 account_claim_uuid TEXT REFERENCES source_post_account_claims(uuid),
 PRIMARY KEY(snapshot_uuid,ordinal),
 UNIQUE(snapshot_uuid,source_table,source_key),
 FOREIGN KEY(identifier_uuid,identifier_evidence_key) REFERENCES source_account_identifier_evidence(identifier_uuid,evidence_key),
 CHECK((identifier_uuid IS NULL)=(identifier_evidence_key IS NULL)),
 CHECK((outcome='mapped' AND reason='') OR (outcome!='mapped' AND reason!='')),
 CHECK(outcome='mapped' OR (identifier_uuid IS NULL AND url_evidence_uuid IS NULL AND post_identifier_evidence_uuid IS NULL AND account_claim_uuid IS NULL)),
 CHECK(outcome!='unassigned' OR source_table='posts'),
 CHECK(identifier_uuid IS NULL OR (source_table IN ('accounts','handles') AND account_uuid IS NOT NULL)),
 CHECK(url_evidence_uuid IS NULL OR (source_table='post_urls' AND post_uuid IS NOT NULL)),
 CHECK(post_identifier_evidence_uuid IS NULL OR (source_table='post_aliases' AND post_uuid IS NOT NULL)),
 CHECK(account_claim_uuid IS NULL OR (source_table='posts' AND post_uuid IS NOT NULL AND account_uuid IS NOT NULL)),
 CHECK(outcome!='mapped' OR identifier_uuid IS NOT NULL OR url_evidence_uuid IS NOT NULL OR post_identifier_evidence_uuid IS NOT NULL OR account_claim_uuid IS NOT NULL)
);
CREATE INDEX catalog_relation_review ON catalog_relation_records(snapshot_uuid,outcome,ordinal);
CREATE TRIGGER catalog_relation_record_immutable BEFORE UPDATE ON catalog_relation_records
BEGIN SELECT RAISE(ABORT,'catalog relationship import receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000033,'Native mappings and retained review evidence for original catalog relationships','{}');
