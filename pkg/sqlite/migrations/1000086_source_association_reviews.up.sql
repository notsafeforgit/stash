-- Reviewing source links does not synchronize gallery members or edit files.
CREATE TABLE gallery_association_reviews (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 decision_uuid TEXT NOT NULL UNIQUE,
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=16384),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(post_uuid,decision_uuid) REFERENCES post_gallery_decisions(post_uuid,uuid)
);
CREATE INDEX gallery_association_reviews_post ON gallery_association_reviews(post_uuid,request_uuid);
CREATE TRIGGER gallery_association_review_immutable BEFORE UPDATE ON gallery_association_reviews
BEGIN SELECT RAISE(ABORT,'gallery association review receipts are immutable'); END;
CREATE TRIGGER gallery_association_review_scope BEFORE INSERT ON gallery_association_reviews
WHEN NOT EXISTS(SELECT 1 FROM post_gallery_decisions d
 JOIN post_gallery_links h ON h.post_uuid=d.post_uuid AND h.decision_uuid=d.uuid
 JOIN source_posts p ON p.uuid=d.post_uuid AND p.state='active'
 WHERE d.uuid=NEW.decision_uuid AND d.post_uuid=NEW.post_uuid AND d.origin='review'
 AND d.selection_uuid IS NULL AND p.revision=d.revision
 AND json_extract(NEW.request_json,'$.request_uuid') IS NEW.request_uuid
 AND json_extract(NEW.request_json,'$.post_uuid') IS d.post_uuid
 AND json_extract(NEW.request_json,'$.post_revision') IS d.revision-1
 AND json_extract(NEW.request_json,'$.state') IS d.state
 AND coalesce(json_extract(NEW.request_json,'$.reason'),'') IS d.reason
 AND json_extract(NEW.request_json,'$.gallery_uuid') IS d.gallery_uuid
 AND ((d.state='disabled' AND json_extract(NEW.request_json,'$.gallery_revision') IS NULL)
 OR EXISTS(SELECT 1 FROM archive_entities a WHERE a.uuid=d.gallery_uuid AND a.kind='gallery'
 AND a.state='active' AND a.revision=json_extract(NEW.request_json,'$.gallery_revision'))))
BEGIN SELECT RAISE(ABORT,'gallery association review is outside its decision scope'); END;

CREATE TABLE attachment_media_reviews (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 attachment_uuid TEXT NOT NULL REFERENCES source_attachments(uuid),
 decision_uuid TEXT NOT NULL UNIQUE,
 request_json TEXT NOT NULL CHECK(json_valid(request_json) AND json_type(request_json)='object' AND length(CAST(request_json AS BLOB))<=16384),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(attachment_uuid,decision_uuid) REFERENCES attachment_media_decisions(attachment_uuid,uuid)
);
CREATE INDEX attachment_media_reviews_attachment ON attachment_media_reviews(attachment_uuid,request_uuid);
CREATE INDEX attachment_media_reviews_post ON attachment_media_reviews(post_uuid,request_uuid);
CREATE TRIGGER attachment_media_review_immutable BEFORE UPDATE ON attachment_media_reviews
BEGIN SELECT RAISE(ABORT,'attachment media review receipts are immutable'); END;
CREATE TRIGGER attachment_media_review_scope BEFORE INSERT ON attachment_media_reviews
WHEN NOT EXISTS(SELECT 1 FROM attachment_media_decisions d
 JOIN attachment_media_links h ON h.attachment_uuid=d.attachment_uuid AND h.decision_uuid=d.uuid
 JOIN source_attachments a ON a.uuid=d.attachment_uuid AND a.post_uuid=NEW.post_uuid
 JOIN source_posts p ON p.uuid=a.post_uuid AND p.state='active'
 WHERE d.uuid=NEW.decision_uuid AND d.attachment_uuid=NEW.attachment_uuid AND d.origin='review'
 AND a.revision=d.revision
 AND json_extract(NEW.request_json,'$.request_uuid') IS NEW.request_uuid
 AND json_extract(NEW.request_json,'$.post_uuid') IS p.uuid
 AND json_extract(NEW.request_json,'$.post_revision') IS p.revision-1
 AND json_extract(NEW.request_json,'$.attachment_uuid') IS d.attachment_uuid
 AND json_extract(NEW.request_json,'$.attachment_revision') IS d.revision-1
 AND json_extract(NEW.request_json,'$.state') IS d.state
 AND coalesce(json_extract(NEW.request_json,'$.reason'),'') IS d.reason
 AND json_extract(NEW.request_json,'$.media_uuid') IS d.media_uuid
 AND ((d.state!='linked' AND json_extract(NEW.request_json,'$.media_revision') IS NULL)
 OR EXISTS(SELECT 1 FROM archive_entities m WHERE m.uuid=d.media_uuid AND m.kind IN ('scene','image')
 AND m.state='active' AND m.revision=json_extract(NEW.request_json,'$.media_revision'))))
BEGIN SELECT RAISE(ABORT,'attachment media review is outside its decision scope'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000086,'Native gallery and attachment association review receipts','{}');
