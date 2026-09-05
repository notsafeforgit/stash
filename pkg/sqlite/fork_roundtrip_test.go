package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestForkSavedFilterRollbackRoundTrip(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "roundtrip.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	flat := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{
		Field: "rating100", Value: map[string]interface{}{"value": float64(80), "modifier": "GREATER_THAN"},
	}}}
	complexAST := &models.FilterAST{Root: &models.FilterASTNode{Group: &models.FilterASTGroup{
		Operator: models.FilterGroupOperatorOr,
		Children: []*models.FilterASTNode{flat.Root, {Condition: &models.FilterASTCondition{Field: "organized", Value: map[string]interface{}{"value": true}}}},
	}}}
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		for name, ast := range map[string]*models.FilterAST{"Flat": flat, "Complex": complexAST, "Removed": flat} {
			if err := repo.SavedFilter.Create(ctx, &models.SavedFilter{Name: name, Mode: models.FilterModeScenes, FilterAST: ast}); err != nil {
				return err
			}
		}
		return nil
	}))
	upstreamVersion := db.Version()
	require.NoError(t, db.Close())

	// Simulate rollback to the mainline server, including an upstream delete.
	raw := openRawDB(t, path)
	fixture, err := os.ReadFile("testdata/v25_saved_filter_edits.sql")
	require.NoError(t, err)
	_, err = raw.Exec(string(fixture))
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	// Two opens cover roll-forward reconciliation and its idempotence.
	for range 2 {
		db = sqlite.NewDatabase()
		require.NoError(t, db.Open(path))
		require.Equal(t, upstreamVersion, db.Version())
		repo = db.Repository()
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
	var pending string
	require.NoError(t, raw.QueryRow(`SELECT pending_legacy_object_filter FROM fork_saved_filter_state JOIN saved_filters ON saved_filter_id = id WHERE name = 'Complex'`).Scan(&pending))
	require.JSONEq(t, `{"rating100":{"value":60,"modifier":"LESS_THAN"}}`, pending)
	require.Equal(t, uint(0), queryUint(t, raw, `SELECT COUNT(*) FROM fork_saved_filter_state WHERE saved_filter_id NOT IN (SELECT id FROM saved_filters)`))
}
