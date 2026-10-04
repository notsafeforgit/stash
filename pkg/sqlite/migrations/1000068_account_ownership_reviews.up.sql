-- Explicit account owners do not imply depicted performers or merge accounts.
CREATE TABLE account_ownership_reviews (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid),
 decision_uuid TEXT NOT NULL UNIQUE,
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=16384),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(account_uuid,decision_uuid) REFERENCES account_performer_decisions(account_uuid,uuid)
);
CREATE INDEX account_ownership_reviews_account ON account_ownership_reviews(account_uuid,request_uuid);
CREATE TRIGGER account_ownership_review_immutable BEFORE UPDATE ON account_ownership_reviews
BEGIN SELECT RAISE(ABORT,'account ownership review receipts are immutable'); END;
CREATE TRIGGER account_ownership_review_scope BEFORE INSERT ON account_ownership_reviews
WHEN NOT EXISTS(SELECT 1 FROM account_performer_decisions d
 JOIN account_performer_links h ON h.account_uuid=d.account_uuid AND h.decision_uuid=d.uuid
 JOIN source_accounts a ON a.uuid=d.account_uuid AND a.canonical_uuid=a.uuid
 WHERE d.uuid=NEW.decision_uuid AND d.account_uuid=NEW.account_uuid AND d.origin='review'
 AND a.revision=d.revision
 AND json_extract(NEW.request_json,'$.request_uuid') IS NEW.request_uuid
 AND json_extract(NEW.request_json,'$.account_uuid') IS d.account_uuid
 AND json_extract(NEW.request_json,'$.account_revision') IS d.revision-1
 AND json_extract(NEW.request_json,'$.state') IS d.state
 AND coalesce(json_extract(NEW.request_json,'$.reason'),'') IS d.reason
 AND json_extract(NEW.request_json,'$.performer_uuid') IS d.performer_uuid)
BEGIN SELECT RAISE(ABORT,'account ownership review is outside its decision scope'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000068,'Native account ownership review and retry receipts','{}');
