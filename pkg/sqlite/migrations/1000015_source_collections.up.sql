-- Logical roots survive mount changes. Definitions and local bindings are
-- revisioned together so a stale review cannot silently switch file authority.
CREATE TABLE media_roots (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK(length(uuid)=36 AND uuid=lower(uuid)
      AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
      AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
      AND length(replace(uuid,'-',''))=32
      AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
      AND uuid!='00000000-0000-0000-0000-000000000000'),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(uuid,revision) REFERENCES media_root_revisions(root_uuid,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE media_root_revisions (
  root_uuid TEXT NOT NULL REFERENCES media_roots(uuid),
  revision INTEGER NOT NULL CHECK(revision>0),
  label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 1024),
  state TEXT NOT NULL CHECK(state IN ('active','disabled','retired')),
  server_path TEXT,
  directory_identity TEXT,
  origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
  reason TEXT NOT NULL CHECK(length(reason)<=4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(root_uuid,revision),
  CHECK((server_path IS NULL AND directory_identity IS NULL) OR
    (server_path IS NOT NULL AND directory_identity IS NOT NULL AND length(server_path) BETWEEN 1 AND 4096 AND length(directory_identity) BETWEEN 1 AND 128))
);
CREATE TRIGGER media_root_revision_scope BEFORE INSERT ON media_root_revisions
BEGIN
  SELECT RAISE(ABORT,'root revision is stale or retired') WHERE NOT EXISTS(
    SELECT 1 FROM media_roots r LEFT JOIN media_root_revisions d ON d.root_uuid=r.uuid AND d.revision=r.revision
    WHERE r.uuid=NEW.root_uuid AND ((d.root_uuid IS NULL AND NEW.revision=1 AND r.revision=1)
      OR (d.state!='retired' AND NEW.revision=r.revision+1))
  );
END;
CREATE TRIGGER media_root_revision_publish AFTER INSERT ON media_root_revisions
BEGIN UPDATE media_roots SET revision=NEW.revision WHERE uuid=NEW.root_uuid; END;
CREATE TRIGGER media_root_revision_immutable BEFORE UPDATE ON media_root_revisions
BEGIN SELECT RAISE(ABORT,'root revisions are immutable'); END;
CREATE TRIGGER media_root_identity_immutable BEFORE UPDATE ON media_roots
WHEN NEW.uuid!=OLD.uuid OR NEW.created_at!=OLD.created_at OR NEW.revision NOT IN (OLD.revision,OLD.revision+1)
  OR NOT EXISTS(SELECT 1 FROM media_root_revisions WHERE root_uuid=NEW.uuid AND revision=NEW.revision)
BEGIN SELECT RAISE(ABORT,'root identity advances only to a recorded revision'); END;

CREATE UNIQUE INDEX source_accounts_collection_scope ON source_accounts(uuid,namespace);
CREATE TABLE source_collections (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK(length(uuid)=36 AND uuid=lower(uuid)
      AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
      AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
      AND length(replace(uuid,'-',''))=32
      AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
      AND uuid!='00000000-0000-0000-0000-000000000000'),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(uuid,revision) REFERENCES source_collection_revisions(collection_uuid,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE source_collection_revisions (
  collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
  revision INTEGER NOT NULL CHECK(revision>0),
  label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 1024),
  kind TEXT NOT NULL CHECK(kind IN ('account','feed','subreddit','search','manual_batch','directory','legacy_catalog','collection')),
  namespace TEXT NOT NULL CHECK(length(namespace)<=128),
  state TEXT NOT NULL CHECK(state IN ('active','disabled','retired')),
  target_url TEXT NOT NULL CHECK(length(target_url)<=8192),
  account_uuid TEXT,
  root_uuid TEXT REFERENCES media_roots(uuid),
  path_prefix TEXT NOT NULL CHECK(length(path_prefix)<=4096),
  origin TEXT NOT NULL CHECK(origin IN ('review','migration','ingest')),
  reason TEXT NOT NULL CHECK(length(reason)<=4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(collection_uuid,revision),
  FOREIGN KEY(account_uuid,namespace) REFERENCES source_accounts(uuid,namespace),
  CHECK((root_uuid IS NULL AND path_prefix='') OR (root_uuid IS NOT NULL AND length(path_prefix)>0))
);
CREATE INDEX source_collection_target ON source_collection_revisions(target_url,collection_uuid,revision) WHERE target_url!='';
CREATE INDEX source_collection_root ON source_collection_revisions(root_uuid,collection_uuid,revision) WHERE root_uuid IS NOT NULL;
CREATE INDEX source_collection_account ON source_collection_revisions(account_uuid,collection_uuid,revision) WHERE account_uuid IS NOT NULL;
CREATE TRIGGER source_collection_revision_scope BEFORE INSERT ON source_collection_revisions
BEGIN
  SELECT RAISE(ABORT,'collection revision is stale or retired') WHERE NOT EXISTS(
    SELECT 1 FROM source_collections c LEFT JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
    WHERE c.uuid=NEW.collection_uuid AND ((d.collection_uuid IS NULL AND NEW.revision=1 AND c.revision=1)
      OR (d.state!='retired' AND NEW.revision=c.revision+1))
  );
END;
CREATE TRIGGER source_collection_revision_publish AFTER INSERT ON source_collection_revisions
BEGIN UPDATE source_collections SET revision=NEW.revision WHERE uuid=NEW.collection_uuid; END;
CREATE TRIGGER source_collection_revision_immutable BEFORE UPDATE ON source_collection_revisions
BEGIN SELECT RAISE(ABORT,'collection revisions are immutable'); END;
CREATE TRIGGER source_collection_identity_immutable BEFORE UPDATE ON source_collections
WHEN NEW.uuid!=OLD.uuid OR NEW.created_at!=OLD.created_at OR NEW.revision NOT IN (OLD.revision,OLD.revision+1)
  OR NOT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=NEW.uuid AND revision=NEW.revision)
BEGIN SELECT RAISE(ABORT,'collection identity advances only to a recorded revision'); END;

-- Captures may be observed through several feeds/targets. Pin the definition
-- actually used, preserving old scopes after a URL, root or account edit.
CREATE TABLE source_collection_captures (
  collection_uuid TEXT NOT NULL,
  collection_revision INTEGER NOT NULL,
  capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(collection_uuid,capture_uuid,collection_revision),
  FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_collection_captures_capture ON source_collection_captures(capture_uuid,collection_uuid);
CREATE TRIGGER source_collection_capture_immutable BEFORE UPDATE ON source_collection_captures
BEGIN SELECT RAISE(ABORT,'collection capture provenance is immutable'); END;

-- Direct scans/purchased media can record intake provenance without making a
-- fictitious account or source post. These are facts, not curated membership.
CREATE TABLE source_collection_media_intake (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK(length(uuid)=36 AND uuid=lower(uuid)
      AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
      AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
      AND length(replace(uuid,'-',''))=32
      AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
      AND uuid!='00000000-0000-0000-0000-000000000000'),
  collection_uuid TEXT NOT NULL,
  collection_revision INTEGER NOT NULL,
  media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  submitted_media_uuid TEXT NOT NULL,
  origin TEXT NOT NULL CHECK(origin IN ('scan','ingest','review','migration')),
  reason TEXT NOT NULL CHECK(length(reason)<=4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_collection_intake_page ON source_collection_media_intake(collection_uuid,uuid);
CREATE INDEX source_collection_intake_media ON source_collection_media_intake(media_uuid,collection_uuid,uuid);
CREATE TRIGGER source_collection_intake_kind BEFORE INSERT ON source_collection_media_intake
WHEN NEW.submitted_media_uuid!=NEW.media_uuid OR NOT EXISTS(SELECT 1 FROM archive_entities WHERE uuid=NEW.media_uuid AND kind IN ('scene','image'))
BEGIN SELECT RAISE(ABORT,'collection intake requires a scene or image identity'); END;
CREATE TRIGGER source_collection_intake_immutable BEFORE UPDATE ON source_collection_media_intake
WHEN NEW.uuid!=OLD.uuid OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
  OR NEW.origin!=OLD.origin OR NEW.reason!=OLD.reason OR NEW.created_at!=OLD.created_at OR NEW.submitted_media_uuid!=OLD.submitted_media_uuid
  OR (NEW.media_uuid!=OLD.media_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.media_uuid))
BEGIN SELECT RAISE(ABORT,'collection intake provenance is immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000015,'Revisioned media roots, source collections and intake provenance','{}');
