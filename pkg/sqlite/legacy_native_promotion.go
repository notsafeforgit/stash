// LEGACY-UPGRADE-ONLY: upstream/frozen-fork input preparation for migration
// 1000000. Reached only from the historical upgrade chain, never native bootstrap.
// Retirement requires retiring those inputs and interrupted-upgrade recovery;
// see migrations/README.md and PROJECT.md.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var promotedTables = map[string]string{
	"fork_performer_autotag_ignored_names": "performer_autotag_ignored_names",
	"fork_saved_filter_state":              "saved_filter_state",
	"fork_video_file_metadata":             "video_file_metadata",
	"fork_image_file_metadata":             "image_file_metadata",
	"fork_scene_cover_sources":             "scene_cover_sources",
	"fork_shares":                          "shares",
	"fork_share_sessions":                  "share_sessions",
	"fork_file_deletions":                  "file_deletions",
}

func (m *Migrator) prepareNativePromotion(ctx context.Context) error {
	known := map[string]string{
		"fork_schema_migrations":     "table",
		"fork_scenes_created_at":     "index",
		"fork_images_created_at":     "index",
		"fork_shares_created":        "index",
		"fork_share_sessions_share":  "index",
		"fork_share_sessions_expiry": "index",
	}
	for old := range promotedTables {
		known[old] = "table"
	}
	var objects []struct {
		Name string `db:"name"`
		Type string `db:"type"`
	}
	if err := m.conn.SelectContext(ctx, &objects, "SELECT name, type FROM sqlite_schema WHERE name GLOB 'fork_*' ORDER BY name"); err != nil {
		return err
	}
	for _, object := range objects {
		if known[object.Name] != object.Type {
			return fmt.Errorf("unrecognized legacy fork object %s (%s); preserve and map it before promotion", object.Name, object.Type)
		}
	}
	for _, name := range promotedTables {
		var exists bool
		if err := m.conn.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name = ?)", name); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("native promotion destination %s already exists", name)
		}
	}
	if err := m.snapshotLegacyHistory(ctx); err != nil {
		return err
	}
	// Historical conversions run once, before the independent schema. Their
	// final reconciliation preserves pending filter conflicts for explicit review.
	if err := m.RunAllForkMigrations(ctx); err != nil {
		return err
	}
	if err := m.RunForkReconcilers(ctx); err != nil {
		return err
	}
	var table, parent string
	var rowID sql.NullInt64
	var fkID int
	err := m.conn.QueryRowContext(ctx, "PRAGMA foreign_key_check").Scan(&table, &rowID, &parent, &fkID)
	if err == nil {
		return fmt.Errorf("legacy database has a foreign-key violation in %s referencing %s (row %v, constraint %d)", table, parent, rowID, fkID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("checking legacy database foreign keys: %w", err)
	}
	return m.snapshotLegacyHistory(ctx)
}

func (m *Migrator) snapshotLegacyHistory(ctx context.Context) error {
	tx, err := m.conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS native_promotion_input_history (
  track TEXT NOT NULL,
  version INTEGER NOT NULL,
  name TEXT NOT NULL,
  applied_at DATETIME NOT NULL,
  PRIMARY KEY (track, version)
);
INSERT OR IGNORE INTO native_promotion_input_history
SELECT 'legacy-primary', version, 'legacy Stash schema', CURRENT_TIMESTAMP
FROM schema_migrations WHERE dirty = 0`); err != nil {
		return err
	}
	var exists bool
	if err := tx.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name = 'fork_schema_migrations')"); err != nil {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO native_promotion_input_history
SELECT 'legacy-fork', version, name, applied_at FROM fork_schema_migrations`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
