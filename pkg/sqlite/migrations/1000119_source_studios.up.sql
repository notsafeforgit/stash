-- Native source attribution, independent of scraper configuration revisions.
-- One current studio per source; posts and shared albums expose their union.
CREATE TABLE source_collection_studios (
 collection_uuid TEXT PRIMARY KEY REFERENCES source_collections(uuid),
 studio_id INTEGER REFERENCES studios(id) ON DELETE SET NULL,
 revision INTEGER NOT NULL CHECK(revision>0)
);
CREATE INDEX source_collection_studios_studio ON source_collection_studios(studio_id,collection_uuid);
CREATE TRIGGER source_collection_studio_removed AFTER UPDATE OF studio_id ON source_collection_studios
WHEN NEW.studio_id IS NULL AND OLD.studio_id IS NOT NULL AND NEW.revision=OLD.revision
BEGIN
 UPDATE source_collection_studios SET revision=revision+1 WHERE collection_uuid=NEW.collection_uuid;
END;

-- UNION ALL keeps post/collection lookups indexable. Consumers deduplicate
-- studios, not captures. Aliases and consolidated posts resolve at read time.
CREATE VIEW source_post_studio_collections AS
SELECT i.canonical_uuid AS post_uuid,coalesce(a.source_uuid,e.collection_uuid) AS collection_uuid
FROM source_collection_post_evidence e
JOIN source_post_identities i ON i.post_uuid=e.post_uuid
LEFT JOIN source_collection_aliases a ON a.alias_uuid=e.collection_uuid
UNION ALL
SELECT i.canonical_uuid,coalesce(a.source_uuid,e.collection_uuid)
FROM source_collection_captures e JOIN source_captures c ON c.uuid=e.capture_uuid
JOIN source_post_identities i ON i.post_uuid=c.post_uuid
LEFT JOIN source_collection_aliases a ON a.alias_uuid=e.collection_uuid;

CREATE TABLE source_gallery_studio_pending (
 gallery_id INTEGER PRIMARY KEY REFERENCES galleries(id) ON DELETE CASCADE
);

CREATE TRIGGER source_gallery_studio_link_insert AFTER INSERT ON post_gallery_links
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM post_gallery_links l WHERE l.gallery_uuid=NEW.gallery_uuid) OR a.uuid=NEW.gallery_uuid)
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_link_update AFTER UPDATE ON post_gallery_links
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM post_gallery_links l WHERE l.gallery_uuid=NEW.gallery_uuid OR l.gallery_uuid=OLD.gallery_uuid) OR a.uuid=NEW.gallery_uuid OR a.uuid=OLD.gallery_uuid)
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_link_delete AFTER DELETE ON post_gallery_links
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM post_gallery_links l WHERE l.gallery_uuid=OLD.gallery_uuid) OR a.uuid=OLD.gallery_uuid)
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_binding_insert AFTER INSERT ON source_collection_studios
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM source_post_studio_collections e JOIN post_gallery_links l ON l.post_uuid=e.post_uuid WHERE e.collection_uuid=NEW.collection_uuid))
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_binding_update AFTER UPDATE ON source_collection_studios
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM source_post_studio_collections e JOIN post_gallery_links l ON l.post_uuid=e.post_uuid WHERE e.collection_uuid=NEW.collection_uuid))
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_collection_post_evidence_studio AFTER INSERT ON source_collection_post_evidence
WHEN EXISTS(SELECT 1 FROM source_collection_studios WHERE studio_id IS NOT NULL
 AND collection_uuid=coalesce((SELECT source_uuid FROM source_collection_aliases WHERE alias_uuid=NEW.collection_uuid),NEW.collection_uuid))
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM post_gallery_links l JOIN source_post_identities i ON i.canonical_uuid=l.post_uuid WHERE i.post_uuid=NEW.post_uuid))
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_collection_captures_studio AFTER INSERT ON source_collection_captures
WHEN EXISTS(SELECT 1 FROM source_collection_studios WHERE studio_id IS NOT NULL
 AND collection_uuid=coalesce((SELECT source_uuid FROM source_collection_aliases WHERE alias_uuid=NEW.collection_uuid),NEW.collection_uuid))
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM post_gallery_links l JOIN source_post_identities i ON i.canonical_uuid=l.post_uuid WHERE i.post_uuid=(SELECT post_uuid FROM source_captures WHERE uuid=NEW.capture_uuid)))
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_post_merge AFTER UPDATE OF canonical_uuid ON source_post_identities
WHEN NEW.canonical_uuid!=OLD.canonical_uuid
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT gallery_uuid FROM post_gallery_links WHERE post_uuid IN (NEW.canonical_uuid,OLD.canonical_uuid)))
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_studio_alias AFTER INSERT ON source_collection_aliases
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid IN (SELECT l.gallery_uuid FROM source_post_studio_collections e JOIN post_gallery_links l ON l.post_uuid=e.post_uuid WHERE e.collection_uuid=NEW.source_uuid))
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;
CREATE TRIGGER source_gallery_studio_origin AFTER UPDATE OF origin ON galleries
WHEN NEW.origin='source' AND OLD.origin IS NOT NEW.origin
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (g.id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_choice_insert AFTER INSERT ON metadata_field_heads
WHEN NEW.field='studio'
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid=NEW.entity_uuid)
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

CREATE TRIGGER source_gallery_studio_choice_update AFTER UPDATE ON metadata_field_heads
WHEN NEW.field='studio'
BEGIN
 INSERT INTO source_gallery_studio_pending
 SELECT DISTINCT g.id FROM galleries g JOIN archive_entities a ON a.gallery_id=g.id
 WHERE g.origin='source' AND a.state='active' AND (a.uuid=NEW.entity_uuid)
 AND NOT EXISTS(SELECT 1 FROM source_gallery_studio_pending q WHERE q.gallery_id=g.id);
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000119,'Source studios and shared album attribution','{}');

