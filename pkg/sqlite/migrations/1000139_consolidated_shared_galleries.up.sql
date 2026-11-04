-- A reviewed post merge may retain a gallery that its original identity already
-- shared. The application verifies that prior choice before opening this scope;
-- the existing consolidation commit guard requires the context to be closed.
DROP TRIGGER post_gallery_thread_share_insert;
CREATE TRIGGER post_gallery_thread_share_insert BEFORE INSERT ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT NULL
AND NOT EXISTS (SELECT 1 FROM source_post_consolidation_context c
 JOIN source_post_consolidations m ON m.uuid=c.request_uuid
 WHERE c.source_uuid=m.source_uuid AND c.destination_uuid=m.destination_uuid
 AND c.destination_uuid=NEW.post_uuid)
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
VALUES(1000139,'Preserve existing shared galleries through post consolidation','{}');
