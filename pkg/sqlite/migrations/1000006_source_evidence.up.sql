CREATE TABLE source_posts (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'forgotten')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE source_post_identifiers (
  namespace TEXT NOT NULL CHECK (length(namespace) BETWEEN 1 AND 128),
  value TEXT NOT NULL CHECK (length(value) BETWEEN 1 AND 2048),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  PRIMARY KEY (namespace, value)
) WITHOUT ROWID;
CREATE INDEX source_post_identifiers_post ON source_post_identifiers(post_uuid, namespace, value);

-- Digests address exact json-v1 bytes. Compression changes physical storage,
-- never the checksum or retained evidence. Profile bodies and attachment deltas
-- can use the same payload record independently of their semantic roles.
CREATE TABLE source_payloads (
  digest TEXT NOT NULL PRIMARY KEY CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
  encoding TEXT NOT NULL CHECK (encoding IN ('json', 'gzip')),
  byte_length INTEGER NOT NULL CHECK (byte_length BETWEEN 2 AND 4194304),
  data BLOB NOT NULL CHECK (length(data) BETWEEN 2 AND 4194304)
) WITHOUT ROWID;
CREATE TABLE source_profile_bodies (
  hash TEXT NOT NULL PRIMARY KEY CHECK (length(hash) = 64 AND hash NOT GLOB '*[^0-9a-f]*'),
  namespace TEXT NOT NULL CHECK (length(namespace) BETWEEN 1 AND 128),
  payload_digest TEXT NOT NULL REFERENCES source_payloads(digest)
) WITHOUT ROWID;
CREATE INDEX source_profile_bodies_payload ON source_profile_bodies(payload_digest);

CREATE TABLE source_post_revisions (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  signature TEXT NOT NULL CHECK (length(signature) = 64 AND signature NOT GLOB '*[^0-9a-f]*'),
  body_digest TEXT NOT NULL REFERENCES source_payloads(digest),
  metadata TEXT NOT NULL CHECK (length(metadata) <= 262144 AND json_valid(metadata) AND json_type(metadata) = 'object'),
  structure_version TEXT NOT NULL,
  UNIQUE (post_uuid, signature),
  UNIQUE (post_uuid, uuid)
);
CREATE INDEX source_post_revisions_payload ON source_post_revisions(body_digest);
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
  captured_at DATETIME NOT NULL CHECK (length(captured_at) = 30 AND substr(captured_at, 11, 1) = 'T' AND substr(captured_at, 20, 1) = '.' AND substr(captured_at, 30, 1) = 'Z' AND julianday(captured_at) IS NOT NULL),
  extractor_version TEXT CHECK (extractor_version IS NULL OR length(extractor_version) <= 128),
  retention_policy TEXT NOT NULL CHECK (length(retention_policy) BETWEEN 1 AND 128),
  patch_digest TEXT NOT NULL REFERENCES source_payloads(digest),
  signature TEXT NOT NULL CHECK (length(signature) = 64 AND signature NOT GLOB '*[^0-9a-f]*'),
  FOREIGN KEY (post_uuid, revision_uuid) REFERENCES source_post_revisions(post_uuid, uuid)
);
CREATE INDEX source_captures_post ON source_captures(post_uuid, captured_at, uuid);
CREATE INDEX source_captures_revision ON source_captures(revision_uuid, captured_at, uuid);
CREATE INDEX source_captures_payload ON source_captures(patch_digest);
CREATE TABLE source_capture_profiles (
  capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
  part TEXT NOT NULL CHECK (part IN ('shared', 'patch')),
  path TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 8192 AND substr(path, 1, 1) = '/'),
  profile_hash TEXT NOT NULL REFERENCES source_profile_bodies(hash),
  PRIMARY KEY (capture_uuid, part, path)
) WITHOUT ROWID;
CREATE INDEX source_capture_profiles_body ON source_capture_profiles(profile_hash, capture_uuid);

CREATE TRIGGER source_payload_immutable BEFORE UPDATE ON source_payloads
BEGIN SELECT RAISE(ABORT, 'retained source payloads are immutable'); END;
CREATE TRIGGER source_profile_body_immutable BEFORE UPDATE ON source_profile_bodies
BEGIN SELECT RAISE(ABORT, 'retained source profiles are immutable'); END;
CREATE TRIGGER source_post_revision_immutable BEFORE UPDATE ON source_post_revisions
BEGIN SELECT RAISE(ABORT, 'source post revisions are immutable'); END;
CREATE TRIGGER source_capture_immutable BEFORE UPDATE ON source_captures
BEGIN SELECT RAISE(ABORT, 'source captures are immutable'); END;
CREATE TRIGGER source_capture_profile_immutable BEFORE UPDATE ON source_capture_profiles
BEGIN SELECT RAISE(ABORT, 'source capture profile references are immutable'); END;
CREATE TRIGGER source_post_identifier_immutable BEFORE UPDATE ON source_post_identifiers
BEGIN SELECT RAISE(ABORT, 'source post identifiers require an explicit merge'); END;
CREATE TRIGGER source_post_no_resurrection BEFORE UPDATE OF state ON source_posts
WHEN OLD.state = 'forgotten' AND NEW.state != OLD.state
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot be resurrected'); END;
CREATE TRIGGER source_capture_active_post BEFORE INSERT ON source_captures
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten source post cannot receive captures'); END;

INSERT INTO native_migration_history(version, name, details)
VALUES (1000006, 'Shared post/profile bodies and reconstructable source captures', '{}');
