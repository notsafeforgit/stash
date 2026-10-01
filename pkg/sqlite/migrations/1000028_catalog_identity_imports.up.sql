-- One reviewed registry cutover per original database and Stash namespace.
-- Receipt evidence is immutable; current identities/ownership use native tables.
CREATE TABLE catalog_identity_imports (
  uuid TEXT PRIMARY KEY CHECK(length(uuid)=36),
  source_uuid TEXT NOT NULL CHECK(length(source_uuid)=36),
  namespace TEXT NOT NULL CHECK(length(namespace) BETWEEN 1 AND 128),
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
  plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64 AND plan_sha256 NOT GLOB '*[^0-9a-f]*'),
  record_count INTEGER NOT NULL CHECK(record_count BETWEEN 0 AND 10000),
  plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=8388608),
  created_at TEXT NOT NULL,
  UNIQUE(source_uuid,namespace)
);
CREATE TRIGGER catalog_identity_import_immutable BEFORE UPDATE ON catalog_identity_imports
BEGIN SELECT RAISE(ABORT,'catalog identity import receipts are immutable'); END;

CREATE TABLE catalog_identity_import_records (
  id INTEGER PRIMARY KEY,
  import_uuid TEXT NOT NULL REFERENCES catalog_identity_imports(uuid),
  source_table TEXT NOT NULL CHECK(source_table IN ('performer_identities','performer_identity_bindings',
    'performer_account_associations','performer_identity_events','performer_identity_migrations',
    'catalog_metadata_performers','catalog_metadata_accounts')),
  source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array' AND length(source_key)<=8192),
  outcome TEXT NOT NULL CHECK(outcome IN ('copied','mapped','review','superseded')),
  reason TEXT NOT NULL,
  archive_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  account_uuid TEXT REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
  evidence TEXT NOT NULL CHECK(json_valid(evidence) AND json_type(evidence)='object' AND length(CAST(evidence AS BLOB))<=1048576),
  UNIQUE(import_uuid,source_table,source_key)
);
CREATE INDEX catalog_identity_import_page ON catalog_identity_import_records(import_uuid,id);
CREATE INDEX catalog_identity_import_review ON catalog_identity_import_records(import_uuid,outcome,id);
CREATE TRIGGER catalog_identity_record_immutable BEFORE UPDATE ON catalog_identity_import_records
WHEN NEW.id!=OLD.id OR NEW.import_uuid!=OLD.import_uuid OR NEW.source_table!=OLD.source_table
  OR NEW.source_key!=OLD.source_key OR NEW.outcome!=OLD.outcome OR NEW.reason!=OLD.reason OR NEW.evidence!=OLD.evidence
  OR (NEW.archive_uuid IS NOT OLD.archive_uuid AND (OLD.archive_uuid IS NULL OR EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.archive_uuid)))
  OR (NEW.account_uuid IS NOT OLD.account_uuid AND (OLD.account_uuid IS NULL OR EXISTS(SELECT 1 FROM source_accounts WHERE uuid=OLD.account_uuid)))
BEGIN SELECT RAISE(ABORT,'catalog identity import evidence is immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000028,'Reviewed catalog performer UUID and account ownership import','{}');
