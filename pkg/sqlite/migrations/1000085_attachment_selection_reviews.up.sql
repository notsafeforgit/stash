-- Source order reviews never mutate library gallery memberships or media links.
CREATE TABLE attachment_selection_reviews (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 decision_uuid TEXT NOT NULL UNIQUE,
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=16384),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(post_uuid,decision_uuid) REFERENCES post_attachment_decisions(post_uuid,uuid)
);
CREATE INDEX attachment_selection_reviews_post ON attachment_selection_reviews(post_uuid,request_uuid);
CREATE TRIGGER attachment_selection_review_immutable BEFORE UPDATE ON attachment_selection_reviews
BEGIN SELECT RAISE(ABORT,'source-list review receipts are immutable'); END;
CREATE TRIGGER attachment_selection_review_scope BEFORE INSERT ON attachment_selection_reviews
WHEN NOT EXISTS(SELECT 1 FROM post_attachment_decisions d
 JOIN post_attachment_selections h ON h.post_uuid=d.post_uuid AND h.decision_uuid=d.uuid
 JOIN source_posts p ON p.uuid=d.post_uuid AND p.state='active'
 WHERE d.uuid=NEW.decision_uuid AND d.post_uuid=NEW.post_uuid AND d.origin='review'
 AND p.revision=d.revision
 AND json_extract(NEW.request_json,'$.request_uuid') IS NEW.request_uuid
 AND json_extract(NEW.request_json,'$.post_uuid') IS d.post_uuid
 AND json_extract(NEW.request_json,'$.post_revision') IS d.revision-1
 AND json_extract(NEW.request_json,'$.mode') IS d.mode
 AND coalesce(json_extract(NEW.request_json,'$.reason'),'') IS d.reason
 AND json_extract(NEW.request_json,'$.capture_uuid') IS d.capture_uuid)
BEGIN SELECT RAISE(ABORT,'source-list review is outside its decision scope'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000085,'Native source-list selection review and retry receipts','{}');
