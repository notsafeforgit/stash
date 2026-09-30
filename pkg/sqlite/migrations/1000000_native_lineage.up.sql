-- The migration driver executes this entire promotion in one transaction.
-- Historical sidecars have been completed and reconciled by the preflight.
CREATE TABLE native_schema (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  lineage TEXT NOT NULL,
  promoted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO native_schema (singleton, lineage)
VALUES (1, 'org.notsafeforgit.stash.native-archive');

ALTER TABLE native_promotion_input_history RENAME TO legacy_schema_history;
DROP TABLE fork_schema_migrations;

ALTER TABLE fork_video_file_metadata RENAME TO video_file_metadata;
ALTER TABLE fork_image_file_metadata RENAME TO image_file_metadata;
ALTER TABLE fork_scene_cover_sources RENAME TO scene_cover_sources;
ALTER TABLE fork_shares RENAME TO shares;
ALTER TABLE fork_share_sessions RENAME TO share_sessions;
ALTER TABLE fork_file_deletions RENAME TO file_deletions;

-- Keep pending filter conflicts and per-name policies intact while their
-- repositories are converted to the canonical AST and performer-name models.
ALTER TABLE fork_saved_filter_state RENAME TO saved_filter_state;
ALTER TABLE fork_performer_autotag_ignored_names RENAME TO performer_autotag_ignored_names;

-- Older clients could omit these nullable columns. Their empty representation
-- has the same meaning and is required by the typed saved-filter repository.
UPDATE saved_filters SET
  find_filter = COALESCE(find_filter, ''),
  object_filter = COALESCE(object_filter, ''),
  ui_options = COALESCE(ui_options, '');

DROP INDEX fork_shares_created;
DROP INDEX fork_share_sessions_share;
DROP INDEX fork_share_sessions_expiry;
CREATE INDEX shares_created ON shares(created_at DESC, id);
CREATE INDEX share_sessions_share ON share_sessions(share_id);
CREATE INDEX share_sessions_expiry ON share_sessions(expires_at);

-- The large covering indexes retain their historical names to avoid a rebuild.
-- They are now owned by this migration sequence, not a startup reconciler.
