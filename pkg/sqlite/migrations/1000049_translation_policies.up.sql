CREATE TABLE translation_policies (
 collection_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_collections(uuid),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 FOREIGN KEY(collection_uuid,revision) REFERENCES translation_policy_revisions(collection_uuid,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE translation_policy_revisions (
 collection_uuid TEXT NOT NULL REFERENCES translation_policies(collection_uuid),
 revision INTEGER NOT NULL CHECK(revision>0),
 collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
 definition TEXT NOT NULL CHECK(json_valid(definition) AND json_type(definition)='object' AND length(CAST(definition AS BLOB))<=1024),
 origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
 reason TEXT NOT NULL CHECK(length(reason)<=4096),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(collection_uuid,revision),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE TRIGGER translation_policy_revision_scope BEFORE INSERT ON translation_policy_revisions
BEGIN
 SELECT RAISE(ABORT,'translation policy revision is stale') WHERE NOT EXISTS(
  SELECT 1 FROM translation_policies p LEFT JOIN translation_policy_revisions r ON r.collection_uuid=p.collection_uuid AND r.revision=p.revision
  JOIN source_collections c ON c.uuid=p.collection_uuid
  JOIN source_collection_revisions s ON s.collection_uuid=c.uuid AND s.revision=c.revision
  WHERE p.collection_uuid=NEW.collection_uuid AND c.revision=NEW.collection_revision AND s.state!='retired'
   AND ((r.collection_uuid IS NULL AND NEW.revision=1 AND p.revision=1) OR NEW.revision=p.revision+1)
 );
END;
CREATE TRIGGER translation_policy_revision_publish AFTER INSERT ON translation_policy_revisions
BEGIN UPDATE translation_policies SET revision=NEW.revision WHERE collection_uuid=NEW.collection_uuid; END;
CREATE TRIGGER translation_policy_revision_immutable BEFORE UPDATE ON translation_policy_revisions
BEGIN SELECT RAISE(ABORT,'translation policy revisions are immutable'); END;
CREATE TRIGGER translation_policy_identity_immutable BEFORE UPDATE ON translation_policies
WHEN NEW.collection_uuid!=OLD.collection_uuid OR NEW.revision NOT IN (OLD.revision,OLD.revision+1)
 OR NOT EXISTS(SELECT 1 FROM translation_policy_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.revision)
BEGIN SELECT RAISE(ABORT,'translation policy advances only to a recorded revision'); END;

CREATE TABLE capture_translation_decisions (
 collection_uuid TEXT NOT NULL,
 capture_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 policy_revision INTEGER,
 status TEXT NOT NULL CHECK(status IN ('no_policy','disabled','collection_changed','recorded')),
 entry_count INTEGER NOT NULL CHECK(entry_count BETWEEN 0 AND 2),
 created_at DATETIME NOT NULL,
 PRIMARY KEY(collection_uuid,capture_uuid,collection_revision),
 FOREIGN KEY(collection_uuid,capture_uuid,collection_revision) REFERENCES source_collection_captures(collection_uuid,capture_uuid,collection_revision),
 FOREIGN KEY(collection_uuid,policy_revision) REFERENCES translation_policy_revisions(collection_uuid,revision),
 CHECK((status='no_policy')=(policy_revision IS NULL)),
 CHECK((status='recorded' AND entry_count BETWEEN 1 AND 2) OR (status!='recorded' AND entry_count=0))
);
CREATE TRIGGER capture_translation_decision_immutable BEFORE UPDATE ON capture_translation_decisions
BEGIN SELECT RAISE(ABORT,'capture translation decisions are immutable'); END;
CREATE TABLE capture_translation_entries (
 collection_uuid TEXT NOT NULL,
 capture_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 field TEXT NOT NULL CHECK(field IN ('title','caption')),
 status TEXT NOT NULL CHECK(status IN ('created','retained','no_text')),
 target_uuid TEXT,
 target_revision INTEGER,
 PRIMARY KEY(collection_uuid,capture_uuid,collection_revision,field),
 FOREIGN KEY(collection_uuid,capture_uuid,collection_revision) REFERENCES capture_translation_decisions(collection_uuid,capture_uuid,collection_revision),
 FOREIGN KEY(target_uuid,target_revision) REFERENCES translation_target_history(target_uuid,revision),
 CHECK((target_uuid IS NULL)=(target_revision IS NULL)),
 CHECK((status IN ('created','retained'))=(target_uuid IS NOT NULL))
);
CREATE INDEX capture_translation_target ON capture_translation_entries(target_uuid,target_revision) WHERE target_uuid IS NOT NULL;
CREATE TRIGGER capture_translation_entry_immutable BEFORE UPDATE ON capture_translation_entries
BEGIN SELECT RAISE(ABORT,'capture translation entries are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000049,'Revisioned source translation policies and capture scheduling decisions','{}');
