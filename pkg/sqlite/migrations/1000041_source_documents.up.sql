-- Retained documents are source evidence. They do not create library files,
-- infer performers or change selected scene/image metadata.
CREATE TABLE source_document_contents (
 content_sha256 TEXT PRIMARY KEY NOT NULL
 CHECK(length(content_sha256)=64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
 byte_size INTEGER NOT NULL CHECK(byte_size BETWEEN 0 AND 16777216),
 storage_encoding TEXT NOT NULL CHECK(storage_encoding IN ('raw','gzip')),
 data BLOB NOT NULL CHECK(typeof(data)='blob' AND length(data)<=16777216),
 CHECK(storage_encoding!='raw' OR length(data)=byte_size)
);
CREATE TRIGGER source_document_content_immutable BEFORE UPDATE ON source_document_contents
BEGIN SELECT RAISE(ABORT,'source document bytes are immutable'); END;

CREATE TABLE source_documents (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 content_sha256 TEXT NOT NULL REFERENCES source_document_contents(content_sha256),
 encoding TEXT NOT NULL CHECK(length(CAST(encoding AS BLOB))<=128),
 parser TEXT NOT NULL CHECK(length(CAST(parser AS BLOB)) BETWEEN 1 AND 128),
 parse_status TEXT NOT NULL CHECK(length(CAST(parse_status AS BLOB)) BETWEEN 1 AND 128),
 warnings TEXT NOT NULL CHECK(length(CAST(warnings AS BLOB))<=4194304 AND json_valid(warnings) AND json_type(warnings)='array'),
 parsed TEXT NOT NULL CHECK(length(CAST(parsed AS BLOB))<=4194304 AND json_valid(parsed) AND json_type(parsed)='object')
);
CREATE INDEX source_documents_content ON source_documents(content_sha256,uuid);
CREATE TRIGGER source_document_immutable BEFORE UPDATE ON source_documents
BEGIN SELECT RAISE(ABORT,'source document interpretation is immutable'); END;

CREATE TABLE source_document_sources (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 document_uuid TEXT NOT NULL REFERENCES source_documents(uuid),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 relative_path TEXT NOT NULL CHECK(length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 8192 AND instr(relative_path,char(0))=0),
 post_uuid TEXT REFERENCES source_posts(uuid),
 captured_at TEXT NOT NULL CHECK(length(captured_at)<=64),
 origin TEXT NOT NULL CHECK(origin IN ('capture','migration','review')),
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object'),
 recorded_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_document_sources_post ON source_document_sources(post_uuid,uuid);
CREATE INDEX source_document_sources_location ON source_document_sources(collection_uuid,relative_path,uuid);
CREATE INDEX source_document_sources_document ON source_document_sources(document_uuid,uuid);
CREATE TRIGGER source_document_source_immutable BEFORE UPDATE ON source_document_sources
BEGIN SELECT RAISE(ABORT,'source document observations are immutable'); END;
CREATE TRIGGER source_document_source_active BEFORE INSERT ON source_document_sources
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'source post has been forgotten'); END;
CREATE TRIGGER source_document_source_revision AFTER INSERT ON source_document_sources
WHEN NEW.post_uuid IS NOT NULL
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid; END;

CREATE TABLE source_document_head_claims (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 source_uuid TEXT NOT NULL REFERENCES source_document_sources(uuid),
 collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
 relative_path TEXT NOT NULL,
 observed_at TEXT NOT NULL CHECK(length(observed_at)<=64),
 origin TEXT NOT NULL CHECK(origin IN ('capture','migration','review')),
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object'),
 recorded_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX source_document_head_claims_location ON source_document_head_claims(collection_uuid,relative_path,uuid);
CREATE INDEX source_document_head_claims_source ON source_document_head_claims(source_uuid,uuid);
CREATE TRIGGER source_document_head_claim_scope BEFORE INSERT ON source_document_head_claims
WHEN NOT EXISTS(SELECT 1 FROM source_document_sources s WHERE s.uuid=NEW.source_uuid AND s.collection_uuid=NEW.collection_uuid AND s.relative_path=NEW.relative_path)
 OR EXISTS(SELECT 1 FROM source_document_sources s JOIN source_posts p ON p.uuid=s.post_uuid WHERE s.uuid=NEW.source_uuid AND p.state!='active')
BEGIN SELECT RAISE(ABORT,'source document head claim has invalid scope'); END;
CREATE TRIGGER source_document_head_claim_immutable BEFORE UPDATE ON source_document_head_claims
BEGIN SELECT RAISE(ABORT,'source document head evidence is immutable'); END;
CREATE TRIGGER source_document_head_claim_revision AFTER INSERT ON source_document_head_claims
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=(SELECT post_uuid FROM source_document_sources WHERE uuid=NEW.source_uuid); END;

CREATE TABLE source_document_head_decisions (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
 relative_path TEXT NOT NULL CHECK(length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 8192 AND instr(relative_path,char(0))=0),
 revision INTEGER NOT NULL CHECK(revision>0),
 state TEXT NOT NULL CHECK(state IN ('linked','unlinked')),
 source_uuid TEXT REFERENCES source_document_sources(uuid),
 claim_uuid TEXT REFERENCES source_document_head_claims(uuid),
 origin TEXT NOT NULL CHECK(origin IN ('capture','migration','review')),
 reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB))<=4096),
 created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 UNIQUE(collection_uuid,relative_path,revision),
 CHECK((state='linked')=(source_uuid IS NOT NULL)),
 CHECK(claim_uuid IS NULL OR source_uuid IS NOT NULL)
);
CREATE INDEX source_document_head_decisions_source ON source_document_head_decisions(source_uuid,uuid);
CREATE INDEX source_document_head_decisions_claim ON source_document_head_decisions(claim_uuid,uuid);
CREATE TABLE source_document_heads (
 collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
 relative_path TEXT NOT NULL,
 decision_uuid TEXT NOT NULL UNIQUE REFERENCES source_document_head_decisions(uuid),
 PRIMARY KEY(collection_uuid,relative_path)
);
CREATE TRIGGER source_document_head_decision_valid BEFORE INSERT ON source_document_head_decisions
WHEN NEW.revision!=coalesce((SELECT d.revision+1 FROM source_document_heads h JOIN source_document_head_decisions d ON d.uuid=h.decision_uuid
 WHERE h.collection_uuid=NEW.collection_uuid AND h.relative_path=NEW.relative_path),1)
 OR (NEW.source_uuid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM source_document_sources s
 WHERE s.uuid=NEW.source_uuid AND s.collection_uuid=NEW.collection_uuid AND s.relative_path=NEW.relative_path))
 OR (NEW.claim_uuid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM source_document_head_claims c WHERE c.uuid=NEW.claim_uuid AND c.source_uuid=NEW.source_uuid))
 OR EXISTS(SELECT 1 FROM source_document_sources s JOIN source_posts p ON p.uuid=s.post_uuid WHERE s.uuid=NEW.source_uuid AND p.state!='active')
 OR (NEW.origin!='review' AND EXISTS(SELECT 1 FROM source_document_heads h JOIN source_document_head_decisions d ON d.uuid=h.decision_uuid
 WHERE h.collection_uuid=NEW.collection_uuid AND h.relative_path=NEW.relative_path AND (d.origin IN ('review','migration') OR NEW.origin='migration')))
