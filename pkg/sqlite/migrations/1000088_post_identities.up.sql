-- Captures, revisions, identifiers and receipts retain their original post
-- owner. Canonical identity is a separate indexed relation; no historical
-- source row or assertion is rewritten by consolidation.
CREATE TABLE source_post_identities (
 post_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_posts(uuid),
 canonical_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 UNIQUE(post_uuid,canonical_uuid)
) WITHOUT ROWID;
INSERT INTO source_post_identities(post_uuid,canonical_uuid) SELECT uuid,uuid FROM source_posts;
CREATE INDEX source_post_identities_canonical ON source_post_identities(canonical_uuid,post_uuid);

CREATE TABLE source_post_consolidations (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 source_uuid TEXT NOT NULL UNIQUE REFERENCES source_posts(uuid),
 destination_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 source_revision INTEGER NOT NULL CHECK(source_revision>0),
 destination_revision INTEGER NOT NULL CHECK(destination_revision>0),
 member_count INTEGER NOT NULL CHECK(member_count BETWEEN 2 AND 256),
 identity_signature TEXT NOT NULL CHECK(length(identity_signature)=64 AND identity_signature NOT GLOB '*[^0-9a-f]*'),
 review_signature TEXT NOT NULL CHECK(length(review_signature)=64 AND review_signature NOT GLOB '*[^0-9a-f]*'),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 origin TEXT NOT NULL CHECK(origin IN ('review','migration')),
 reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB))<=4096),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK(source_uuid!=destination_uuid)
);
CREATE INDEX source_post_consolidations_destination ON source_post_consolidations(destination_uuid,sequence);
CREATE TABLE source_post_consolidation_context (
 request_uuid TEXT PRIMARY KEY NOT NULL,
 source_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 destination_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 CHECK(source_uuid!=destination_uuid)
);
CREATE TRIGGER source_post_consolidation_scope BEFORE INSERT ON source_post_consolidations
BEGIN
 SELECT RAISE(ABORT,'post consolidation requires its managed write context') WHERE NOT EXISTS(
  SELECT 1 FROM source_post_consolidation_context w WHERE w.request_uuid=NEW.uuid
  AND w.source_uuid=NEW.source_uuid AND w.destination_uuid=NEW.destination_uuid);
 SELECT RAISE(ABORT,'post consolidation requires current active roots') WHERE NOT EXISTS(
  SELECT 1 FROM source_posts s JOIN source_post_identities si ON si.post_uuid=s.uuid
  JOIN source_posts d JOIN source_post_identities di ON di.post_uuid=d.uuid
  WHERE s.uuid=NEW.source_uuid AND d.uuid=NEW.destination_uuid
  AND si.canonical_uuid=s.uuid AND di.canonical_uuid=d.uuid AND s.state='active' AND d.state='active'
  AND s.revision=NEW.source_revision AND d.revision=NEW.destination_revision);
 SELECT RAISE(ABORT,'post consolidation member scope changed') WHERE NEW.member_count != (
  SELECT count(*) FROM (SELECT 1 FROM source_post_identities WHERE canonical_uuid IN (NEW.source_uuid,NEW.destination_uuid) LIMIT 257))
  OR EXISTS(SELECT 1 FROM source_post_identities i JOIN source_posts p ON p.uuid=i.post_uuid
   WHERE i.canonical_uuid IN (NEW.source_uuid,NEW.destination_uuid) AND p.state!='active');
 -- A shared URL, caption or media item cannot override different qualified
 -- upstream identities. Legacy catalog keys remain original aliases.
 SELECT RAISE(ABORT,'post consolidation has conflicting upstream identifiers') WHERE 1 < (
  SELECT count(*) FROM (SELECT DISTINCT i.namespace,i.value FROM source_post_identities p
  JOIN source_post_identifiers i ON i.post_uuid=p.post_uuid
  WHERE p.canonical_uuid IN (NEW.source_uuid,NEW.destination_uuid) AND substr(i.namespace,1,7)!='legacy:' LIMIT 2));
