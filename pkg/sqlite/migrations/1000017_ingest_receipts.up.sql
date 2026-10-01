CREATE TABLE ingest_producers (
  uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
    AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
    AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND uuid!='00000000-0000-0000-0000-000000000000'),
  label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 256),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TRIGGER ingest_producer_immutable BEFORE UPDATE ON ingest_producers
BEGIN SELECT RAISE(ABORT,'ingestion producer identity is immutable'); END;

CREATE TABLE ingest_credentials (
  uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
    AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
    AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND uuid!='00000000-0000-0000-0000-000000000000'),
  producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
  secret_hash TEXT NOT NULL CHECK(length(secret_hash)=64 AND secret_hash NOT GLOB '*[^0-9a-f]*'),
  expires_at DATETIME,
  revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN (0,1)),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(producer_uuid,uuid)
);
CREATE INDEX ingest_credentials_producer ON ingest_credentials(producer_uuid,uuid);
CREATE TRIGGER ingest_credential_immutable BEFORE UPDATE ON ingest_credentials
WHEN NEW.uuid!=OLD.uuid OR NEW.producer_uuid!=OLD.producer_uuid OR NEW.secret_hash!=OLD.secret_hash
  OR NEW.expires_at IS NOT OLD.expires_at OR NEW.created_at!=OLD.created_at OR NEW.revoked<OLD.revoked
BEGIN SELECT RAISE(ABORT,'ingestion credentials may only be revoked'); END;

CREATE TABLE ingest_credential_scopes (
  credential_uuid TEXT NOT NULL REFERENCES ingest_credentials(uuid),
  collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
  root_uuid TEXT REFERENCES media_roots(uuid),
  PRIMARY KEY(credential_uuid,collection_uuid)
);
CREATE TRIGGER ingest_scope_immutable BEFORE UPDATE ON ingest_credential_scopes
BEGIN SELECT RAISE(ABORT,'ingestion scopes are immutable; issue a new credential'); END;

CREATE TABLE ingest_receipts (
  producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
  event_uuid TEXT NOT NULL CHECK(length(event_uuid)=36 AND event_uuid=lower(event_uuid)
    AND substr(event_uuid,9,1)='-' AND substr(event_uuid,14,1)='-' AND substr(event_uuid,19,1)='-' AND substr(event_uuid,24,1)='-'
    AND length(replace(event_uuid,'-',''))=32 AND replace(event_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND event_uuid!='00000000-0000-0000-0000-000000000000'),
  digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
  credential_uuid TEXT NOT NULL,
  collection_uuid TEXT NOT NULL,
  collection_revision INTEGER NOT NULL,
  root_uuid TEXT REFERENCES media_roots(uuid),
  run_uuid TEXT NOT NULL CHECK(length(run_uuid)=36 AND run_uuid=lower(run_uuid)
    AND substr(run_uuid,9,1)='-' AND substr(run_uuid,14,1)='-' AND substr(run_uuid,19,1)='-' AND substr(run_uuid,24,1)='-'
    AND length(replace(run_uuid,'-',''))=32 AND replace(run_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND run_uuid!='00000000-0000-0000-0000-000000000000'),
  kind TEXT NOT NULL CHECK(kind='source.capture'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
  result TEXT NOT NULL CHECK(json_valid(result) AND json_type(result)='object' AND length(result)<=16384),
  committed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(producer_uuid,event_uuid),
  FOREIGN KEY(producer_uuid,credential_uuid) REFERENCES ingest_credentials(producer_uuid,uuid),
  FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
  FOREIGN KEY(credential_uuid,collection_uuid) REFERENCES ingest_credential_scopes(credential_uuid,collection_uuid),
  FOREIGN KEY(post_uuid,capture_uuid) REFERENCES source_captures(post_uuid,uuid)
);
CREATE INDEX ingest_receipts_capture ON ingest_receipts(capture_uuid);
CREATE TRIGGER ingest_receipt_scope BEFORE INSERT ON ingest_receipts
BEGIN
  SELECT RAISE(ABORT,'ingestion receipt root is outside its credential scope') WHERE NEW.root_uuid IS NOT
    (SELECT root_uuid FROM ingest_credential_scopes WHERE credential_uuid=NEW.credential_uuid AND collection_uuid=NEW.collection_uuid);
  SELECT RAISE(ABORT,'ingestion receipt root differs from its collection revision') WHERE NEW.root_uuid IS NOT
    (SELECT root_uuid FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision);
  SELECT RAISE(ABORT,'ingestion receipt requires capture provenance') WHERE NOT EXISTS
    (SELECT 1 FROM source_collection_captures WHERE capture_uuid=NEW.capture_uuid AND collection_uuid=NEW.collection_uuid AND collection_revision=NEW.collection_revision);
END;
CREATE TRIGGER ingest_receipt_immutable BEFORE UPDATE ON ingest_receipts
BEGIN SELECT RAISE(ABORT,'ingestion receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000017,'Scoped ingestion producers and durable capture receipts','{}');
