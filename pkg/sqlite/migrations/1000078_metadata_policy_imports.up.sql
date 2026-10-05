-- One immutable receipt binds each reviewed conversion to both its original
-- effective settings and the exact native policy revision it published.
CREATE TABLE metadata_policy_imports (
  uuid TEXT NOT NULL PRIMARY KEY,
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64),
  plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
  collection_uuid TEXT NOT NULL,
  collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
  policy_revision INTEGER NOT NULL CHECK(policy_revision>0),
  binding TEXT NOT NULL CHECK(json_valid(binding) AND json_type(binding)='object' AND length(CAST(binding AS BLOB))<=1048576),
  plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=524288),
  created_at TEXT NOT NULL,
  FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
  FOREIGN KEY(collection_uuid,policy_revision) REFERENCES metadata_policy_revisions(collection_uuid,revision)
);
CREATE INDEX metadata_policy_import_collection ON metadata_policy_imports(collection_uuid,uuid);
CREATE TRIGGER metadata_policy_import_immutable BEFORE UPDATE ON metadata_policy_imports
BEGIN SELECT RAISE(ABORT,'metadata policy migration receipts are immutable'); END;
CREATE TABLE metadata_policy_import_documents (
  import_uuid TEXT NOT NULL REFERENCES metadata_policy_imports(uuid),
  source_uuid TEXT NOT NULL REFERENCES source_document_sources(uuid),
  head_uuid TEXT NOT NULL REFERENCES source_document_head_decisions(uuid),
  PRIMARY KEY(import_uuid,source_uuid)
);
CREATE TRIGGER metadata_policy_import_document_immutable BEFORE UPDATE ON metadata_policy_import_documents
BEGIN SELECT RAISE(ABORT,'metadata policy migration document references are immutable'); END;
CREATE TRIGGER metadata_policy_import_document_source BEFORE INSERT ON metadata_policy_import_documents
WHEN NOT EXISTS(SELECT 1 FROM source_document_head_decisions h WHERE h.uuid=NEW.head_uuid AND h.source_uuid=NEW.source_uuid AND h.state='linked')
BEGIN SELECT RAISE(ABORT,'metadata policy migration requires a selected document source'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000078,'Reviewed metadata policy migration provenance','{}');
