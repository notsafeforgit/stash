-- Thread facts remain on the identity that originally captured them. Reviewed
-- post consolidation must preserve their use by the canonical post without
-- allowing unrelated authors or conversations to share source galleries.
DROP TRIGGER source_post_thread_scope;
CREATE TRIGGER source_post_thread_scope BEFORE INSERT ON source_post_threads
BEGIN
  SELECT RAISE(ABORT,'thread evidence must belong to its post') WHERE
    (SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=NEW.post_uuid) IS NULL OR
    (SELECT i.canonical_uuid FROM source_captures c JOIN source_post_identities i ON i.post_uuid=c.post_uuid
      WHERE c.uuid=NEW.capture_uuid) IS NOT
    (SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=NEW.post_uuid) OR
    (SELECT i.canonical_uuid FROM source_post_identifiers p JOIN source_post_identities i ON i.post_uuid=p.post_uuid
      WHERE p.namespace=NEW.namespace AND p.value=NEW.post_id) IS NOT
    (SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=NEW.post_uuid);
END;

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
  AND NOT EXISTS (SELECT 1 FROM post_gallery_links l
    JOIN source_post_identities ai ON ai.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=NEW.post_uuid)
    JOIN source_post_threads a ON a.post_uuid=ai.post_uuid
    JOIN source_post_identities bi ON bi.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=l.post_uuid)
    JOIN source_post_threads b ON b.post_uuid=bi.post_uuid
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
  AND NOT EXISTS (SELECT 1 FROM post_gallery_links l
    JOIN source_post_identities ai ON ai.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=NEW.post_uuid)
    JOIN source_post_threads a ON a.post_uuid=ai.post_uuid
    JOIN source_post_identities bi ON bi.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=l.post_uuid)
    JOIN source_post_threads b ON b.post_uuid=bi.post_uuid
    AND b.namespace=a.namespace AND b.conversation_id=a.conversation_id AND b.author_id=a.author_id
    WHERE l.gallery_uuid=NEW.gallery_uuid AND l.post_uuid!=NEW.post_uuid
    AND a.conflict_capture_uuid IS NULL AND b.conflict_capture_uuid IS NULL
    AND (a.reply_id='' OR a.reply_author_id=a.author_id) AND (b.reply_id='' OR b.reply_author_id=b.author_id));
END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000140,'Resolve captured thread evidence through reviewed post identities','{}');
