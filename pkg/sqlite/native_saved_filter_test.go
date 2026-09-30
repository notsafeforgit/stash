package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeSavedFilterPromotionValidatesBeforeReplacingState(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "filters.sqlite")
	buildLegacyDatabase(t, path, sqlite.NativeSchemaBaseline, false)
	raw := openRawDB(t, path)
	_, err := raw.Exec(`INSERT INTO saved_filters(id, name, mode, find_filter, object_filter, ui_options)
VALUES (10, 'Invalid input retained', 'SCENES', '', '{}', '');
INSERT INTO saved_filter_state(saved_filter_id, filter_ast, legacy_object_filter)
VALUES (10, '{"root":{}}', '{}');`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.ErrorContains(t, db.RunAllMigrations(), "saved filter 10 has an invalid canonical AST")
	raw = openRawDB(t, path)
	require.Equal(t, sqlite.NativeSchemaBaseline, queryUint(t, raw, "SELECT version FROM schema_migrations WHERE dirty = 0"))
	require.True(t, rawTableExists(t, raw, "saved_filter_state"))
	require.True(t, rawColumnExists(t, raw, "saved_filters", "object_filter"))
	require.False(t, rawTableExists(t, raw, "native_saved_filter_input"))
	// Repair only the malformed fixture, then resume from the unchanged input.
	want := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "organized", Value: true}}}
	encoded, err := json.Marshal(want)
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE saved_filter_state SET filter_ast = ? WHERE saved_filter_id = 10", string(encoded))
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Open(path))
	defer db.Close()
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		got, err := repo.SavedFilter.Find(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, want, got.FilterAST)
		require.Nil(t, got.ObjectFilter)
		return nil
	}))
}

func TestNativeSavedFilterWritesAreCanonicalAndRejectInvalidValues(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "filters.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	defer db.Close()
	repo := db.Repository()
	original := models.SavedFilter{
		Name: "Selected filter", Mode: models.FilterModeScenes,
		FilterAST: &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "organized", Value: true}}},
		UIOptions: map[string]interface{}{"display_mode": float64(1)},
	}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.SavedFilter.Create(ctx, &original)
	}))
	for _, invalid := range []models.SavedFilter{
		{FilterAST: &models.FilterAST{}},
		{UIOptions: map[string]interface{}{"bad": make(chan bool)}},
		{FilterAST: &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "organized", Value: make(chan bool)}}}},
	} {
		invalid.Name, invalid.Mode = "Rejected write", original.Mode
		require.Error(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.SavedFilter.Create(ctx, &invalid)
		}))
		invalid.ID = original.ID
		require.Error(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.SavedFilter.Update(ctx, &invalid)
		}))
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		all, err := repo.SavedFilter.All(ctx)
		require.NoError(t, err)
		require.Equal(t, []*models.SavedFilter{&original}, all)
		return nil
	}))
	// A clear survives a restart: there is no legacy projection to resurrect it.
	original.FilterAST = nil
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.SavedFilter.Update(ctx, &original)
	}))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		got, err := repo.SavedFilter.Find(ctx, original.ID)
		require.NoError(t, err)
		require.Equal(t, &original, got)
		return nil
	}))
}