END;
CREATE TRIGGER source_post_consolidation_publish AFTER INSERT ON source_post_consolidations
BEGIN
 UPDATE source_posts SET revision=revision+1 WHERE uuid IN (
  SELECT post_uuid FROM source_post_identities WHERE canonical_uuid IN (NEW.source_uuid,NEW.destination_uuid));
 UPDATE source_post_identities SET canonical_uuid=NEW.destination_uuid WHERE canonical_uuid=NEW.source_uuid;
END;
CREATE TRIGGER source_post_consolidation_immutable BEFORE UPDATE ON source_post_consolidations
BEGIN SELECT RAISE(ABORT,'post consolidation history is immutable'); END;
CREATE TRIGGER source_post_root_initial BEFORE INSERT ON source_post_identities
WHEN NEW.canonical_uuid!=NEW.post_uuid
BEGIN SELECT RAISE(ABORT,'new posts must begin as independent identities'); END;
CREATE TRIGGER source_post_root_created AFTER INSERT ON source_posts
BEGIN INSERT INTO source_post_identities(post_uuid,canonical_uuid) VALUES(NEW.uuid,NEW.uuid); END;
CREATE TRIGGER source_post_root_update BEFORE UPDATE ON source_post_identities
BEGIN
 SELECT RAISE(ABORT,'source post identity owners are immutable') WHERE NEW.post_uuid!=OLD.post_uuid;
 SELECT RAISE(ABORT,'canonical posts change only through reviewed consolidation') WHERE NEW.canonical_uuid!=OLD.canonical_uuid AND NOT EXISTS(
  SELECT 1 FROM source_post_consolidation_context w
  JOIN source_post_consolidations c ON c.uuid=w.request_uuid AND c.source_uuid=w.source_uuid AND c.destination_uuid=w.destination_uuid
  JOIN source_posts s ON s.uuid=w.source_uuid AND s.state='active' AND s.revision=c.source_revision+1
  JOIN source_posts d ON d.uuid=w.destination_uuid AND d.state='active' AND d.revision=c.destination_revision+1
  JOIN source_post_identities di ON di.post_uuid=d.uuid AND di.canonical_uuid=d.uuid
  WHERE w.source_uuid=OLD.canonical_uuid AND w.destination_uuid=NEW.canonical_uuid);
END;
CREATE TRIGGER source_post_uuid_immutable BEFORE UPDATE OF uuid ON source_posts
WHEN OLD.uuid!=NEW.uuid
BEGIN SELECT RAISE(ABORT,'source post UUIDs are immutable'); END;
CREATE TRIGGER source_post_group_identifier BEFORE INSERT ON source_post_identifiers
WHEN substr(NEW.namespace,1,7)!='legacy:'
 AND EXISTS(SELECT 1 FROM source_post_identities p JOIN source_post_identities m ON m.canonical_uuid=p.canonical_uuid
  WHERE p.post_uuid=NEW.post_uuid AND m.post_uuid!=p.post_uuid)
 AND EXISTS(SELECT 1 FROM source_post_identities p JOIN source_post_identities m ON m.canonical_uuid=p.canonical_uuid
  JOIN source_post_identifiers i ON i.post_uuid=m.post_uuid WHERE p.post_uuid=NEW.post_uuid
  AND substr(i.namespace,1,7)!='legacy:' AND (i.namespace!=NEW.namespace OR i.value!=NEW.value))
BEGIN SELECT RAISE(ABORT,'consolidated post cannot acquire a contradictory upstream identity'); END;
CREATE TRIGGER source_post_group_forgotten AFTER UPDATE OF state ON source_posts
WHEN NEW.state='forgotten' AND OLD.state!='forgotten'
BEGIN
 UPDATE source_posts SET state='forgotten',revision=revision+1
 WHERE uuid IN (SELECT m.post_uuid FROM source_post_identities p
  JOIN source_post_identities m ON m.canonical_uuid=p.canonical_uuid WHERE p.post_uuid=NEW.uuid)
 AND state!='forgotten';
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000088,'Canonical source post identities with immutable consolidation receipts','{}');
