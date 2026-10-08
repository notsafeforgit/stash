-- Retain accepted provider values without inferring provenance from an entity's
-- stash IDs. Manual edits and source-post captures remain separate concepts.
CREATE TABLE provider_metadata_imports (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 entity_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 original_entity_uuid TEXT NOT NULL CHECK(length(original_entity_uuid)=36),
 entity_kind TEXT NOT NULL CHECK(entity_kind IN ('scene','performer','studio','tag')),
 entity_revision INTEGER NOT NULL CHECK(entity_revision>0),
 endpoint TEXT NOT NULL CHECK(length(endpoint) BETWEEN 1 AND 4096),
 remote_id TEXT NOT NULL CHECK(length(remote_id) BETWEEN 1 AND 1024),
 operation TEXT NOT NULL CHECK(operation IN ('review','identify','batch')),
 values_json TEXT NOT NULL CHECK(json_valid(values_json) AND json_type(values_json)='object'
  AND length(CAST(values_json AS BLOB)) BETWEEN 3 AND 4194304),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX provider_metadata_imports_entity ON provider_metadata_imports(entity_uuid,sequence);
CREATE TRIGGER provider_metadata_import_scope BEFORE INSERT ON provider_metadata_imports
BEGIN
 SELECT RAISE(ABORT,'provider metadata import requires the current entity revision') WHERE NOT EXISTS (
  SELECT 1 FROM archive_entities WHERE uuid=NEW.entity_uuid AND uuid=NEW.original_entity_uuid
   AND state='active' AND kind=NEW.entity_kind AND revision=NEW.entity_revision);
END;
CREATE TRIGGER provider_metadata_import_immutable BEFORE UPDATE ON provider_metadata_imports
WHEN NEW.sequence!=OLD.sequence OR NEW.uuid!=OLD.uuid OR NEW.original_entity_uuid!=OLD.original_entity_uuid
 OR NEW.entity_kind!=OLD.entity_kind OR NEW.entity_revision!=OLD.entity_revision OR NEW.endpoint!=OLD.endpoint
 OR NEW.remote_id!=OLD.remote_id OR NEW.operation!=OLD.operation OR NEW.values_json!=OLD.values_json
 OR NEW.signature!=OLD.signature OR NEW.created_at!=OLD.created_at
 OR (NEW.entity_uuid!=OLD.entity_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.entity_uuid))
BEGIN SELECT RAISE(ABORT,'provider metadata imports are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000096,'Attributed provider metadata import receipts','{}');
