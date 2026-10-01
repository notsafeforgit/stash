PRAGMA defer_foreign_keys=ON;
ALTER TABLE source_accounts ADD COLUMN canonical_uuid TEXT REFERENCES source_accounts(uuid);
UPDATE source_accounts SET canonical_uuid=uuid;
CREATE UNIQUE INDEX source_accounts_identity_pair ON source_accounts(uuid,canonical_uuid);
CREATE INDEX source_accounts_canonical ON source_accounts(canonical_uuid,uuid);

-- Canonical identity is indexed alongside the original evidence owner. The
-- composite foreign key makes this a checked lookup index, not independent
-- identity state. Consolidation changes cascade to it in the same transaction.
CREATE TABLE native_account_identifier_rows AS SELECT * FROM source_account_identifiers;
DROP TABLE source_account_identifiers;
CREATE TABLE source_account_identifiers (
  uuid TEXT NOT NULL PRIMARY KEY,
  account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid),
  canonical_uuid TEXT NOT NULL,
  namespace TEXT NOT NULL CHECK(length(namespace) BETWEEN 1 AND 128),
  kind TEXT NOT NULL CHECK(length(kind) BETWEEN 1 AND 32),
  value TEXT NOT NULL CHECK(length(value) BETWEEN 1 AND 2048),
  UNIQUE(account_uuid,namespace,kind,value),
  FOREIGN KEY(account_uuid,canonical_uuid) REFERENCES source_accounts(uuid,canonical_uuid) ON UPDATE CASCADE
);
INSERT INTO source_account_identifiers(uuid,account_uuid,canonical_uuid,namespace,kind,value)
SELECT uuid,account_uuid,account_uuid,namespace,kind,value FROM native_account_identifier_rows;
DROP TABLE native_account_identifier_rows;
CREATE INDEX source_account_identifiers_lookup ON source_account_identifiers(namespace,kind,value,account_uuid);
CREATE INDEX source_account_identifiers_account ON source_account_identifiers(account_uuid,uuid);
CREATE INDEX source_account_identifiers_canonical ON source_account_identifiers(namespace,kind,value,canonical_uuid);
-- Keep original records, evidence and ownership decisions. A consolidation is
-- a reviewed redirect between duplicate records in the same service namespace.
CREATE TABLE source_account_consolidations (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
    AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
    AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND uuid!='00000000-0000-0000-0000-000000000000'),
  source_uuid TEXT NOT NULL UNIQUE REFERENCES source_accounts(uuid),
  destination_uuid TEXT NOT NULL REFERENCES source_accounts(uuid),
  source_revision INTEGER NOT NULL CHECK(source_revision>0),
  destination_revision INTEGER NOT NULL CHECK(destination_revision>0),
  ownership_decision_uuid TEXT NOT NULL,
  signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
  request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
  origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
  reason TEXT NOT NULL CHECK(length(reason)<=4096),
  accepted_identifier_conflicts BOOLEAN NOT NULL CHECK(accepted_identifier_conflicts IN (0,1)),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK(source_uuid!=destination_uuid),
  UNIQUE(destination_uuid,destination_revision),
  FOREIGN KEY(destination_uuid,ownership_decision_uuid) REFERENCES account_performer_decisions(account_uuid,uuid)
);
CREATE INDEX source_account_consolidations_destination ON source_account_consolidations(destination_uuid,sequence);
CREATE TABLE source_account_consolidation_context (
  source_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_accounts(uuid),
  destination_uuid TEXT NOT NULL REFERENCES source_accounts(uuid),
  CHECK(source_uuid!=destination_uuid)
);
CREATE TRIGGER source_account_consolidation_scope BEFORE INSERT ON source_account_consolidations
BEGIN
  SELECT RAISE(ABORT,'account consolidation requires its write context') WHERE NOT EXISTS(
    SELECT 1 FROM source_account_consolidation_context WHERE source_uuid=NEW.source_uuid AND destination_uuid=NEW.destination_uuid
  );
  SELECT RAISE(ABORT,'account consolidation requires current roots in one namespace') WHERE NOT EXISTS(
    SELECT 1 FROM source_accounts s JOIN source_accounts d ON s.namespace=d.namespace
    WHERE s.uuid=NEW.source_uuid AND d.uuid=NEW.destination_uuid
    AND s.revision=NEW.source_revision AND d.revision=NEW.destination_revision
    AND s.canonical_uuid=s.uuid AND d.canonical_uuid=d.uuid
  ) OR EXISTS(SELECT 1 FROM source_account_consolidations WHERE source_uuid IN (NEW.source_uuid,NEW.destination_uuid));
  SELECT RAISE(ABORT,'account consolidation requires its selected ownership') WHERE NOT EXISTS(
    SELECT 1 FROM account_performer_links l JOIN account_performer_decisions d ON d.uuid=l.decision_uuid AND d.account_uuid=l.account_uuid
    WHERE l.account_uuid=NEW.destination_uuid AND l.decision_uuid=NEW.ownership_decision_uuid AND d.revision=NEW.destination_revision
  );
