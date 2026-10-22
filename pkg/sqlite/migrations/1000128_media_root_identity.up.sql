-- Identity format upgrades add reviewed filesystem evidence to a root revision.
-- They cannot move the root, edit its definition, or change admitted work.
CREATE TABLE media_root_identity_upgrades (
  root_uuid TEXT NOT NULL,
  root_revision INTEGER NOT NULL,
  persistent_identity TEXT NOT NULL CHECK(length(persistent_identity) BETWEEN 1 AND 128 AND persistent_identity LIKE 'fsid-v1:%'),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(root_uuid,root_revision),
  FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision)
);
CREATE TRIGGER media_root_identity_upgrade_scope BEFORE INSERT ON media_root_identity_upgrades
WHEN NOT EXISTS(SELECT 1 FROM media_roots b JOIN media_root_revisions r ON r.root_uuid=b.uuid AND r.revision=b.revision
  WHERE b.uuid=NEW.root_uuid AND b.revision=NEW.root_revision AND r.state='active'
    AND r.directory_identity IS NOT NULL AND r.directory_identity NOT LIKE 'fsid-v1:%')
BEGIN SELECT RAISE(ABORT,'identity upgrade requires a current active legacy binding'); END;
CREATE TRIGGER media_root_identity_upgrade_immutable BEFORE UPDATE ON media_root_identity_upgrades
BEGIN SELECT RAISE(ABORT,'root identity upgrades are immutable'); END;
CREATE TRIGGER media_root_identity_upgrade_retained BEFORE DELETE ON media_root_identity_upgrades
BEGIN SELECT RAISE(ABORT,'root identity upgrades are retained'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000128,'Reviewed persistent filesystem identities for legacy media roots','{}');
