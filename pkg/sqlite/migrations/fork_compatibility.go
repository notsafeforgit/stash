package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
)

const createPerformerAutoTagIgnoredNamesTable = `
CREATE TABLE IF NOT EXISTS fork_performer_autotag_ignored_names (
  performer_id INTEGER NOT NULL REFERENCES performers(id) ON DELETE CASCADE,
  name TEXT NOT NULL COLLATE NOCASE,
  PRIMARY KEY (performer_id, name)
)`

const createSavedFilterStateTable = `
CREATE TABLE IF NOT EXISTS fork_saved_filter_state (
  saved_filter_id INTEGER PRIMARY KEY REFERENCES saved_filters(id) ON DELETE CASCADE,
  filter_ast BLOB NOT NULL DEFAULT '',
  legacy_object_filter BLOB NOT NULL DEFAULT '',
  pending_legacy_object_filter BLOB
)`

const createVideoFileMetadataTable = `
CREATE TABLE IF NOT EXISTS fork_video_file_metadata (
  file_id INTEGER PRIMARY KEY REFERENCES video_files(file_id) ON DELETE CASCADE,
  source_size INTEGER NOT NULL,
  source_mod_time DATETIME NOT NULL,
  video_stream_duration REAL,
  frame_count INTEGER,
  duration_mismatch BOOLEAN NOT NULL DEFAULT 0,
  bit_depth INTEGER,
  color_range VARCHAR(255),
  color_space VARCHAR(255),
  color_transfer VARCHAR(255),
  color_primaries VARCHAR(255)
)`

const createImageFileMetadataTable = `
CREATE TABLE IF NOT EXISTS fork_image_file_metadata (
  file_id INTEGER PRIMARY KEY REFERENCES image_files(file_id) ON DELETE CASCADE,
  source_size INTEGER NOT NULL,
  source_mod_time DATETIME NOT NULL,
  bit_depth INTEGER,
  color_range VARCHAR(255),
  color_space VARCHAR(255),
  color_transfer VARCHAR(255),
  color_primaries VARCHAR(255)
)`

func fork005CompatibilitySidecars(ctx context.Context, db *sqlx.DB) error {
	if err := migratePerformerAutoTagSidecar(ctx, db); err != nil {
		return err
	}
	if err := migrateSavedFilterSidecar(ctx, db); err != nil {
		return err
	}
	if err := migrateFileMetadataSidecars(ctx, db); err != nil {
		return err
	}
	if err := compactPrivateForkMigrationHistory(ctx, db); err != nil {
		return err
	}

	return nil
}

func compactPrivateForkMigrationHistory(ctx context.Context, db *sqlx.DB) error {
	var exists bool
	if err := db.GetContext(ctx, &exists, "SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'fork_schema_migrations')"); err != nil {
		return fmt.Errorf("checking private fork migration history: %w", err)
	}
	if !exists {
		return nil
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM fork_schema_migrations"); err != nil {
		return fmt.Errorf("compacting private fork migration history: %w", err)
	}
	return nil
}

func migratePerformerAutoTagSidecar(ctx context.Context, db *sqlx.DB) error {
	if _, err := db.ExecContext(ctx, createPerformerAutoTagIgnoredNamesTable); err != nil {
		return fmt.Errorf("creating performer auto-tag ignored names table: %w", err)
	}

	exists, err := forkColumnExists(ctx, db, "performer_aliases", "ignore_auto_tag")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if _, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO fork_performer_autotag_ignored_names (performer_id, name)
SELECT performer_id, alias FROM performer_aliases WHERE ignore_auto_tag = 1`); err != nil {
		return fmt.Errorf("migrating performer alias auto-tag settings: %w", err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE performer_aliases DROP COLUMN ignore_auto_tag"); err != nil {
		return fmt.Errorf("dropping performer_aliases.ignore_auto_tag: %w", err)
	}

	return nil
}

func migrateSavedFilterSidecar(ctx context.Context, db *sqlx.DB) error {
	if _, err := db.ExecContext(ctx, createSavedFilterStateTable); err != nil {
		return fmt.Errorf("creating saved-filter sidecar: %w", err)
	}

	exists, err := forkColumnExists(ctx, db, "saved_filters", "filter_ast")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	type savedFilterConversion struct {
		id           int
		ast          string
		objectFilter string
	}
	var conversions []savedFilterConversion
	if err := func() error {
		rows, err := db.QueryxContext(ctx, "SELECT id, filter_ast FROM saved_filters WHERE filter_ast != ''")
		if err != nil {
			return fmt.Errorf("reading saved-filter ASTs: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			var encodedAST string
			if err := rows.Scan(&id, &encodedAST); err != nil {
				return fmt.Errorf("scanning saved-filter AST: %w", err)
			}

			var ast models.FilterAST
			if err := json.Unmarshal([]byte(encodedAST), &ast); err != nil {
				return fmt.Errorf("decoding saved-filter %d AST: %w", id, err)
			}
			flat, _ := ast.FlatObjectFilter()
			encodedFlat, err := json.Marshal(flat)
			if err != nil {
				return fmt.Errorf("encoding saved-filter %d compatibility filter: %w", id, err)
			}
			conversions = append(conversions, savedFilterConversion{id: id, ast: encodedAST, objectFilter: string(encodedFlat)})
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("reading saved-filter ASTs: %w", err)
		}
		return nil
	}(); err != nil {
		return err
	}

	for _, conversion := range conversions {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fork_saved_filter_state (saved_filter_id, filter_ast, legacy_object_filter)
VALUES (?, ?, ?)
ON CONFLICT(saved_filter_id) DO UPDATE SET
  filter_ast = excluded.filter_ast,
  legacy_object_filter = excluded.legacy_object_filter`, conversion.id, conversion.ast, conversion.objectFilter); err != nil {
			return fmt.Errorf("storing saved-filter %d sidecar: %w", conversion.id, err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE saved_filters SET object_filter = ? WHERE id = ?", conversion.objectFilter, conversion.id); err != nil {
			return fmt.Errorf("restoring saved-filter %d compatibility filter: %w", conversion.id, err)
		}
	}

	if _, err := db.ExecContext(ctx, "ALTER TABLE saved_filters DROP COLUMN filter_ast"); err != nil {
		return fmt.Errorf("dropping saved_filters.filter_ast: %w", err)
	}

	return nil
}

