package manager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/require"
)

func defaultFilterTestManager(t *testing.T, source string) *Manager {
	t.Helper()
	c := config.InitializeEmpty()
	dir := t.TempDir()
	c.SetConfigFile(filepath.Join(dir, "config.yml"))
	ui := map[string]interface{}{"theme": "dark"}
	if source != "" {
		require.NoError(t, json.Unmarshal([]byte(source), &ui))
	}
	c.SetUIConfiguration(ui)
	require.NoError(t, c.Write())
	before, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(dir, "native.sqlite")))
	after, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	require.Equal(t, before, after, "creating a database must not rewrite modern configuration with historical migrations")
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return &Manager{Config: c, Database: db, Repository: db.Repository()}
}

const importedFlatDefault = `{"theme":"dark","defaultFilters":{"scenes":{"mode":"SCENES","object_filter":{"organized":true},"find_filter":{"sort":"date"},"ui_options":{"display_mode":2}}}}`

const importedConflictingDefault = `{
"defaultFilters":{"scenes":{"mode":"SCENES","object_filter":{"organized":true},"ui_options":{"display_mode":2}}},
"forkDefaultFilterState":{"scenes":{
  "filter_ast":{"root":{"group":{"operator":"OR","children":[
    {"condition":{"field":"favorite","value":{"value":true}}},
    {"condition":{"field":"rating100","value":{"value":80,"modifier":"GREATER_THAN"}}}
  ]}}},
  "legacy_object_filter":{"favorite":true}
}}}`

func TestDefaultFilterImportResumesAfterFilePublicationFailure(t *testing.T) {
	ctx := context.Background()
	m := defaultFilterTestManager(t, importedFlatDefault)
	original := m.Config.GetUIConfiguration()
	path := filepath.Join(t.TempDir(), "not-created", "config.yml")
	m.Config.SetConfigFile(path)
	require.Error(t, m.PromoteDefaultFilterConfig(ctx))
	require.Equal(t, original, m.Config.GetUIConfiguration())
	require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		checkpoint, err := m.Repository.ConfigurationMigration.Find(ctx, defaultFilterMigrationName)
		require.NoError(t, err)
		require.Equal(t, "prepared", checkpoint.State)
		require.NotContains(t, checkpoint.SourceJSON, "theme")
		filter, err := m.Repository.DefaultFilter.Find(ctx, "scenes")
		require.NoError(t, err)
		require.Equal(t, 1, filter.Revision)
		return nil
	}))
	require.NoError(t, os.Mkdir(filepath.Dir(path), 0700))
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	require.Equal(t, map[string]interface{}{"theme": "dark"}, m.Config.GetUIConfiguration())
	// Restart with the same database and retry: native edits are not reimported.
	_, err := m.ConfigureDefaultFilter(ctx, "scenes", "CLEAR", nil, nil)
	require.NoError(t, err)
	dbPath := m.Database.DatabasePath()
	require.NoError(t, m.Database.Close())
	require.NoError(t, m.Database.Open(dbPath))
	m.Repository = m.Database.Repository()
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	ui, err := m.UIConfiguration(ctx)
	require.NoError(t, err)
	require.NotContains(t, ui, "defaultFilters")
	require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		checkpoint, err := m.Repository.ConfigurationMigration.Find(ctx, defaultFilterMigrationName)
		require.NoError(t, err)
		require.Equal(t, "published", checkpoint.State)
		filter, err := m.Repository.DefaultFilter.Find(ctx, "scenes")
		require.NoError(t, err)
		require.Equal(t, 2, filter.Revision)
		require.False(t, filter.Enabled)
		return nil
	}))
}

func TestDefaultFilterImportResumesAfterConfigWasPublished(t *testing.T) {
	ctx := context.Background()
	m := defaultFilterTestManager(t, importedFlatDefault)
	require.NoError(t, m.Repository.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := m.Database.ExecSQL(ctx, `CREATE TRIGGER fail_config_checkpoint BEFORE UPDATE ON configuration_migrations
BEGIN SELECT RAISE(ABORT, 'injected publication checkpoint failure'); END`, nil)
		return err
	}))
	require.ErrorContains(t, m.PromoteDefaultFilterConfig(ctx), "injected publication checkpoint failure")
	require.NotContains(t, m.Config.GetUIConfiguration(), "defaultFilters")
	require.NoError(t, m.Repository.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := m.Database.ExecSQL(ctx, "DROP TRIGGER fail_config_checkpoint", nil)
		return err
	}))
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		filter, err := m.Repository.DefaultFilter.Find(ctx, "scenes")
		require.NoError(t, err)
		require.Equal(t, 1, filter.Revision)
		return nil
	}))
}