BEGIN SELECT RAISE(ABORT,'source document head decision conflicts'); END;
CREATE TRIGGER source_document_head_decision_immutable BEFORE UPDATE ON source_document_head_decisions
BEGIN SELECT RAISE(ABORT,'source document head decisions are immutable'); END;
CREATE TRIGGER source_document_head_insert_valid BEFORE INSERT ON source_document_heads
WHEN NOT EXISTS(SELECT 1 FROM source_document_head_decisions d WHERE d.uuid=NEW.decision_uuid AND d.collection_uuid=NEW.collection_uuid AND d.relative_path=NEW.relative_path
 AND d.revision=(SELECT max(x.revision) FROM source_document_head_decisions x WHERE x.collection_uuid=NEW.collection_uuid AND x.relative_path=NEW.relative_path))
BEGIN SELECT RAISE(ABORT,'source document head does not select its current decision'); END;
CREATE TRIGGER source_document_head_update_valid BEFORE UPDATE ON source_document_heads
WHEN NEW.collection_uuid!=OLD.collection_uuid OR NEW.relative_path!=OLD.relative_path
 OR NOT EXISTS(SELECT 1 FROM source_document_head_decisions d WHERE d.uuid=NEW.decision_uuid AND d.collection_uuid=NEW.collection_uuid AND d.relative_path=NEW.relative_path
 AND d.revision=(SELECT max(x.revision) FROM source_document_head_decisions x WHERE x.collection_uuid=NEW.collection_uuid AND x.relative_path=NEW.relative_path))
BEGIN SELECT RAISE(ABORT,'source document head does not select its current decision'); END;
CREATE TRIGGER source_document_head_publish AFTER INSERT ON source_document_head_decisions
BEGIN
 INSERT INTO source_document_heads(collection_uuid,relative_path,decision_uuid) VALUES(NEW.collection_uuid,NEW.relative_path,NEW.uuid)
 ON CONFLICT(collection_uuid,relative_path) DO UPDATE SET decision_uuid=excluded.decision_uuid;
 UPDATE source_posts SET revision=revision+1 WHERE uuid IN (
 SELECT s.post_uuid FROM source_document_head_decisions d JOIN source_document_sources s ON s.uuid=d.source_uuid
 WHERE d.collection_uuid=NEW.collection_uuid AND d.relative_path=NEW.relative_path AND d.revision IN (NEW.revision,NEW.revision-1));
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000041,'Shared retained source documents and guarded document head choices','{}');
