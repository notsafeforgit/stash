-- Unknown observation time is null. Recording an old saved payload today
-- does not turn today into the original collector's observation time.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_capture_time_rows AS SELECT rowid AS original_rowid, * FROM source_captures;
DROP TABLE source_captures;

CREATE TABLE source_captures (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  revision_uuid TEXT NOT NULL,
  origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 128),
  platform TEXT NOT NULL CHECK (length(platform) BETWEEN 1 AND 128),
  captured_at DATETIME CHECK (captured_at IS NULL OR (length(captured_at) = 30 AND substr(captured_at, 11, 1) = 'T' AND substr(captured_at, 20, 1) = '.' AND substr(captured_at, 30, 1) = 'Z' AND julianday(captured_at) IS NOT NULL)),
  extractor_version TEXT CHECK (extractor_version IS NULL OR length(extractor_version) <= 128),
  retention_policy TEXT NOT NULL CHECK (length(retention_policy) BETWEEN 1 AND 128),
  patch_digest TEXT NOT NULL REFERENCES source_payloads(digest),
  signature TEXT NOT NULL CHECK (length(signature) = 64 AND signature NOT GLOB '*[^0-9a-f]*'),
  recorded_at DATETIME CHECK (recorded_at IS NULL OR (length(recorded_at) = 30 AND substr(recorded_at, 11, 1) = 'T' AND substr(recorded_at, 20, 1) = '.' AND substr(recorded_at, 30, 1) = 'Z' AND julianday(recorded_at) IS NOT NULL)),
  CHECK ((captured_at IS NULL) != (recorded_at IS NULL)),
  FOREIGN KEY (post_uuid, revision_uuid) REFERENCES source_post_revisions(post_uuid, uuid)
);
CREATE INDEX source_captures_post ON source_captures(post_uuid, captured_at, uuid);
CREATE INDEX source_captures_revision ON source_captures(revision_uuid, captured_at, uuid);
CREATE INDEX source_captures_payload ON source_captures(patch_digest);

INSERT INTO source_captures(rowid,uuid,post_uuid,revision_uuid,origin,platform,captured_at,extractor_version,retention_policy,patch_digest,signature) SELECT original_rowid,uuid,post_uuid,revision_uuid,origin,platform,captured_at,extractor_version,retention_policy,patch_digest,signature FROM native_capture_time_rows;
DROP TABLE native_capture_time_rows;
CREATE UNIQUE INDEX source_captures_scope ON source_captures(post_uuid,uuid);
CREATE INDEX source_captures_order ON source_captures(post_uuid,coalesce(captured_at,recorded_at),uuid);
CREATE INDEX source_captures_unrecorded ON source_captures(uuid) WHERE captured_at IS NULL;
CREATE TRIGGER source_capture_immutable BEFORE UPDATE ON source_captures
BEGIN SELECT RAISE(ABORT, 'source captures are immutable'); END;
CREATE TRIGGER source_capture_active_post BEFORE INSERT ON source_captures
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot receive captures'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000061,'Distinguish unknown observation times from archive recording times','{}');
