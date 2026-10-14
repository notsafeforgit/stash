-- Alternate gallery labels survive consolidation without duplicating post
-- bodies. Original posts, captions and attachment lists remain separate.
CREATE TABLE gallery_alternate_titles (
  gallery_id INTEGER NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,
  title TEXT NOT NULL CHECK (length(trim(title)) > 0),
  PRIMARY KEY (gallery_id, title)
) WITHOUT ROWID;

-- Scoped to a managed consolidation transaction; must be empty at commit.
-- Normal association changes retain the existing thread/claim validation.
CREATE TABLE source_gallery_merge_context (
  source_uuid TEXT NOT NULL PRIMARY KEY REFERENCES archive_entities(uuid),
  target_uuid TEXT NOT NULL REFERENCES archive_entities(uuid),
  CHECK (source_uuid != target_uuid)
) WITHOUT ROWID;
DROP TRIGGER post_gallery_thread_share_insert;
CREATE TRIGGER post_gallery_thread_share_insert BEFORE INSERT ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT NULL
BEGIN
  SELECT RAISE(ABORT,'only evidenced self-replies may share a source gallery')
  WHERE EXISTS (SELECT 1 FROM post_gallery_links WHERE gallery_uuid=NEW.gallery_uuid AND post_uuid!=NEW.post_uuid)
  AND NOT EXISTS (SELECT 1 FROM post_gallery_links l JOIN source_post_threads a ON a.post_uuid=NEW.post_uuid
    JOIN source_post_threads b ON b.post_uuid=l.post_uuid
    AND b.namespace=a.namespace AND b.conversation_id=a.conversation_id AND b.author_id=a.author_id
    WHERE l.gallery_uuid=NEW.gallery_uuid AND l.post_uuid!=NEW.post_uuid
    AND a.conflict_capture_uuid IS NULL AND b.conflict_capture_uuid IS NULL
    AND (a.reply_id='' OR a.reply_author_id=a.author_id) AND (b.reply_id='' OR b.reply_author_id=b.author_id));
END;
DROP TRIGGER post_gallery_thread_share_update;
CREATE TRIGGER post_gallery_thread_share_update BEFORE UPDATE OF gallery_uuid ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT NULL AND NEW.gallery_uuid IS NOT OLD.gallery_uuid
AND NOT EXISTS (SELECT 1 FROM source_gallery_merge_context
  WHERE source_uuid=OLD.gallery_uuid AND target_uuid=NEW.gallery_uuid)
BEGIN
  SELECT RAISE(ABORT,'only evidenced self-replies may share a source gallery')
  WHERE EXISTS (SELECT 1 FROM post_gallery_links WHERE gallery_uuid=NEW.gallery_uuid AND post_uuid!=NEW.post_uuid)
  AND NOT EXISTS (SELECT 1 FROM post_gallery_links l JOIN source_post_threads a ON a.post_uuid=NEW.post_uuid
    JOIN source_post_threads b ON b.post_uuid=l.post_uuid
    AND b.namespace=a.namespace AND b.conversation_id=a.conversation_id AND b.author_id=a.author_id
    WHERE l.gallery_uuid=NEW.gallery_uuid AND l.post_uuid!=NEW.post_uuid
    AND a.conflict_capture_uuid IS NULL AND b.conflict_capture_uuid IS NULL
    AND (a.reply_id='' OR a.reply_author_id=a.author_id) AND (b.reply_id='' OR b.reply_author_id=b.author_id));
END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000117,'Consolidated overlapping source albums and alternate titles','{}');
