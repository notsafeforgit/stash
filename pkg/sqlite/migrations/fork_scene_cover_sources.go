package migrations

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

func reconcileSceneCoverSources(ctx context.Context, db *sqlx.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS fork_scene_cover_sources (
  scene_id INTEGER PRIMARY KEY REFERENCES scenes(id) ON DELETE CASCADE,
  cover_checksum TEXT NOT NULL,
  source_file_id INTEGER NOT NULL,
  at REAL NOT NULL CHECK (at >= 0),
  source_fingerprint BLOB NOT NULL
);
DELETE FROM fork_scene_cover_sources
WHERE NOT EXISTS (
  SELECT 1 FROM scenes
  WHERE scenes.id = fork_scene_cover_sources.scene_id
    AND scenes.cover_blob = fork_scene_cover_sources.cover_checksum
)`)
	return err
}

func init() {
	sqlite.RegisterForkMigration(7, "scene cover sources", reconcileSceneCoverSources)
	sqlite.RegisterForkReconciler("scene cover sources", reconcileSceneCoverSources)
}
