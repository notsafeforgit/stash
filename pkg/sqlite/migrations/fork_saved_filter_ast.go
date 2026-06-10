package migrations

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
)

type forkSavedFilterASTMigrator struct {
	migrator
}

func fork002SavedFilterAST(ctx context.Context, db *sqlx.DB) error {
	exists, err := forkColumnExists(ctx, db, "saved_filters", "filter_ast")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.ExecContext(ctx, "ALTER TABLE `saved_filters` ADD COLUMN `filter_ast` blob not null default ''"); err != nil {
			return fmt.Errorf("adding saved_filters.filter_ast: %w", err)
		}
	}

	m := forkSavedFilterASTMigrator{
		migrator: migrator{
			db: db,
		},
	}

	return m.migrate(ctx)
}

// migrate converts each saved filter's legacy object_filter criteria — either
// the pure v2.5 flat map or the transitional v3 form with a compact AST under
// __filter_ast — into the canonical filter_ast column. Rows that fail
// conversion are logged and left untouched: they keep working through the
// object_filter fallback in the API layer.
func (m *forkSavedFilterASTMigrator) migrate(ctx context.Context) error {
	return m.withTxn(ctx, func(tx *sqlx.Tx) error {
		rows, err := tx.Query("SELECT `id`, `name`, `object_filter` FROM `saved_filters` WHERE `object_filter` != '' AND `filter_ast` = ''")
		if err != nil {
			return err
		}
		defer rows.Close()

		type conversion struct {
			id  int
			ast string
		}
		var conversions []conversion

		for rows.Next() {
			var (
				id           int
				name         string
				objectFilter string
			)
			if err := rows.Scan(&id, &name, &objectFilter); err != nil {
				return err
			}

			var legacy map[string]interface{}
			if err := json.Unmarshal([]byte(objectFilter), &legacy); err != nil {
				logger.Warnf("saved filter %d (%q): leaving unconverted, invalid object_filter JSON: %v", id, name, err)
				continue
			}
			if len(legacy) == 0 {
				continue
			}

			ast, err := models.FilterASTFromLegacySavedFilter(legacy)
			if err != nil {
				logger.Warnf("saved filter %d (%q): leaving unconverted: %v", id, name, err)
				continue
			}
			if ast == nil {
				continue
			}

			encoded, err := json.Marshal(ast)
			if err != nil {
				return fmt.Errorf("encoding filter AST for saved filter %d: %w", id, err)
			}

			conversions = append(conversions, conversion{id: id, ast: string(encoded)})
		}
		if err := rows.Err(); err != nil {
			return err
		}

		for _, c := range conversions {
			if _, err := tx.Exec("UPDATE `saved_filters` SET `filter_ast` = ?, `object_filter` = '' WHERE `id` = ?", c.ast, c.id); err != nil {
				return fmt.Errorf("updating saved filter %d: %w", c.id, err)
			}
		}

		if len(conversions) > 0 {
			logger.Infof("converted %d saved filter(s) to the filter AST format", len(conversions))
		}

		return nil
	})
}

func init() {
	sqlite.RegisterLegacyForkMigration(2, "saved filter filter_ast", 999, fork002SavedFilterAST)
}
