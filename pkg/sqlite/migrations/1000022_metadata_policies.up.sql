-- Policies remain bound to the reviewed collection scope. A changed collection
-- needs a new policy revision before its new scope inherits these rules.
CREATE TABLE metadata_policies (
  collection_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_collections(uuid),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  FOREIGN KEY(collection_uuid,revision) REFERENCES metadata_policy_revisions(collection_uuid,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE metadata_policy_revisions (
  collection_uuid TEXT NOT NULL REFERENCES metadata_policies(collection_uuid),
  revision INTEGER NOT NULL CHECK(revision>0),
  collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
  definition TEXT NOT NULL CHECK(json_valid(definition) AND json_type(definition)='object' AND length(CAST(definition AS BLOB))<=131072),
  origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
  reason TEXT NOT NULL CHECK(length(reason)<=4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(collection_uuid,revision),
  FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE TRIGGER metadata_policy_revision_scope BEFORE INSERT ON metadata_policy_revisions
BEGIN
  SELECT RAISE(ABORT,'policy revision is stale') WHERE NOT EXISTS(
    SELECT 1 FROM metadata_policies p LEFT JOIN metadata_policy_revisions d ON d.collection_uuid=p.collection_uuid AND d.revision=p.revision
    JOIN source_collections c ON c.uuid=p.collection_uuid
    JOIN source_collection_revisions s ON s.collection_uuid=c.uuid AND s.revision=c.revision
    WHERE p.collection_uuid=NEW.collection_uuid AND c.revision=NEW.collection_revision AND s.state!='retired'
      AND ((d.collection_uuid IS NULL AND NEW.revision=1 AND p.revision=1) OR NEW.revision=p.revision+1)
  );
END;
CREATE TRIGGER metadata_policy_revision_publish AFTER INSERT ON metadata_policy_revisions
BEGIN UPDATE metadata_policies SET revision=NEW.revision WHERE collection_uuid=NEW.collection_uuid; END;
CREATE TRIGGER metadata_policy_revision_immutable BEFORE UPDATE ON metadata_policy_revisions
BEGIN SELECT RAISE(ABORT,'policy revisions are immutable'); END;
CREATE TRIGGER metadata_policy_identity_immutable BEFORE UPDATE ON metadata_policies
WHEN NEW.collection_uuid!=OLD.collection_uuid OR NEW.revision NOT IN (OLD.revision,OLD.revision+1)
  OR NOT EXISTS(SELECT 1 FROM metadata_policy_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.revision)
BEGIN SELECT RAISE(ABORT,'policy advances only to a recorded revision'); END;

CREATE TABLE metadata_decision_policies (
  decision_uuid TEXT NOT NULL PRIMARY KEY REFERENCES metadata_field_decisions(uuid),
  collection_uuid TEXT NOT NULL,
  revision INTEGER NOT NULL,
  FOREIGN KEY(collection_uuid,revision) REFERENCES metadata_policy_revisions(collection_uuid,revision)
);
CREATE INDEX metadata_decision_policy_history ON metadata_decision_policies(collection_uuid,revision,decision_uuid);
CREATE TRIGGER metadata_decision_policy_immutable BEFORE UPDATE ON metadata_decision_policies
BEGIN SELECT RAISE(ABORT,'decision policy provenance is immutable'); END;
CREATE TRIGGER metadata_decision_policy_origin BEFORE INSERT ON metadata_decision_policies
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_decisions WHERE uuid=NEW.decision_uuid AND mode='inherit' AND origin IN ('source','policy','filename'))
BEGIN SELECT RAISE(ABORT,'policy provenance requires an inherited decision'); END;

CREATE INDEX media_root_path ON media_root_revisions(server_path,root_uuid,revision) WHERE server_path IS NOT NULL;
CREATE INDEX source_collection_directory ON source_collection_revisions(root_uuid,path_prefix,collection_uuid,revision) WHERE root_uuid IS NOT NULL;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000022,'Revisioned native metadata policies and decision provenance','{}');
