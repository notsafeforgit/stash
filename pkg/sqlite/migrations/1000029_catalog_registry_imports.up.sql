CREATE TABLE catalog_registry_imports (
 uuid TEXT PRIMARY KEY CHECK(length(uuid)=36),
 source_uuid TEXT NOT NULL UNIQUE CHECK(length(source_uuid)=36),
 identity_import_uuid TEXT NOT NULL REFERENCES catalog_identity_imports(uuid),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64 AND plan_sha256 NOT GLOB '*[^0-9a-f]*'),
 record_count INTEGER NOT NULL CHECK(record_count BETWEEN 0 AND 25000),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=33554432),
 created_at TEXT NOT NULL
);
CREATE TRIGGER catalog_registry_import_immutable BEFORE UPDATE ON catalog_registry_imports
BEGIN SELECT RAISE(ABORT,'registry import receipts are immutable'); END;

CREATE TABLE catalog_registry_import_records (
 id INTEGER PRIMARY KEY,
 import_uuid TEXT NOT NULL REFERENCES catalog_registry_imports(uuid),
 source_table TEXT NOT NULL CHECK(source_table IN ('catalogs','routes','links','account_identifiers','account_identifier_checkpoints','account_profile_urls')),
 source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array' AND length(CAST(source_key AS BLOB))<=32768),
 outcome TEXT NOT NULL CHECK(outcome IN ('copied','mapped','review')),
 reason TEXT NOT NULL,
 account_uuid TEXT REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
 collection_uuid TEXT REFERENCES source_collections(uuid) ON UPDATE CASCADE,
 evidence TEXT NOT NULL CHECK(json_valid(evidence) AND json_type(evidence)='object' AND length(CAST(evidence AS BLOB))<=1048576),
 UNIQUE(import_uuid,source_table,source_key)
);
CREATE INDEX catalog_registry_record_page ON catalog_registry_import_records(import_uuid,id);
CREATE INDEX catalog_registry_record_review ON catalog_registry_import_records(import_uuid,outcome,id);
CREATE TRIGGER catalog_registry_record_immutable BEFORE UPDATE ON catalog_registry_import_records
WHEN NEW.id!=OLD.id OR NEW.import_uuid!=OLD.import_uuid OR NEW.source_table!=OLD.source_table
 OR NEW.source_key!=OLD.source_key OR NEW.outcome!=OLD.outcome OR NEW.reason!=OLD.reason OR NEW.evidence!=OLD.evidence
 OR (NEW.account_uuid IS NOT OLD.account_uuid AND (OLD.account_uuid IS NULL OR EXISTS(SELECT 1 FROM source_accounts WHERE uuid=OLD.account_uuid)))
 OR (NEW.collection_uuid IS NOT OLD.collection_uuid AND (OLD.collection_uuid IS NULL OR EXISTS(SELECT 1 FROM source_collections WHERE uuid=OLD.collection_uuid)))
BEGIN SELECT RAISE(ABORT,'registry import evidence is immutable'); END;

CREATE TABLE catalog_account_mappings (
 source_uuid TEXT NOT NULL,
 account_key TEXT NOT NULL,
 account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
 import_uuid TEXT NOT NULL REFERENCES catalog_registry_imports(uuid),
 PRIMARY KEY(source_uuid,account_key)
);
CREATE TRIGGER catalog_account_mapping_immutable BEFORE UPDATE ON catalog_account_mappings
WHEN NEW.source_uuid!=OLD.source_uuid OR NEW.account_key!=OLD.account_key OR NEW.import_uuid!=OLD.import_uuid
 OR (NEW.account_uuid!=OLD.account_uuid AND EXISTS(SELECT 1 FROM source_accounts WHERE uuid=OLD.account_uuid))
BEGIN SELECT RAISE(ABORT,'original registry account mappings are immutable'); END;
CREATE TABLE catalog_collection_mappings (
 source_uuid TEXT NOT NULL,
 catalog_id TEXT NOT NULL,
 collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid) ON UPDATE CASCADE,
 import_uuid TEXT NOT NULL REFERENCES catalog_registry_imports(uuid),
 PRIMARY KEY(source_uuid,catalog_id)
);
CREATE TRIGGER catalog_collection_mapping_immutable BEFORE UPDATE ON catalog_collection_mappings
WHEN NEW.source_uuid!=OLD.source_uuid OR NEW.catalog_id!=OLD.catalog_id OR NEW.import_uuid!=OLD.import_uuid
 OR (NEW.collection_uuid!=OLD.collection_uuid AND EXISTS(SELECT 1 FROM source_collections WHERE uuid=OLD.collection_uuid))
BEGIN SELECT RAISE(ABORT,'original catalog collection mappings are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000029,'Native accounts and catalog collections from a reviewed registry snapshot','{}');
