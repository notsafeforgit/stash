package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

const (
	// NativeSchemaBaseline separates this lineage from upstream migrations and
	// the historical private 998/999 schemas. It also fits GraphQL's Int range.
	NativeSchemaBaseline uint = 1000000
	NativeSchemaLineage       = "org.notsafeforgit.stash.native-archive"
	lastCompatibleSchema uint = 86
)

// validateDatabaseLineage runs before opening a writable migration connection.
// A matching version number alone is never sufficient to identify our schema.
func validateDatabaseLineage(path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "mode=ro"}
	conn, err := sqlx.Open(sqlite3Driver, uri.String())
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	var tables []string
	if err := conn.Select(&tables, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"); err != nil {
		return fmt.Errorf("reading database identity: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}
	present := make(map[string]bool, len(tables))
	for _, name := range tables {
		present[name] = true
	}
	if !present["schema_migrations"] {
		return errors.New("database is not a supported Stash schema (migration ledger missing)")
	}
	var versions []struct {
		Version uint `db:"version"`
		Dirty   bool `db:"dirty"`
	}
	if err := conn.Select(&versions, "SELECT version, dirty FROM schema_migrations"); err != nil {
		return fmt.Errorf("reading database version: %w", err)
	}
	if len(versions) == 0 && len(tables) == 1 {
		return nil // a newly initialized migration driver, before its first step
	}
	if len(versions) != 1 {
		return errors.New("database migration ledger must contain exactly one version")
	}
	version := versions[0].Version
	if versions[0].Dirty {
		return fmt.Errorf("database migration %d is incomplete; recover the migration or restore its backup before opening", version)
	}
	if present["native_schema"] {
		var lineage string
		if err := conn.Get(&lineage, "SELECT lineage FROM native_schema WHERE singleton = 1"); err != nil {
			return fmt.Errorf("reading native database lineage: %w", err)
		}
		if lineage != NativeSchemaLineage || version < NativeSchemaBaseline {
			return fmt.Errorf("unsupported database lineage %q at schema %d", lineage, version)
		}
		if version > GetRequiredSchemaVersion() {
			return &MismatchedSchemaVersionError{CurrentSchemaVersion: version, RequiredSchemaVersion: GetRequiredSchemaVersion()}
		}
		for _, name := range []string{"video_file_metadata", "image_file_metadata", "scene_cover_sources", "shares", "share_sessions", "file_deletions", "legacy_schema_history"} {
			if !present[name] {
				return fmt.Errorf("native database schema is incomplete: missing %s", name)
			}
		}
		if present[forkSchemaMigrationsTable] {
			return errors.New("native database still contains an active fork migration ledger")
		}
		if version >= NativeSchemaBaseline+1 {
			for _, name := range []string{"native_migration_history", "saved_filter_import_conflicts"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			var hasAST bool
			if err := conn.Get(&hasAST, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('saved_filters') WHERE name = 'filter_ast')"); err != nil {
				return err
			}
			if !hasAST {
				return errors.New("native database schema is incomplete: missing saved_filters.filter_ast")
			}
		}
		if version >= NativeSchemaBaseline+2 {
			if !present["performer_names"] {
				return errors.New("native database schema is incomplete: missing performer_names")
			}
			var missingPrimary bool
			if err := conn.Get(&missingPrimary, `SELECT EXISTS(SELECT 1 FROM performers
WHERE NOT EXISTS (SELECT 1 FROM performer_names WHERE performer_id = performers.id AND position = 0))`); err != nil {
				return err
			}
			if missingPrimary {
				return errors.New("native database schema is incomplete: performer has no canonical name")
			}
		}
		return nil
	}
	if version >= NativeSchemaBaseline {
		return fmt.Errorf("schema %d has no native lineage marker; refusing to open an unrelated database", version)
	}
	if version > lastCompatibleSchema && version != legacyForkFirstSchemaVersion && version != legacyForkLastSchemaVersion {
		return fmt.Errorf("unsupported legacy Stash schema %d; last compatible input is %d", version, lastCompatibleSchema)
	}
	if present[forkSchemaMigrationsTable] {
		var forkVersion sql.NullInt64
		if err := conn.Get(&forkVersion, "SELECT MAX(version) FROM fork_schema_migrations"); err != nil {
			return err
		}
		if forkVersion.Valid && (forkVersion.Int64 < 0 || uint(forkVersion.Int64) > GetRequiredForkSchemaVersion()) {
			return fmt.Errorf("unsupported legacy fork schema %d", forkVersion.Int64)
		}
	}
	return nil
}

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
