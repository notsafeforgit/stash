-- Original captures, publisher decisions and identifier dates are retained.
-- New bindings describe source context at capture acceptance, before assessment.
CREATE TABLE source_capture_contexts (
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 path TEXT NOT NULL CHECK(path IN ('/_parent','/_reddit')),
 parent_capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 PRIMARY KEY(capture_uuid,path),
 CHECK(capture_uuid!=parent_capture_uuid)
);
CREATE INDEX source_capture_contexts_parent ON source_capture_contexts(parent_capture_uuid,capture_uuid);
CREATE INDEX source_captures_with_context ON source_captures(uuid) WHERE retention_policy='source-retention-v1+capture-context-v1';
CREATE TRIGGER source_capture_context_immutable BEFORE UPDATE ON source_capture_contexts
BEGIN SELECT RAISE(ABORT,'source capture context is immutable'); END;
CREATE TRIGGER source_capture_context_scope BEFORE INSERT ON source_capture_contexts
WHEN NOT EXISTS(SELECT 1 FROM source_captures c JOIN source_captures p ON p.post_uuid=c.post_uuid
 JOIN source_posts s ON s.uuid=c.post_uuid
 WHERE c.uuid=NEW.capture_uuid AND p.uuid=NEW.parent_capture_uuid AND s.state='active'
 AND c.retention_policy='source-retention-v1+capture-context-v1'
 AND (p.captured_at IS NULL OR (c.captured_at IS NOT NULL AND julianday(p.captured_at)<=julianday(c.captured_at))))
 OR EXISTS(SELECT 1 FROM capture_publisher_decisions WHERE capture_uuid=NEW.capture_uuid)
 OR EXISTS(SELECT 1 FROM enrichment_published_records WHERE capture_uuid=NEW.capture_uuid)
BEGIN SELECT RAISE(ABORT,'source context must precede publisher assessment and publication'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000063,'Retain original observation provenance for reused capture context','{}');