type legacyMetadataColumn struct {
	table   string
	column  string
	sidecar string
}

func migrateFileMetadataSidecars(ctx context.Context, db *sqlx.DB) error {
	if _, err := db.ExecContext(ctx, createVideoFileMetadataTable); err != nil {
		return fmt.Errorf("creating video metadata sidecar: %w", err)
	}
	if _, err := db.ExecContext(ctx, createImageFileMetadataTable); err != nil {
		return fmt.Errorf("creating image metadata sidecar: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO fork_video_file_metadata (file_id, source_size, source_mod_time)
SELECT video_files.file_id, files.size, files.mod_time
FROM video_files JOIN files ON files.id = video_files.file_id`); err != nil {
		return fmt.Errorf("initializing video metadata sidecar: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO fork_image_file_metadata (file_id, source_size, source_mod_time)
SELECT image_files.file_id, files.size, files.mod_time
FROM image_files JOIN files ON files.id = image_files.file_id`); err != nil {
		return fmt.Errorf("initializing image metadata sidecar: %w", err)
	}

	columns := []legacyMetadataColumn{
		{table: "video_files", column: "video_stream_duration", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "frame_count", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "duration_mismatch", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "bit_depth", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "color_range", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "color_space", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "color_transfer", sidecar: "fork_video_file_metadata"},
		{table: "video_files", column: "color_primaries", sidecar: "fork_video_file_metadata"},
		{table: "image_files", column: "bit_depth", sidecar: "fork_image_file_metadata"},
		{table: "image_files", column: "color_range", sidecar: "fork_image_file_metadata"},
		{table: "image_files", column: "color_space", sidecar: "fork_image_file_metadata"},
		{table: "image_files", column: "color_transfer", sidecar: "fork_image_file_metadata"},
		{table: "image_files", column: "color_primaries", sidecar: "fork_image_file_metadata"},
	}

	for _, column := range columns {
		exists, err := forkColumnExists(ctx, db, column.table, column.column)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		query := fmt.Sprintf(`UPDATE %[1]s
SET %[2]s = (SELECT %[2]s FROM %[3]s WHERE %[3]s.file_id = %[1]s.file_id)`, column.sidecar, column.column, column.table)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("copying %s.%s: %w", column.table, column.column, err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", column.table, column.column)); err != nil {
			return fmt.Errorf("dropping %s.%s: %w", column.table, column.column, err)
		}
	}

	return nil
}

func reconcilePerformerAutoTagNames(ctx context.Context, db *sqlx.DB) error {
	if _, err := db.ExecContext(ctx, createPerformerAutoTagIgnoredNamesTable); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
DELETE FROM fork_performer_autotag_ignored_names
WHERE NOT EXISTS (
    SELECT 1 FROM performers
    WHERE performers.id = fork_performer_autotag_ignored_names.performer_id
  )
  OR NOT EXISTS (
    SELECT 1 FROM performers
    WHERE performers.id = fork_performer_autotag_ignored_names.performer_id
      AND performers.name = fork_performer_autotag_ignored_names.name
    UNION ALL
    SELECT 1 FROM performer_aliases
    WHERE performer_aliases.performer_id = fork_performer_autotag_ignored_names.performer_id
      AND performer_aliases.alias = fork_performer_autotag_ignored_names.name
  )`)
	return err
}

func reconcileSavedFilters(ctx context.Context, db *sqlx.DB) error {
	if _, err := db.ExecContext(ctx, createSavedFilterStateTable); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
DELETE FROM fork_saved_filter_state
WHERE NOT EXISTS (
  SELECT 1 FROM saved_filters
  WHERE saved_filters.id = fork_saved_filter_state.saved_filter_id
)`); err != nil {
		return fmt.Errorf("removing orphaned saved-filter sidecars: %w", err)
	}

	type savedFilterState struct {
		id           int
		objectFilter string
		filterAST    sql.NullString
		shadow       sql.NullString
		pending      sql.NullString
	}
	var changed []savedFilterState
	if err := func() error {
		rows, err := db.QueryxContext(ctx, `
SELECT saved_filters.id, COALESCE(saved_filters.object_filter, ''),
       fork_saved_filter_state.filter_ast, fork_saved_filter_state.legacy_object_filter,
       fork_saved_filter_state.pending_legacy_object_filter
FROM saved_filters
LEFT JOIN fork_saved_filter_state ON fork_saved_filter_state.saved_filter_id = saved_filters.id`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var state savedFilterState
			if err := rows.Scan(&state.id, &state.objectFilter, &state.filterAST, &state.shadow, &state.pending); err != nil {
				return err
			}
			if !state.shadow.Valid || state.shadow.String != state.objectFilter {
				changed = append(changed, state)
			}
		}
		return rows.Err()
	}(); err != nil {
		return err
	}

	for _, state := range changed {
		if state.filterAST.Valid && state.filterAST.String != "" {
			var existing models.FilterAST
			if err := json.Unmarshal([]byte(state.filterAST.String), &existing); err != nil {
				return fmt.Errorf("decoding saved-filter %d sidecar AST: %w", state.id, err)
			}
			if !existing.IsFlatRepresentable() {
				if !state.pending.Valid || state.pending.String != state.objectFilter {
					if _, err := db.ExecContext(ctx, "UPDATE fork_saved_filter_state SET pending_legacy_object_filter = ? WHERE saved_filter_id = ?", state.objectFilter, state.id); err != nil {
						return fmt.Errorf("preserving upstream saved-filter %d edit: %w", state.id, err)
					}
					logger.Warnf("saved filter %d: preserving complex v3 AST and retaining the conflicting upstream edit", state.id)
				}
				continue
			}
		}

		encodedAST := ""
		if state.objectFilter != "" {
			var legacy map[string]interface{}
			if err := json.Unmarshal([]byte(state.objectFilter), &legacy); err != nil {
				logger.Warnf("saved filter %d: upstream compatibility filter is invalid JSON: %v", state.id, err)
			} else {
				ast, err := models.FilterASTFromLegacySavedFilter(legacy)
				if err != nil {
					logger.Warnf("saved filter %d: cannot import upstream compatibility filter: %v", state.id, err)
				} else if ast != nil {
					encoded, err := json.Marshal(ast)
					if err != nil {
						return fmt.Errorf("encoding reconciled saved-filter %d AST: %w", state.id, err)
					}
					encodedAST = string(encoded)
				}
			}
		}

		if _, err := db.ExecContext(ctx, `
INSERT INTO fork_saved_filter_state (saved_filter_id, filter_ast, legacy_object_filter, pending_legacy_object_filter)
VALUES (?, ?, ?, NULL)
ON CONFLICT(saved_filter_id) DO UPDATE SET
  filter_ast = excluded.filter_ast,
  legacy_object_filter = excluded.legacy_object_filter,
  pending_legacy_object_filter = NULL`, state.id, encodedAST, state.objectFilter); err != nil {
			return fmt.Errorf("reconciling saved filter %d: %w", state.id, err)
		}
	}

	return nil
}

