package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/jsonschema"
	"github.com/stashapp/stash/pkg/savedfilter"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeSavedFilterExportAndHistoricalImport(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "filters.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	restoredDB := sqlite.NewDatabase()
	require.NoError(t, restoredDB.Open(filepath.Join(t.TempDir(), "restored.sqlite")))
	t.Cleanup(func() { require.NoError(t, restoredDB.Close()) })
	restoredRepo := restoredDB.Repository()
	ctx := context.Background()
	const canonical = `{"root":{"group":{"operator":"OR","children":[
		{"condition":{"field":"title","value":{"modifier":"INCLUDES","value":"café"}}},
		{"group":{"operator":"AND","children":[
			{"condition":{"field":"title","value":{"modifier":"EXCLUDES","value":"other"}}},
			{"condition":{"field":"performers","value":{"modifier":"INCLUDES","value":[{"id":"42","label":"Name at save time"}]}}}
		]}}
	]}}}`
	for _, tt := range []struct{ name, criteria string }{
		{"canonical", `"filter_ast":` + canonical},
		{"legacy", `"object_filter":{"title":{"modifier":"INCLUDES","value":"café"},"performers":{"modifier":"INCLUDES","value":{"items":[{"id":"42","label":"Name at save time"}],"excluded":[{"id":"43","label":"Excluded name"}]}}}`},
		{"transitional", `"object_filter":{"__filter_ast":{"k":0,"o":1,"c":[{"k":1,"f":"title","m":6,"v":"café"},{"k":1,"f":"title","m":8,"v":"other"}]}}`},
		{"canonical_precedence", `"filter_ast":` + canonical + `,"object_filter":{"title":{"modifier":"INCLUDES","value":"outdated"}}`},
		{"empty", `"filter_ast":null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var input jsonschema.SavedFilter
			require.NoError(t, json.Unmarshal([]byte(`{"mode":"SCENES","name":"Imported filter","find_filter":{"q":"search","sort":"title","per_page":40},"ui_options":{"retained":"option"},`+tt.criteria+`}`), &input))
			input.Name += " " + tt.name
			importOne := func(repo models.Repository, input jsonschema.SavedFilter) *models.SavedFilter {
				t.Helper()
				importer := savedfilter.Importer{ReaderWriter: repo.SavedFilter, Input: input}
				require.NoError(t, importer.PreImport(ctx))
				var id *int
				require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
					var err error
					id, err = importer.Create(ctx)
					return err
				}))
				var stored *models.SavedFilter
				require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
					var err error
					stored, err = repo.SavedFilter.Find(ctx, *id)
					return err
				}))
				return stored
			}
			original := importOne(repo, input)
			if tt.name == "empty" {
				require.Nil(t, original.FilterAST)
			} else {
				require.NotNil(t, original.FilterAST)
				if tt.name == "canonical" || tt.name == "canonical_precedence" {
					encoded, err := json.Marshal(original.FilterAST)
					require.NoError(t, err)
					require.JSONEq(t, canonical, string(encoded))
				}
			}
			exported, err := savedfilter.ToJSON(ctx, original)
			require.NoError(t, err)
			encoded, err := json.Marshal(exported)
			require.NoError(t, err)
			var properties map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &properties))
			require.NotContains(t, properties, "object_filter")
			if tt.name == "legacy" {
				require.Contains(t, string(encoded), "Name at save time")
				require.Contains(t, string(encoded), "Excluded name")
				require.Contains(t, string(encoded), "EXCLUDES")
			}
			var restoredInput jsonschema.SavedFilter
			require.NoError(t, json.Unmarshal(encoded, &restoredInput))
			restored := importOne(restoredRepo, restoredInput)
			restored.ID = original.ID
			require.Equal(t, original, restored)
		})
	}

	for _, criteria := range []string{
		`"object_filter":{"__filter_ast":{"k":99}}`,
		`"filter_ast":{"root":{}}`,
	} {
		var input jsonschema.SavedFilter
		require.NoError(t, json.Unmarshal([]byte(`{"mode":"SCENES","name":"Invalid import",`+criteria+`}`), &input))
		importer := savedfilter.Importer{ReaderWriter: repo.SavedFilter, Input: input}
		err := importer.PreImport(ctx)
		if err == nil {
			err = repo.WithTxn(ctx, func(ctx context.Context) error {
				_, err := importer.Create(ctx)
				return err
			})
		}
		require.Error(t, err)
	}
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		all, err := repo.SavedFilter.All(ctx)
		require.NoError(t, err)
		require.Len(t, all, 5, "rejected imports must not leave partial records")
		return nil
	}))
}

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
