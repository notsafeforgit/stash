-- Reviewed post associations are independent of optional attachment slots.
-- Existing observations are evidence only: migration selects no new links.
CREATE TABLE post_media_decisions (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
 AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
 AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
 AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 post_revision INTEGER NOT NULL CHECK(post_revision>1),
 media_revision INTEGER NOT NULL CHECK(media_revision>0),
 state TEXT NOT NULL CHECK(state IN ('linked','unlinked','undecided')),
 origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
 reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB))<=4096),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(post_uuid,post_revision),
 UNIQUE(post_uuid,media_uuid,uuid)
);
CREATE INDEX post_media_decisions_media ON post_media_decisions(media_uuid,post_uuid,post_revision);
CREATE TABLE post_media_links (
 post_uuid TEXT NOT NULL,
 media_uuid TEXT NOT NULL,
 decision_uuid TEXT NOT NULL UNIQUE,
 PRIMARY KEY(post_uuid,media_uuid),
 FOREIGN KEY(post_uuid,media_uuid,decision_uuid) REFERENCES post_media_decisions(post_uuid,media_uuid,uuid) ON UPDATE CASCADE
) WITHOUT ROWID;
CREATE INDEX post_media_links_media ON post_media_links(media_uuid,post_uuid);
CREATE TABLE post_media_supersessions (
 previous_uuid TEXT PRIMARY KEY NOT NULL REFERENCES post_media_decisions(uuid),
 decision_uuid TEXT NOT NULL REFERENCES post_media_decisions(uuid)
) WITHOUT ROWID;
CREATE INDEX post_media_supersessions_decision ON post_media_supersessions(decision_uuid);
CREATE TRIGGER post_media_supersession_scope BEFORE INSERT ON post_media_supersessions
WHEN NOT EXISTS(SELECT 1 FROM post_media_decisions old JOIN post_media_decisions current ON current.uuid=NEW.decision_uuid
WHERE old.uuid=NEW.previous_uuid AND old.post_uuid=current.post_uuid AND old.post_revision<current.post_revision)
BEGIN SELECT RAISE(ABORT,'post media replacement requires a later decision for the same post'); END;
CREATE TRIGGER post_media_supersession_immutable BEFORE UPDATE ON post_media_supersessions
BEGIN SELECT RAISE(ABORT,'post media replacements are immutable'); END;
CREATE TRIGGER post_media_decision_scope BEFORE INSERT ON post_media_decisions
BEGIN
 SELECT RAISE(ABORT,'post media requires an active post and its current revision')
 WHERE NOT EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state='active' AND revision=NEW.post_revision);
 SELECT RAISE(ABORT,'post media requires a current scene or image')
 WHERE NOT EXISTS(SELECT 1 FROM archive_entities WHERE uuid=NEW.media_uuid AND state='active'
 AND kind IN ('scene','image') AND revision=NEW.media_revision);
END;
CREATE TRIGGER post_media_decision_immutable BEFORE UPDATE ON post_media_decisions
WHEN NEW.uuid!=OLD.uuid OR NEW.post_uuid!=OLD.post_uuid OR NEW.post_revision!=OLD.post_revision
 OR NEW.media_revision!=OLD.media_revision OR NEW.state!=OLD.state OR NEW.origin!=OLD.origin
 OR NEW.reason!=OLD.reason OR NEW.request_digest!=OLD.request_digest OR NEW.created_at!=OLD.created_at
 OR (NEW.media_uuid!=OLD.media_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.media_uuid))
BEGIN SELECT RAISE(ABORT,'post media decisions are immutable'); END;
CREATE TRIGGER post_media_head_forward BEFORE UPDATE OF decision_uuid ON post_media_links
WHEN NEW.decision_uuid!=OLD.decision_uuid AND (SELECT post_revision FROM post_media_decisions WHERE uuid=NEW.decision_uuid)
 <= (SELECT post_revision FROM post_media_decisions WHERE uuid=OLD.decision_uuid)
BEGIN SELECT RAISE(ABORT,'post media head cannot move backwards'); END;

CREATE TABLE metadata_decision_post_media (
 decision_uuid TEXT PRIMARY KEY NOT NULL REFERENCES metadata_field_decisions(uuid),
 post_media_decision_uuid TEXT NOT NULL REFERENCES post_media_decisions(uuid)
) WITHOUT ROWID;
CREATE INDEX metadata_decision_post_media_source ON metadata_decision_post_media(post_media_decision_uuid);
CREATE TRIGGER metadata_decision_post_media_scope BEFORE INSERT ON metadata_decision_post_media
WHEN NOT EXISTS(SELECT 1 FROM metadata_field_decisions d
 JOIN source_captures c ON c.uuid=d.capture_uuid
 JOIN post_media_decisions p ON p.uuid=NEW.post_media_decision_uuid AND p.post_uuid=c.post_uuid AND p.state='linked'
 WHERE d.uuid=NEW.decision_uuid AND d.origin='source'
 AND d.entity_uuid IN (WITH RECURSIVE targets(uuid,depth) AS (
 SELECT p.media_uuid,0 UNION ALL SELECT a.redirect_to,t.depth+1 FROM targets t JOIN archive_entities a ON a.uuid=t.uuid
 WHERE a.state='redirected' AND t.depth<127
 ) SELECT uuid FROM targets))
BEGIN SELECT RAISE(ABORT,'metadata post association requires matching source provenance'); END;
CREATE TRIGGER metadata_decision_post_media_immutable BEFORE UPDATE ON metadata_decision_post_media
BEGIN SELECT RAISE(ABORT,'metadata post association provenance is immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000081,'Reviewed post media associations without invented attachment slots','{}');
