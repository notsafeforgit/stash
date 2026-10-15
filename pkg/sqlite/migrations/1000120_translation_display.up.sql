-- Current display preference and restartable, bounded propagation work. The
-- source text and translation results remain separate immutable records.
CREATE TABLE source_post_translation_display (
  post_uuid TEXT PRIMARY KEY REFERENCES source_posts(uuid),
  target_language TEXT NOT NULL,
  pending BOOLEAN NOT NULL DEFAULT 1 CHECK(pending IN (0,1)),
  after_entity TEXT NOT NULL DEFAULT '',
  adopt_before DATETIME
);
CREATE INDEX source_post_translation_display_pending ON source_post_translation_display(post_uuid) WHERE pending=1;

CREATE TABLE translation_policy_defaults (
  namespace TEXT PRIMARY KEY,
  revision INTEGER NOT NULL CHECK(revision>0),
  definition TEXT NOT NULL CHECK(json_valid(definition))
);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000120,'Apply translations to inherited display text','{}');
