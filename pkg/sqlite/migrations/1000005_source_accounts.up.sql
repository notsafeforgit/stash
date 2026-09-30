-- Source accounts identify publishers independently of depicted performers.
-- Reused handles and conflicting claims are retained, never made globally
-- unique. Imported decisions and observations can coexist without guessing.
CREATE TABLE source_accounts (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  namespace TEXT NOT NULL CHECK (length(namespace) BETWEEN 1 AND 128),
  label TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX source_accounts_namespace ON source_accounts(namespace, uuid);

CREATE TABLE source_account_identifiers (
  uuid TEXT NOT NULL PRIMARY KEY,
  account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
  namespace TEXT NOT NULL CHECK (length(namespace) BETWEEN 1 AND 128),
  kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 32),
  value TEXT NOT NULL CHECK (length(value) BETWEEN 1 AND 2048),
  UNIQUE (account_uuid, namespace, kind, value)
);
CREATE INDEX source_account_identifiers_lookup ON source_account_identifiers(namespace, kind, value, account_uuid);
CREATE INDEX source_account_identifiers_account ON source_account_identifiers(account_uuid, uuid);

CREATE TABLE source_account_identifier_evidence (
  identifier_uuid TEXT NOT NULL REFERENCES source_account_identifiers(uuid) ON UPDATE CASCADE,
  evidence_key TEXT NOT NULL CHECK (length(evidence_key) BETWEEN 1 AND 512),
  basis TEXT NOT NULL CHECK (length(basis) BETWEEN 1 AND 128),
  origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 128),
  details TEXT NOT NULL CHECK (json_valid(details) AND json_type(details) = 'object'),
  first_observed DATETIME NOT NULL,
  last_observed DATETIME NOT NULL,
  PRIMARY KEY (identifier_uuid, evidence_key),
  CHECK (first_observed <= last_observed)
) WITHOUT ROWID;

-- Choices are append-only; the head references its exact event, so the current
-- decision and its audit history cannot disagree about the selected performer.
CREATE TABLE account_performer_decisions (
  uuid TEXT NOT NULL PRIMARY KEY,
  account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
  revision INTEGER NOT NULL CHECK (revision > 0),
  state TEXT NOT NULL CHECK (state IN ('linked', 'unlinked', 'undecided')),
  performer_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  origin TEXT NOT NULL CHECK (origin IN ('review', 'profile-url', 'migration')),
  reason TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (account_uuid, revision),
  UNIQUE (account_uuid, uuid),
  CHECK ((state = 'linked' AND performer_uuid IS NOT NULL) OR
    (state IN ('unlinked', 'undecided') AND performer_uuid IS NULL))
);
CREATE INDEX account_performer_decisions_performer ON account_performer_decisions(performer_uuid) WHERE performer_uuid IS NOT NULL;
CREATE TABLE account_performer_links (
  account_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
  decision_uuid TEXT NOT NULL,
  FOREIGN KEY (account_uuid, decision_uuid) REFERENCES account_performer_decisions(account_uuid, uuid) ON UPDATE CASCADE
);

CREATE TRIGGER account_performer_decision_kind_insert BEFORE INSERT ON account_performer_decisions
WHEN NEW.performer_uuid IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'account ownership requires a performer identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.performer_uuid AND kind != 'performer');
END;
CREATE TRIGGER account_performer_decision_kind_update BEFORE UPDATE OF performer_uuid ON account_performer_decisions
WHEN NEW.performer_uuid IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'account ownership requires a performer identity')
    WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.performer_uuid AND kind != 'performer');
END;

-- FK cascades may rename an adopted archive UUID. Existing choices otherwise
-- cannot be rewritten: a new event is required. Retired archive UUIDs persist,
-- so an absent old UUID specifically identifies the in-flight FK rename.
CREATE TRIGGER account_performer_decision_immutable BEFORE UPDATE ON account_performer_decisions
WHEN NEW.uuid != OLD.uuid OR NEW.account_uuid != OLD.account_uuid OR NEW.revision != OLD.revision
  OR NEW.state != OLD.state OR NEW.origin != OLD.origin OR NEW.reason != OLD.reason OR NEW.created_at != OLD.created_at
  OR (NEW.performer_uuid IS NOT OLD.performer_uuid AND
    (NEW.performer_uuid IS NULL OR OLD.performer_uuid IS NULL OR EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.performer_uuid)))
BEGIN SELECT RAISE(ABORT, 'account ownership decisions are immutable'); END;

CREATE TRIGGER account_performer_head_forward BEFORE UPDATE OF decision_uuid ON account_performer_links
WHEN NEW.decision_uuid != OLD.decision_uuid
BEGIN
  SELECT RAISE(ABORT, 'account ownership head cannot move backwards')
    WHERE (SELECT revision FROM account_performer_decisions WHERE uuid = NEW.decision_uuid)
       <= (SELECT revision FROM account_performer_decisions WHERE uuid = OLD.decision_uuid);
END;

INSERT INTO native_migration_history(version, name, details)
VALUES (1000005, 'Native source accounts, identifier evidence and ownership decisions', '{}');
