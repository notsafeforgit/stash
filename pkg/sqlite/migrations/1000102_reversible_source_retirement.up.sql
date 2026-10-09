-- Retirement describes operational intent, not deletion of an identity.
-- Restoring a definition appends a revision; historical scopes stay immutable.
DROP TRIGGER media_root_revision_scope;
CREATE TRIGGER media_root_revision_scope BEFORE INSERT ON media_root_revisions
BEGIN
  SELECT RAISE(ABORT,'root revision is stale') WHERE NOT EXISTS(
    SELECT 1 FROM media_roots r LEFT JOIN media_root_revisions d ON d.root_uuid=r.uuid AND d.revision=r.revision
    WHERE r.uuid=NEW.root_uuid AND ((d.root_uuid IS NULL AND NEW.revision=1 AND r.revision=1)
      OR (d.root_uuid IS NOT NULL AND NEW.revision=r.revision+1))
  );
END;

DROP TRIGGER source_collection_revision_scope;
CREATE TRIGGER source_collection_revision_scope BEFORE INSERT ON source_collection_revisions
BEGIN
  SELECT RAISE(ABORT,'collection revision is stale') WHERE NOT EXISTS(
    SELECT 1 FROM source_collections c LEFT JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
    WHERE c.uuid=NEW.collection_uuid AND ((d.collection_uuid IS NULL AND NEW.revision=1 AND c.revision=1)
      OR (d.collection_uuid IS NOT NULL AND NEW.revision=c.revision+1))
  );
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000102,'Allow restoring retired source collections and media roots','{}');
