-- Share unchanged capture content, retaining existing IDs used by imported
-- media associations. Subsequent identical producer events update one sighting
-- range rather than creating another capture and publisher history.
CREATE TABLE source_capture_content (
 capture_uuid TEXT NOT NULL PRIMARY KEY,
 post_uuid TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 FOREIGN KEY(post_uuid,capture_uuid) REFERENCES source_captures(post_uuid,uuid)
) WITHOUT ROWID;
CREATE INDEX source_capture_content_post ON source_capture_content(post_uuid,digest,capture_uuid);
CREATE TABLE source_capture_sightings (
 capture_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_captures(uuid),
 count INTEGER NOT NULL CHECK(count>0),
 first_seen DATETIME NOT NULL CHECK(length(first_seen)=30 AND julianday(first_seen) IS NOT NULL),
 last_seen DATETIME NOT NULL CHECK(length(last_seen)=30 AND julianday(last_seen) IS NOT NULL),
 CHECK(first_seen<=last_seen)
) WITHOUT ROWID;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000101,'Share unchanged post content and summarize repeat sightings','{}');
