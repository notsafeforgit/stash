-- Candidate validation needs the canonical account first, whereas global ID
-- lookup uses the existing namespace/kind/value-leading index.
CREATE INDEX source_account_identifiers_canonical_kind ON source_account_identifiers(canonical_uuid,namespace,kind,value);

-- Publisher identity is a choice about one capture's source account, never a
-- depicted performer or an ownership decision. Original account UUIDs survive
-- consolidation; queries follow their canonical account without rewriting facts.
CREATE TABLE capture_publisher_decisions (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK(length(uuid)=36 AND uuid=lower(uuid)
      AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
      AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
      AND length(replace(uuid,'-',''))=32
      AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
      AND uuid!='00000000-0000-0000-0000-000000000000'),
  capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
  revision INTEGER NOT NULL CHECK(revision>0),
  state TEXT NOT NULL CHECK(state IN ('linked','unlinked','undecided')),
  account_uuid TEXT REFERENCES source_accounts(uuid),
  origin TEXT NOT NULL CHECK(origin IN ('capture','review','migration')),
  policy TEXT NOT NULL CHECK(length(policy) BETWEEN 1 AND 128),
  reason TEXT NOT NULL CHECK(length(reason)<=4096),
  request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK((state='linked' AND account_uuid IS NOT NULL) OR (state!='linked' AND account_uuid IS NULL)),
  UNIQUE(capture_uuid,revision),
  UNIQUE(capture_uuid,uuid),
  FOREIGN KEY(capture_uuid) REFERENCES capture_publisher_heads(capture_uuid) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX capture_publisher_decisions_account ON capture_publisher_decisions(account_uuid,capture_uuid) WHERE account_uuid IS NOT NULL;
CREATE TABLE capture_publisher_heads (
  capture_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_captures(uuid),
  decision_uuid TEXT NOT NULL,
  FOREIGN KEY(capture_uuid,decision_uuid) REFERENCES capture_publisher_decisions(capture_uuid,uuid)
);
CREATE TRIGGER capture_publisher_decision_scope BEFORE INSERT ON capture_publisher_decisions
BEGIN
  SELECT RAISE(ABORT,'publisher revision is stale') WHERE NEW.revision!=coalesce(
    (SELECT d.revision+1 FROM capture_publisher_heads h JOIN capture_publisher_decisions d ON d.uuid=h.decision_uuid WHERE h.capture_uuid=NEW.capture_uuid),1);
  SELECT RAISE(ABORT,'forgotten post cannot receive publisher decisions') WHERE EXISTS(
    SELECT 1 FROM source_captures c JOIN source_posts p ON p.uuid=c.post_uuid WHERE c.uuid=NEW.capture_uuid AND p.state!='active');
  SELECT RAISE(ABORT,'publisher choice requires a canonical account') WHERE EXISTS(
    SELECT 1 FROM source_accounts a WHERE a.uuid=NEW.account_uuid AND a.uuid!=a.canonical_uuid);
END;
CREATE TRIGGER capture_publisher_decision_publish AFTER INSERT ON capture_publisher_decisions
BEGIN
  INSERT INTO capture_publisher_heads(capture_uuid,decision_uuid) VALUES(NEW.capture_uuid,NEW.uuid)
    ON CONFLICT(capture_uuid) DO UPDATE SET decision_uuid=excluded.decision_uuid;
  UPDATE source_posts SET revision=revision+1 WHERE uuid=(SELECT post_uuid FROM source_captures WHERE uuid=NEW.capture_uuid);
END;
CREATE TRIGGER capture_publisher_decision_immutable BEFORE UPDATE ON capture_publisher_decisions
BEGIN SELECT RAISE(ABORT,'publisher decisions are immutable'); END;
CREATE TRIGGER capture_publisher_head_scope BEFORE INSERT ON capture_publisher_heads
WHEN (SELECT revision FROM capture_publisher_decisions WHERE uuid=NEW.decision_uuid)!=(SELECT max(revision) FROM capture_publisher_decisions WHERE capture_uuid=NEW.capture_uuid)
BEGIN SELECT RAISE(ABORT,'publisher head requires the latest decision'); END;
CREATE TRIGGER capture_publisher_head_forward BEFORE UPDATE ON capture_publisher_heads
WHEN NEW.capture_uuid!=OLD.capture_uuid OR (NEW.decision_uuid!=OLD.decision_uuid AND
  (SELECT revision FROM capture_publisher_decisions WHERE uuid=NEW.decision_uuid)<=(SELECT revision FROM capture_publisher_decisions WHERE uuid=OLD.decision_uuid))
BEGIN SELECT RAISE(ABORT,'publisher head cannot move backwards'); END;

-- These references identify exactly which account claims a decision recorded;
-- the payload and profile bodies stay shared in source capture storage.
CREATE TABLE capture_publisher_claims (
  decision_uuid TEXT NOT NULL REFERENCES capture_publisher_decisions(uuid),
  identifier_uuid TEXT NOT NULL,
  evidence_key TEXT NOT NULL,
  PRIMARY KEY(decision_uuid,identifier_uuid,evidence_key),
  FOREIGN KEY(identifier_uuid,evidence_key) REFERENCES source_account_identifier_evidence(identifier_uuid,evidence_key)
);
CREATE INDEX capture_publisher_claims_identifier ON capture_publisher_claims(identifier_uuid,evidence_key);
CREATE TRIGGER capture_publisher_claim_scope BEFORE INSERT ON capture_publisher_claims
WHEN NOT EXISTS(SELECT 1 FROM capture_publisher_decisions d JOIN source_account_identifiers i ON i.account_uuid=d.account_uuid
WHERE d.uuid=NEW.decision_uuid AND d.state='linked' AND i.uuid=NEW.identifier_uuid)
BEGIN SELECT RAISE(ABORT,'publisher claim belongs to a different account'); END;
CREATE TRIGGER capture_publisher_claim_immutable BEFORE UPDATE ON capture_publisher_claims
BEGIN SELECT RAISE(ABORT,'publisher claims are immutable'); END;

-- A late failure must not leave a newly allocated account or half its claims
-- committed when a caller accidentally ignores the error.
CREATE TABLE capture_publisher_write_context (
  request_uuid TEXT NOT NULL PRIMARY KEY,
  capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid)
);
INSERT INTO native_migration_history(version,name,details)
VALUES(1000016,'Capture publisher identity decisions and linked account evidence','{}');
