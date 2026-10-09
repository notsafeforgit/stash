CREATE TABLE source_post_threads (
  post_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_posts(uuid),
  capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
  namespace TEXT NOT NULL CHECK(namespace='native:twitter'),
  post_id TEXT NOT NULL CHECK(length(post_id) BETWEEN 1 AND 20 AND post_id NOT GLOB '*[^0-9]*'),
  conversation_id TEXT NOT NULL,
  author_id TEXT NOT NULL,
  reply_id TEXT NOT NULL DEFAULT '',
  reply_author_id TEXT NOT NULL DEFAULT '',
  conflict_capture_uuid TEXT REFERENCES source_captures(uuid),
  UNIQUE(namespace,post_id)
) WITHOUT ROWID;
CREATE INDEX source_post_threads_conversation ON source_post_threads(namespace,conversation_id,length(post_id),post_id);
CREATE INDEX source_post_threads_author ON source_post_threads(namespace,conversation_id,author_id,length(post_id),post_id);
CREATE INDEX source_post_threads_parent ON source_post_threads(namespace,reply_id,post_uuid) WHERE reply_id!='';
CREATE TRIGGER source_post_thread_scope BEFORE INSERT ON source_post_threads
BEGIN
  SELECT RAISE(ABORT,'thread evidence must belong to its post') WHERE
    (SELECT post_uuid FROM source_captures WHERE uuid=NEW.capture_uuid) IS NOT NEW.post_uuid OR
    (SELECT post_uuid FROM source_post_identifiers WHERE namespace=NEW.namespace AND value=NEW.post_id) IS NOT NEW.post_uuid;
END;
CREATE TRIGGER source_post_thread_immutable BEFORE UPDATE ON source_post_threads
WHEN NEW.post_uuid!=OLD.post_uuid OR NEW.namespace!=OLD.namespace OR NEW.post_id!=OLD.post_id
 OR NEW.conversation_id!=OLD.conversation_id OR NEW.reply_id!=OLD.reply_id OR NEW.author_id!=OLD.author_id
 OR (NEW.reply_author_id!=OLD.reply_author_id AND OLD.reply_author_id!='')
 OR (NEW.conflict_capture_uuid IS NOT OLD.conflict_capture_uuid AND OLD.conflict_capture_uuid IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'thread identity is immutable'); END;

-- Shared source galleries remain explicit per-post associations. The service
-- permits sharing only for a captured same-author thread; manual claims retain
-- the existing exclusive-claim check.
DROP INDEX post_gallery_links_gallery;
CREATE INDEX post_gallery_links_gallery ON post_gallery_links(gallery_uuid,post_uuid) WHERE gallery_uuid IS NOT NULL;
CREATE TRIGGER post_gallery_thread_share_insert BEFORE INSERT ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT NULL
BEGIN
  SELECT RAISE(ABORT,'only evidenced self-replies may share a source gallery') WHERE EXISTS (
    SELECT 1 FROM post_gallery_links l WHERE l.gallery_uuid=NEW.gallery_uuid AND l.post_uuid!=NEW.post_uuid
    AND NOT EXISTS (SELECT 1 FROM source_post_threads a JOIN source_post_threads b
      ON b.namespace=a.namespace AND b.conversation_id=a.conversation_id AND b.author_id=a.author_id
      WHERE a.post_uuid=NEW.post_uuid AND b.post_uuid=l.post_uuid
      AND a.conflict_capture_uuid IS NULL AND b.conflict_capture_uuid IS NULL
      AND (a.reply_id='' OR a.reply_author_id=a.author_id) AND (b.reply_id='' OR b.reply_author_id=b.author_id)));
END;
CREATE TRIGGER post_gallery_thread_share_update BEFORE UPDATE OF gallery_uuid ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT NULL AND NEW.gallery_uuid IS NOT OLD.gallery_uuid
BEGIN
  SELECT RAISE(ABORT,'only evidenced self-replies may share a source gallery') WHERE EXISTS (
    SELECT 1 FROM post_gallery_links l WHERE l.gallery_uuid=NEW.gallery_uuid AND l.post_uuid!=NEW.post_uuid
    AND NOT EXISTS (SELECT 1 FROM source_post_threads a JOIN source_post_threads b
      ON b.namespace=a.namespace AND b.conversation_id=a.conversation_id AND b.author_id=a.author_id
      WHERE a.post_uuid=NEW.post_uuid AND b.post_uuid=l.post_uuid
      AND a.conflict_capture_uuid IS NULL AND b.conflict_capture_uuid IS NULL
      AND (a.reply_id='' OR a.reply_author_id=a.author_id) AND (b.reply_id='' OR b.reply_author_id=b.author_id)));
END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000105,'Captured Twitter reply relationships and shared thread galleries','{}');
