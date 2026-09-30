package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeImportPreservesLegacySavedFilterConflicts(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "roundtrip.sqlite")
	db := sqlite.NewDatabase()
	buildLegacyDatabase(t, path, 86, true)
	flat := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{
		Field: "rating100", Value: map[string]interface{}{"value": float64(80), "modifier": "GREATER_THAN"},
	}}}
	complexAST := &models.FilterAST{Root: &models.FilterASTNode{Group: &models.FilterASTGroup{
		Operator: models.FilterGroupOperatorOr,
		Children: []*models.FilterASTNode{flat.Root, {Condition: &models.FilterASTCondition{Field: "organized", Value: map[string]interface{}{"value": true}}}},
	}}}
	raw := openRawDB(t, path)
	for name, ast := range map[string]*models.FilterAST{"Flat": flat, "Complex": complexAST, "Removed": flat} {
		encodedAST, err := json.Marshal(ast)
		require.NoError(t, err)
		projection, _ := ast.FlatObjectFilter()
		encodedFlat, err := json.Marshal(projection)
		require.NoError(t, err)
		result, err := raw.Exec("INSERT INTO saved_filters(name, mode, object_filter) VALUES (?, 'SCENES', ?)", name, string(encodedFlat))
		require.NoError(t, err)
		id, err := result.LastInsertId()
		require.NoError(t, err)
		_, err = raw.Exec("INSERT INTO fork_saved_filter_state(saved_filter_id, filter_ast, legacy_object_filter) VALUES (?, ?, ?)", id, string(encodedAST), string(encodedFlat))
		require.NoError(t, err)
	}

	// The import input includes edits made by the old mainline client.
	fixture, err := os.ReadFile("testdata/v25_saved_filter_edits.sql")
	require.NoError(t, err)
	_, err = raw.Exec(string(fixture))
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())

	// Two opens verify the imported values persist without runtime reconciliation.
	for range 2 {
		db = sqlite.NewDatabase()
		require.NoError(t, db.Open(path))
		require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
		repo := db.Repository()
		require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
			filters, err := repo.SavedFilter.All(ctx)
			if err != nil {
				return err
			}
			require.Len(t, filters, 3)
			for _, filter := range filters {
				switch filter.Name {
				case "Flat edited in v2.5":
					projection, lossless := filter.FilterAST.FlatObjectFilter()
					require.True(t, lossless)
					require.Equal(t, float64(90), projection["rating100"].(map[string]interface{})["value"])
				case "Complex":
					require.Equal(t, complexAST, filter.FilterAST)
				case "Created in v2.5":
					projection, lossless := filter.FilterAST.FlatObjectFilter()
					require.True(t, lossless)
					require.Equal(t, map[string]interface{}{"organized": true}, projection)
				default:
					t.Fatalf("unexpected filter after rollback: %s", filter.Name)
				}
			}
			return nil
		}))
		require.NoError(t, db.Close())
	}
	raw = openRawDB(t, path)
	defer raw.Close()
	var evidence, selection string
	var reviewRequired bool
	require.NoError(t, raw.QueryRow(`SELECT evidence, selection, review_required
FROM saved_filter_import_conflicts JOIN saved_filters ON saved_filter_id = saved_filters.id
WHERE name = 'Complex'`).Scan(&evidence, &selection, &reviewRequired))
	var originals map[string]string
	require.NoError(t, json.Unmarshal([]byte(evidence), &originals))
	require.JSONEq(t, `{"rating100":{"value":60,"modifier":"LESS_THAN"}}`, originals["pending_legacy_object_filter"])
	require.Equal(t, "preserved-canonical", selection)
	require.True(t, reviewRequired)
	require.False(t, rawTableExists(t, raw, "saved_filter_state"))
	// Deleting a filter must not erase the migration's audit evidence.
	_, err = raw.Exec("DELETE FROM saved_filters WHERE name = 'Complex'")
	require.NoError(t, err)
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM saved_filter_import_conflicts WHERE saved_filter_id IS NULL"))
}