func TestDefaultFilterImportRefusesChangedConfigurationAndInvalidAST(t *testing.T) {
	ctx := context.Background()
	t.Run("changed source", func(t *testing.T) {
		m := defaultFilterTestManager(t, importedFlatDefault)
		m.Config.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.yml"))
		require.Error(t, m.PromoteDefaultFilterConfig(ctx))
		ui := m.Config.GetUIConfiguration()
		ui["defaultFilters"].(map[string]interface{})["scenes"] = map[string]interface{}{"mode": "SCENES", "object_filter": map[string]interface{}{"favorite": true}}
		m.Config.SetUIConfiguration(ui)
		require.ErrorContains(t, m.PromoteDefaultFilterConfig(ctx), "changed after staging")
		require.Equal(t, ui, m.Config.GetUIConfiguration())
	})
	t.Run("invalid canonical input", func(t *testing.T) {
		m := defaultFilterTestManager(t, `{"defaultFilters":{"scenes":{"mode":"SCENES","filter_ast":{"root":{}}}}}`)
		before, err := os.ReadFile(m.Config.GetConfigFile())
		require.NoError(t, err)
		require.ErrorContains(t, m.PromoteDefaultFilterConfig(ctx), "exactly one")
		after, err := os.ReadFile(m.Config.GetConfigFile())
		require.NoError(t, err)
		require.Equal(t, before, after)
		require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
			checkpoint, err := m.Repository.ConfigurationMigration.Find(ctx, defaultFilterMigrationName)
			require.NoError(t, err)
			require.Nil(t, checkpoint)
			filters, err := m.Repository.DefaultFilter.All(ctx)
			require.NoError(t, err)
			require.Empty(t, filters)
			return nil
		}))
	})
}

func TestHistoricalDefaultFilterConversion(t *testing.T) {
	defaults, conflicts, err := importDefaultFilterConfig(importedConflictingDefault)
	require.NoError(t, err)
	require.Equal(t, models.FilterGroupOperatorOr, defaults["scenes"].FilterAST.Root.Group.Operator)
	require.Nil(t, defaults["scenes"].ObjectFilter)
	require.Len(t, conflicts, 1)
	projection, valid := conflicts[0].Alternative.FlatObjectFilter()
	require.True(t, valid)
	require.Equal(t, map[string]interface{}{"organized": true}, projection)

	flat := `{"defaultFilters":{"scenes":{"mode":"SCENES","object_filter":{"organized":true}}},"forkDefaultFilterState":{"scenes":{"filter_ast":{"root":{"condition":{"field":"favorite","value":{"value":true}}}},"legacy_object_filter":{"favorite":true}}}}`
	defaults, conflicts, err = importDefaultFilterConfig(flat)
	require.NoError(t, err)
	require.Empty(t, conflicts)
	projection, valid = defaults["scenes"].FilterAST.FlatObjectFilter()
	require.True(t, valid)
	require.Equal(t, map[string]interface{}{"organized": true}, projection)
	defaults, conflicts, err = importDefaultFilterConfig(`{"defaultFilters":{"scenes":null},"forkDefaultFilterState":{"scenes":{"filter_ast":{}}}}`)
	require.NoError(t, err)
	require.Empty(t, defaults)
	require.Empty(t, conflicts)
}

func TestDefaultFilterImportNormalizesHistoricalPagination(t *testing.T) {
	source := `{"defaultFilters":{"galleries":{"mode":"GALLERIES","find_filter":{"page":"1","per_page":"120","sort":"date","q":"retained query"},"ui_options":{"display_mode":2}}}}`
	m := defaultFilterTestManager(t, source)
	ctx := context.Background()
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		filter, err := m.Repository.DefaultFilter.Find(ctx, "galleries")
		require.NoError(t, err)
		require.Equal(t, 1, *filter.Filter.FindFilter.Page)
		require.Equal(t, 120, *filter.Filter.FindFilter.PerPage)
		require.Equal(t, "retained query", *filter.Filter.FindFilter.Q)
		checkpoint, err := m.Repository.ConfigurationMigration.Find(ctx, defaultFilterMigrationName)
		require.NoError(t, err)
		require.JSONEq(t, source, checkpoint.SourceJSON)
		return nil
	}))
	_, _, err := importDefaultFilterConfig(`{"defaultFilters":{"scenes":{"mode":"SCENES","find_filter":{"per_page":"not a number"}}}}`)
	require.ErrorContains(t, err, "find_filter.per_page must be an integer")
}
