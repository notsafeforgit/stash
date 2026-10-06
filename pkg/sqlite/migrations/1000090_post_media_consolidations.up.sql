-- A replacement across original post owners records the consolidation that
-- established their shared identity. Existing decisions and supersessions keep
-- their columns and owners. The deferred FK makes an orphan proof uncommittable.
CREATE UNIQUE INDEX post_media_supersessions_scope ON post_media_supersessions(previous_uuid,decision_uuid);
CREATE TABLE post_media_consolidation_edges (
 previous_uuid TEXT PRIMARY KEY NOT NULL REFERENCES post_media_decisions(uuid),
 decision_uuid TEXT NOT NULL REFERENCES post_media_decisions(uuid),
 consolidation_uuid TEXT NOT NULL REFERENCES source_post_consolidations(uuid),
 FOREIGN KEY(previous_uuid,decision_uuid) REFERENCES post_media_supersessions(previous_uuid,decision_uuid) DEFERRABLE INITIALLY DEFERRED
) WITHOUT ROWID;
CREATE INDEX post_media_consolidation_edges_receipt ON post_media_consolidation_edges(consolidation_uuid,previous_uuid);
CREATE TRIGGER post_media_consolidation_edge_scope BEFORE INSERT ON post_media_consolidation_edges
WHEN NOT EXISTS (
 SELECT 1 FROM post_media_decisions old
 JOIN post_media_links head ON head.decision_uuid=old.uuid
 JOIN post_media_decisions current ON current.uuid=NEW.decision_uuid
 JOIN source_posts root ON root.uuid=current.post_uuid AND root.state='active' AND root.revision=current.post_revision
 JOIN source_post_identities identity ON identity.post_uuid=root.uuid AND identity.canonical_uuid=root.uuid
 JOIN source_post_consolidations c ON c.uuid=NEW.consolidation_uuid AND c.destination_uuid=root.uuid
 WHERE old.uuid=NEW.previous_uuid AND old.post_uuid!=current.post_uuid
 AND c.sequence=(SELECT max(sequence) FROM source_post_consolidations WHERE destination_uuid=root.uuid)
 AND current.post_revision>c.destination_revision
 AND current.post_uuid IN (WITH RECURSIVE ancestors(uuid,depth) AS (
  SELECT old.post_uuid,0 UNION ALL
  SELECT h.destination_uuid,a.depth+1 FROM ancestors a JOIN source_post_consolidations h ON h.source_uuid=a.uuid
  WHERE h.sequence<=c.sequence AND a.depth<256
 ) SELECT uuid FROM ancestors)
 AND current.media_uuid IN (WITH RECURSIVE targets(uuid,depth) AS (
  SELECT old.media_uuid,0 UNION ALL
  SELECT e.redirect_to,t.depth+1 FROM targets t JOIN archive_entities e ON e.uuid=t.uuid
  WHERE e.state='redirected' AND t.depth<127
 ) SELECT uuid FROM targets))
BEGIN SELECT RAISE(ABORT,'post media consolidation requires current related choices and its reviewed identity'); END;
CREATE TRIGGER post_media_consolidation_edge_immutable BEFORE UPDATE ON post_media_consolidation_edges
BEGIN SELECT RAISE(ABORT,'post media consolidation evidence is immutable'); END;

DROP TRIGGER post_media_supersession_scope;
CREATE TRIGGER post_media_supersession_scope BEFORE INSERT ON post_media_supersessions
WHEN NOT EXISTS(SELECT 1 FROM post_media_decisions old JOIN post_media_decisions current ON current.uuid=NEW.decision_uuid
 WHERE old.uuid=NEW.previous_uuid AND (
  (old.post_uuid=current.post_uuid AND old.post_revision<current.post_revision)
  OR (old.post_uuid!=current.post_uuid AND EXISTS(SELECT 1 FROM post_media_consolidation_edges e
   WHERE e.previous_uuid=NEW.previous_uuid AND e.decision_uuid=NEW.decision_uuid))))
BEGIN SELECT RAISE(ABORT,'post media replacement requires a later decision or reviewed post consolidation'); END;

-- Consolidation may carry an existing choice for a deleted media identity.
-- It cannot invent one, restore a library row or make deleted media usable.
DROP TRIGGER post_media_decision_scope;
CREATE TRIGGER post_media_decision_scope BEFORE INSERT ON post_media_decisions
BEGIN
 SELECT RAISE(ABORT,'post media requires an active post and its current revision')
 WHERE NOT EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state='active' AND revision=NEW.post_revision);
 SELECT RAISE(ABORT,'post media requires a current scene or image')
 WHERE NOT EXISTS(SELECT 1 FROM archive_entities media WHERE media.uuid=NEW.media_uuid
  AND media.kind IN ('scene','image') AND media.revision=NEW.media_revision
  AND (media.state='active' OR (media.state='deleted'
   AND EXISTS(SELECT 1 FROM source_post_identities root WHERE root.post_uuid=NEW.post_uuid AND root.canonical_uuid=root.post_uuid)
   AND EXISTS(SELECT 1 FROM source_post_consolidations WHERE destination_uuid=NEW.post_uuid AND destination_revision<NEW.post_revision)
   AND EXISTS(WITH RECURSIVE aliases(uuid,depth) AS (
    SELECT NEW.media_uuid,0 UNION ALL SELECT a.uuid,r.depth+1 FROM aliases r JOIN archive_entities a ON a.redirect_to=r.uuid WHERE r.depth<127
   ) SELECT 1 FROM source_post_identities owner JOIN post_media_links h ON h.post_uuid=owner.post_uuid
   WHERE owner.canonical_uuid=NEW.post_uuid AND h.media_uuid IN (SELECT uuid FROM aliases)))));
END;

DROP TRIGGER metadata_decision_post_media_scope;
CREATE TRIGGER metadata_decision_post_media_scope BEFORE INSERT ON metadata_decision_post_media
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_decisions d
 JOIN source_captures c ON c.uuid=d.capture_uuid
 JOIN source_post_identities captured ON captured.post_uuid=c.post_uuid
 JOIN post_media_decisions p ON p.uuid=NEW.post_media_decision_uuid AND p.state='linked'
 JOIN source_post_identities selected ON selected.post_uuid=p.post_uuid AND selected.canonical_uuid=captured.canonical_uuid
 WHERE d.uuid=NEW.decision_uuid AND d.origin='source'
 AND d.entity_uuid IN (WITH RECURSIVE targets(uuid,depth) AS (
 SELECT p.media_uuid,0 UNION ALL SELECT a.redirect_to,t.depth+1 FROM targets t JOIN archive_entities a ON a.uuid=t.uuid
 WHERE a.state='redirected' AND t.depth<127
 ) SELECT uuid FROM targets))
BEGIN SELECT RAISE(ABORT,'metadata post association requires matching source provenance'); END;

INSERT INTO native_migration_history(version,name,details)
 VALUES(1000090,'Preserve media choice history across post consolidation','{}');
