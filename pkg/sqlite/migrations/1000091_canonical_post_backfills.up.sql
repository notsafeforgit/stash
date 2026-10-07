-- Keep each original file-proof chain while allowing a reviewed current post
-- to select media from any member of its consolidated identity. Existing
-- requests, decisions, evidence and their owners remain unchanged.
DROP TRIGGER post_media_decision_evidence_scope;
CREATE TRIGGER post_media_decision_evidence_scope BEFORE INSERT ON post_media_decision_evidence
WHEN NOT EXISTS(SELECT 1 FROM post_media_decisions d
 JOIN post_media_backfill_decisions b ON b.decision_uuid=d.uuid
 JOIN source_post_identities chosen ON chosen.post_uuid=d.post_uuid
 JOIN source_media_evidence e ON e.uuid=NEW.evidence_uuid
 JOIN source_post_identities original ON original.post_uuid=e.post_uuid AND original.canonical_uuid=chosen.canonical_uuid
 JOIN source_post_file_evidence p ON p.uuid=NEW.post_file_uuid AND p.post_uuid=e.post_uuid
 JOIN source_file_matches f ON f.uuid=NEW.match_uuid AND f.observation_uuid=p.observation_uuid
 WHERE d.uuid=NEW.decision_uuid AND d.origin='migration' AND d.state='linked'
 AND e.attachment_uuid IS NULL AND e.basis='legacy' AND p.origin='migration' AND p.basis='catalog-appearance'
 AND p.uuid=json_extract(e.details,'$.source_post_file_evidence_uuid')
 AND f.uuid=json_extract(e.details,'$.source_file_match_uuid') AND f.file_uuid=e.file_uuid)
BEGIN SELECT RAISE(ABORT,'post media matching requires its original file chain and current post identity'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000091,'Preserve original file proof for canonical post backfills','{}');