func reconcileFileMetadata(ctx context.Context, db *sqlx.DB) error {
	if _, err := db.ExecContext(ctx, createVideoFileMetadataTable); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, createImageFileMetadataTable); err != nil {
		return err
	}

	for _, pair := range []struct {
		sidecar string
		base    string
	}{
		{sidecar: "fork_video_file_metadata", base: "video_files"},
		{sidecar: "fork_image_file_metadata", base: "image_files"},
	} {
		query := fmt.Sprintf(`DELETE FROM %[1]s
WHERE NOT EXISTS (SELECT 1 FROM %[2]s WHERE %[2]s.file_id = %[1]s.file_id)
   OR NOT EXISTS (SELECT 1 FROM files WHERE files.id = %[1]s.file_id)
	OR EXISTS (
	  SELECT 1 FROM files
	  WHERE files.id = %[1]s.file_id
	    AND (files.size != %[1]s.source_size OR files.mod_time != %[1]s.source_mod_time)
	)`, pair.sidecar, pair.base)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("invalidating stale %s rows: %w", pair.sidecar, err)
		}
	}

	return nil
}

func init() {
	sqlite.RegisterForkMigration(5, "compatibility sidecars", fork005CompatibilitySidecars)
	sqlite.RegisterForkReconciler("file metadata", reconcileFileMetadata)
	sqlite.RegisterForkReconciler("performer auto-tag names", reconcilePerformerAutoTagNames)
	sqlite.RegisterForkReconciler("saved filters", reconcileSavedFilters)
}
