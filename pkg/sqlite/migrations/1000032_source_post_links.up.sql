CREATE TABLE source_post_urls (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 url TEXT NOT NULL CHECK(length(CAST(url AS BLOB)) BETWEEN 1 AND 8192),
 UNIQUE(post_uuid,url)
);
CREATE INDEX source_post_urls_page ON source_post_urls(post_uuid,uuid);
CREATE INDEX source_post_urls_lookup ON source_post_urls(url,post_uuid);
CREATE TRIGGER source_post_url_immutable BEFORE UPDATE ON source_post_urls
BEGIN SELECT RAISE(ABORT,'retained post URLs are immutable'); END;

CREATE TABLE source_post_url_evidence (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 url_uuid TEXT NOT NULL REFERENCES source_post_urls(uuid),
 origin TEXT NOT NULL CHECK(origin IN ('capture','review','migration')),
 basis TEXT NOT NULL CHECK(length(basis) BETWEEN 1 AND 128),
 observed_at DATETIME NOT NULL CHECK(length(observed_at)=30 AND substr(observed_at,11,1)='T' AND substr(observed_at,20,1)='.' AND substr(observed_at,30,1)='Z' AND julianday(observed_at) IS NOT NULL),
 details TEXT NOT NULL CHECK(json_valid(details) AND json_type(details)='object' AND length(CAST(details AS BLOB))<=65536),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*')
);
CREATE INDEX source_post_url_evidence_page ON source_post_url_evidence(url_uuid,uuid);
CREATE TRIGGER source_post_url_evidence_immutable BEFORE UPDATE ON source_post_url_evidence
BEGIN SELECT RAISE(ABORT,'retained post URL evidence is immutable'); END;
CREATE TRIGGER source_post_url_evidence_revision AFTER INSERT ON source_post_url_evidence
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=(SELECT post_uuid FROM source_post_urls WHERE uuid=NEW.url_uuid); END;

CREATE TABLE source_post_identifier_evidence (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 namespace TEXT NOT NULL,
 value TEXT NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('capture','review','migration')),
 basis TEXT NOT NULL CHECK(length(basis) BETWEEN 1 AND 128),
 observed_at DATETIME NOT NULL CHECK(length(observed_at)=30 AND substr(observed_at,11,1)='T' AND substr(observed_at,20,1)='.' AND substr(observed_at,30,1)='Z' AND julianday(observed_at) IS NOT NULL),
 details TEXT NOT NULL CHECK(json_valid(details) AND json_type(details)='object' AND length(CAST(details AS BLOB))<=65536),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 FOREIGN KEY(namespace,value) REFERENCES source_post_identifiers(namespace,value)
);
CREATE INDEX source_post_identifier_evidence_page ON source_post_identifier_evidence(post_uuid,uuid);
CREATE TRIGGER source_post_identifier_evidence_scope BEFORE INSERT ON source_post_identifier_evidence
WHEN NOT EXISTS(SELECT 1 FROM source_post_identifiers i WHERE i.namespace=NEW.namespace AND i.value=NEW.value AND i.post_uuid=NEW.post_uuid)
BEGIN SELECT RAISE(ABORT,'post identifier belongs to a different post'); END;
CREATE TRIGGER source_post_identifier_evidence_immutable BEFORE UPDATE ON source_post_identifier_evidence
BEGIN SELECT RAISE(ABORT,'retained post identifier evidence is immutable'); END;
CREATE TRIGGER source_post_identifier_evidence_revision AFTER INSERT ON source_post_identifier_evidence
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid; END;

CREATE TABLE source_post_account_claims (
 uuid TEXT NOT NULL PRIMARY KEY CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid),
 origin TEXT NOT NULL CHECK(origin IN ('capture','review','migration')),
 basis TEXT NOT NULL CHECK(length(basis) BETWEEN 1 AND 128),
 observed_at DATETIME NOT NULL CHECK(length(observed_at)=30 AND substr(observed_at,11,1)='T' AND substr(observed_at,20,1)='.' AND substr(observed_at,30,1)='Z' AND julianday(observed_at) IS NOT NULL),
 details TEXT NOT NULL CHECK(json_valid(details) AND json_type(details)='object' AND length(CAST(details AS BLOB))<=65536),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*')
);
CREATE INDEX source_post_account_claim_page ON source_post_account_claims(post_uuid,uuid);
CREATE INDEX source_post_account_claim_account ON source_post_account_claims(account_uuid,post_uuid);
CREATE TRIGGER source_post_account_claim_immutable BEFORE UPDATE ON source_post_account_claims
BEGIN SELECT RAISE(ABORT,'retained post account claims are immutable'); END;
CREATE TRIGGER source_post_account_claim_revision AFTER INSERT ON source_post_account_claims
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid; END;

CREATE TRIGGER source_post_url_active_post BEFORE INSERT ON source_post_urls
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'forgotten source post cannot receive URLs'); END;
CREATE TRIGGER source_post_url_evidence_active_post BEFORE INSERT ON source_post_url_evidence
WHEN EXISTS(SELECT 1 FROM source_post_urls u JOIN source_posts p ON p.uuid=u.post_uuid WHERE u.uuid=NEW.url_uuid AND p.state!='active')
BEGIN SELECT RAISE(ABORT,'forgotten source post cannot receive URL evidence'); END;
CREATE TRIGGER source_post_identifier_evidence_active_post BEFORE INSERT ON source_post_identifier_evidence
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'forgotten source post cannot receive identifier evidence'); END;
CREATE TRIGGER source_post_account_claim_active_post BEFORE INSERT ON source_post_account_claims
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'forgotten source post cannot receive account claims'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000032,'Native post URLs, identifier evidence and unselected publisher claims','{}');