END;
CREATE TRIGGER source_account_consolidation_publish AFTER INSERT ON source_account_consolidations
BEGIN
  UPDATE source_accounts SET canonical_uuid=NEW.destination_uuid WHERE canonical_uuid=NEW.source_uuid;
END;
CREATE TRIGGER source_account_root_initial BEFORE INSERT ON source_accounts
WHEN NEW.canonical_uuid IS NOT NULL AND NEW.canonical_uuid!=NEW.uuid
BEGIN SELECT RAISE(ABORT,'new source accounts must begin as independent identities'); END;
CREATE TRIGGER source_account_root_created AFTER INSERT ON source_accounts
WHEN NEW.canonical_uuid IS NULL
BEGIN UPDATE source_accounts SET canonical_uuid=NEW.uuid WHERE uuid=NEW.uuid; END;
CREATE TRIGGER source_account_root_update BEFORE UPDATE OF canonical_uuid ON source_accounts
WHEN NOT (OLD.canonical_uuid IS NULL AND NEW.canonical_uuid=OLD.uuid)
BEGIN
  SELECT RAISE(ABORT,'canonical accounts change only through consolidation') WHERE NEW.canonical_uuid IS NULL OR NOT EXISTS(
    SELECT 1 FROM source_account_consolidation_context w
    JOIN source_account_consolidations c ON c.source_uuid=w.source_uuid AND c.destination_uuid=w.destination_uuid
    JOIN source_accounts d ON d.uuid=w.destination_uuid AND d.canonical_uuid=d.uuid AND d.namespace=OLD.namespace
    WHERE w.source_uuid=OLD.canonical_uuid AND w.destination_uuid=NEW.canonical_uuid
  );
END;
CREATE TRIGGER source_account_consolidation_immutable BEFORE UPDATE ON source_account_consolidations
BEGIN SELECT RAISE(ABORT,'account consolidations are immutable'); END;
CREATE TRIGGER source_account_namespace_immutable BEFORE UPDATE OF uuid,namespace ON source_accounts
WHEN NEW.uuid!=OLD.uuid OR NEW.namespace!=OLD.namespace
BEGIN SELECT RAISE(ABORT,'source account identity is immutable'); END;
CREATE TRIGGER source_account_identifier_immutable BEFORE UPDATE ON source_account_identifiers
WHEN NEW.uuid!=OLD.uuid OR NEW.account_uuid!=OLD.account_uuid OR NEW.namespace!=OLD.namespace OR NEW.kind!=OLD.kind OR NEW.value!=OLD.value
  OR (NEW.canonical_uuid!=OLD.canonical_uuid AND EXISTS(SELECT 1 FROM source_accounts WHERE uuid=OLD.account_uuid AND canonical_uuid=OLD.canonical_uuid))
BEGIN SELECT RAISE(ABORT,'source account identifiers are immutable'); END;
CREATE TRIGGER source_account_evidence_immutable BEFORE UPDATE ON source_account_identifier_evidence
WHEN NEW.identifier_uuid!=OLD.identifier_uuid OR NEW.evidence_key!=OLD.evidence_key OR NEW.basis!=OLD.basis
  OR NEW.origin!=OLD.origin OR NEW.details!=OLD.details
  OR NEW.first_observed>OLD.first_observed OR NEW.last_observed<OLD.last_observed
BEGIN SELECT RAISE(ABORT,'source account evidence can only extend its observation interval'); END;
CREATE TRIGGER account_performer_active_root BEFORE INSERT ON account_performer_decisions
WHEN EXISTS(SELECT 1 FROM source_account_consolidations WHERE source_uuid=NEW.account_uuid)
BEGIN SELECT RAISE(ABORT,'ownership edits require the canonical source account'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000014,'Reviewed source account consolidation with retained evidence and ownership history','{}');
