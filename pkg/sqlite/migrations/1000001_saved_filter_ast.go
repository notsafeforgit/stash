package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
)

func prepareNativeSavedFilters(ctx context.Context, db *sqlx.DB) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Only staging data is committed here. The primary SQL migration promotes
	// it and drops the old representation in one transaction.
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS native_saved_filter_input (
  saved_filter_id INTEGER PRIMARY KEY,
  filter_ast TEXT NOT NULL,
  conflict_json TEXT
); DELETE FROM native_saved_filter_input`); err != nil {
		return err
	}
	var rows []struct {
		ID      int            `db:"id"`
		Legacy  string         `db:"object_filter"`
		AST     sql.NullString `db:"filter_ast"`
		Shadow  sql.NullString `db:"legacy_object_filter"`
		Pending sql.NullString `db:"pending_legacy_object_filter"`
	}
	if err := tx.SelectContext(ctx, &rows, `SELECT saved_filters.id, saved_filters.object_filter,
state.filter_ast, state.legacy_object_filter, state.pending_legacy_object_filter
FROM saved_filters LEFT JOIN saved_filter_state AS state ON state.saved_filter_id = saved_filters.id
ORDER BY saved_filters.id`); err != nil {
		return err
	}
	for _, row := range rows {
		var ast *models.FilterAST
		if row.AST.Valid && row.AST.String != "" {
			if err := json.Unmarshal([]byte(row.AST.String), &ast); err != nil {
				return fmt.Errorf("saved filter %d has invalid canonical JSON: %w", row.ID, err)
			}
		} else if row.Legacy != "" {
			var legacy map[string]interface{}
			if err := json.Unmarshal([]byte(row.Legacy), &legacy); err != nil {
				return fmt.Errorf("saved filter %d has invalid legacy JSON: %w", row.ID, err)
			}
			ast, err = models.FilterASTFromLegacySavedFilter(legacy)
			if err != nil {
				return fmt.Errorf("converting saved filter %d: %w", row.ID, err)
			}
		}
		encoded := ""
		if ast != nil {
			normalized, err := ast.Normalize()
			if err != nil {
				return fmt.Errorf("saved filter %d has an invalid canonical AST: %w", row.ID, err)
			}
			value, err := json.Marshal(normalized)
			if err != nil {
				return fmt.Errorf("encoding saved filter %d: %w", row.ID, err)
			}
			encoded = string(value)
		}
		var conflict *string
		if row.Pending.Valid {
			// Preserve exact input strings, including an invalid legacy alternative.
			// The selected canonical AST stays active and the alternative needs review.
			value, err := json.Marshal(map[string]interface{}{
				"canonical_filter_ast":         row.AST.String,
				"object_filter":                row.Legacy,
				"previous_legacy_projection":   row.Shadow.String,
				"pending_legacy_object_filter": row.Pending.String,
			})
			if err != nil {
				return err
			}
			text := string(value)
			conflict = &text
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO native_saved_filter_input VALUES (?, ?, ?)", row.ID, encoded, conflict); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func init() {
	sqlite.RegisterPreMigration(1000001, prepareNativeSavedFilters)
}
