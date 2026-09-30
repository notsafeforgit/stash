package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

type DefaultFilterStore struct{}

type defaultFilterRow struct {
	View       string            `db:"view"`
	Revision   int               `db:"revision"`
	Enabled    bool              `db:"enabled"`
	Mode       models.FilterMode `db:"mode"`
	FindFilter string            `db:"find_filter"`
	FilterAST  string            `db:"filter_ast"`
	UIOptions  string            `db:"ui_options"`
}

func (r defaultFilterRow) resolve() (*models.DefaultFilter, error) {
	f, err := (&savedFilterRow{Mode: r.Mode, FindFilter: r.FindFilter, FilterAST: r.FilterAST, UIOptions: r.UIOptions}).resolve()
	if err != nil {
		return nil, fmt.Errorf("reading default filter %q: %w", r.View, err)
	}
	return &models.DefaultFilter{View: r.View, Revision: r.Revision, Enabled: r.Enabled, Filter: *f}, nil
}

func (s *DefaultFilterStore) All(ctx context.Context) ([]*models.DefaultFilter, error) {
	var rows []defaultFilterRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM default_filters ORDER BY view"); err != nil {
		return nil, err
	}
	ret := make([]*models.DefaultFilter, 0, len(rows))
	for _, row := range rows {
		value, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret = append(ret, value)
	}
	return ret, nil
}

func (s *DefaultFilterStore) Find(ctx context.Context, view string) (*models.DefaultFilter, error) {
	var row defaultFilterRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM default_filters WHERE view = ?", view); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve()
}

func (s *DefaultFilterStore) Set(ctx context.Context, view string, filter *models.SavedFilter) error {
	if filter == nil || !filter.Mode.IsValid() {
		return errors.New("default filter requires a valid mode")
	}
	var row savedFilterRow
	if err := row.fromSavedFilter(*filter); err != nil {
		return err
	}
	_, err := dbWrapper.Exec(ctx, `INSERT INTO default_filters(view, mode, find_filter, filter_ast, ui_options)
VALUES (?, ?, ?, ?, ?) ON CONFLICT(view) DO UPDATE SET
revision = default_filters.revision + 1, enabled = 1, mode = excluded.mode,
find_filter = excluded.find_filter, filter_ast = excluded.filter_ast, ui_options = excluded.ui_options`,
		view, row.Mode, row.FindFilter, row.FilterAST, row.UIOptions)
	return err
}

func (s *DefaultFilterStore) Clear(ctx context.Context, view string) error {
	// Retain the revision even when disabled so stale review actions cannot
	// accidentally apply to a later default created for this view.
	_, err := dbWrapper.Exec(ctx, `INSERT INTO default_filters(view, enabled, mode) VALUES (?, 0, '')
ON CONFLICT(view) DO UPDATE SET revision = default_filters.revision + 1, enabled = 0,
mode = '', find_filter = '', filter_ast = '', ui_options = ''`, view)
	return err
}

func (s *DefaultFilterStore) Conflicts(ctx context.Context) ([]*models.DefaultFilterConflict, error) {
	var rows []struct {
		View          string `db:"view"`
		Revision      int    `db:"revision"`
		MigrationName string `db:"migration_name"`
		Evidence      string `db:"evidence"`
		Alternative   string `db:"alternative_ast"`
		ImportError   string `db:"import_error"`
		Selection     string `db:"selection"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT c.view, d.revision, c.migration_name, c.evidence,
c.alternative_ast, c.import_error, c.selection FROM default_filter_import_conflicts c
JOIN default_filters d ON d.view = c.view WHERE c.selection = 'pending' ORDER BY c.view`); err != nil {
		return nil, err
	}
	ret := make([]*models.DefaultFilterConflict, 0, len(rows))
	for _, row := range rows {
		c := &models.DefaultFilterConflict{View: row.View, Revision: row.Revision, MigrationName: row.MigrationName, Evidence: row.Evidence, ImportError: row.ImportError, Selection: row.Selection}
		if err := decodeJSON(row.Alternative, &c.Alternative); err != nil {
			return nil, err
		}
		if c.Alternative != nil {
			if err := c.Alternative.Validate(); err != nil {
				return nil, err
			}
		}
		ret = append(ret, c)
	}
	return ret, nil
}

func (s *DefaultFilterStore) CreateConflict(ctx context.Context, conflict *models.DefaultFilterConflict) error {
	var value string
	if conflict.Alternative != nil {
		ast, err := conflict.Alternative.Normalize()
		if err != nil {
			return err
		}
		value, err = encodeJSONOrEmpty(ast)
		if err != nil {
			return err
		}
	}
	_, err := dbWrapper.Exec(ctx, `INSERT INTO default_filter_import_conflicts
(view, migration_name, evidence, alternative_ast, import_error) VALUES (?, ?, ?, ?, ?)`,
		conflict.View, conflict.MigrationName, conflict.Evidence, value, conflict.ImportError)
	return err
}

func (s *DefaultFilterStore) ResolveConflict(ctx context.Context, view, selection string) error {
	_, err := dbWrapper.Exec(ctx, `UPDATE default_filter_import_conflicts SET selection = ?, resolved_at = CURRENT_TIMESTAMP
WHERE view = ? AND selection = 'pending'`, selection, view)
	return err
}

type ConfigurationMigrationStore struct{}

func (s *ConfigurationMigrationStore) Find(ctx context.Context, name string) (*models.ConfigurationMigration, error) {
	var row models.ConfigurationMigration
	if err := dbWrapper.Get(ctx, &row, "SELECT name, source_json, target_json, state FROM configuration_migrations WHERE name = ?", name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *ConfigurationMigrationStore) Create(ctx context.Context, migration *models.ConfigurationMigration) error {
	_, err := dbWrapper.Exec(ctx, "INSERT INTO configuration_migrations(name, source_json, target_json) VALUES (?, ?, ?)",
		migration.Name, migration.SourceJSON, migration.TargetJSON)
	return err
}

func (s *ConfigurationMigrationStore) Publish(ctx context.Context, name string) error {
	_, err := dbWrapper.Exec(ctx, "UPDATE configuration_migrations SET state = 'published', published_at = CURRENT_TIMESTAMP WHERE name = ? AND state = 'prepared'", name)
	return err
}
