package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeNamesMigrationPreservesSpellingsAndPolicies(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "names.sqlite")
	buildLegacyDatabase(t, path, sqlite.NativeSchemaBaseline+1, false)
	raw := openRawDB(t, path)
	_, err := raw.Exec(`INSERT INTO performers(id, name, ignore_auto_tag, created_at, updated_at) VALUES
(1, 'First', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(2, '', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO performer_aliases VALUES (1, 'Alias'), (1, 'ALIAS'), (1, 'First');
INSERT INTO performer_autotag_ignored_names VALUES (1, 'alias'), (1, 'unmatched historical policy');`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	raw = openRawDB(t, path)
	defer raw.Close()
	require.False(t, rawTableExists(t, raw, "performer_aliases"))
	require.False(t, rawTableExists(t, raw, "performer_autotag_ignored_names"))
	require.False(t, rawColumnExists(t, raw, "performers", "name"))
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM performer_names"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM performer_names WHERE performer_id = 1 AND ignore_auto_tag = 1 AND is_primary = 0"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performer_names WHERE performer_id = 2 AND name = '' AND position = 0"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers WHERE id = 1 AND ignore_auto_tag = 1"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT json_array_length(details, '$.duplicate_primary_aliases') FROM native_migration_history WHERE version = 1000002"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT json_array_length(details, '$.unmatched_name_policies') FROM native_migration_history WHERE version = 1000002"))
	_, err = raw.Exec("INSERT INTO performer_names(performer_id, name, position) VALUES (1, 'Second primary', 0)")
	require.ErrorContains(t, err, "UNIQUE constraint")
	_, err = raw.Exec("INSERT INTO performer_names(performer_id, name, position) VALUES (999, 'Missing owner', 0)")
	require.ErrorContains(t, err, "FOREIGN KEY constraint")
}

func TestNativeNameSelectionRetainsPolicyAndDoesNotRequireGlobalUniqueness(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "names.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	defer db.Close()
	repo := db.Repository()
	first := models.NewPerformer()
	first.Name = "Shared name"
	first.Aliases = models.NewRelatedPerformerAliases([]models.PerformerAlias{
		{Alias: "Zebra", IgnoreAutoTag: true}, {Alias: "Alpha"},
	})
	second := models.NewPerformer()
	second.Name = first.Name
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		for _, p := range []*models.Performer{&first, &second} {
			if err := performer.ValidateCreate(*p); err != nil {
				return err
			}
			if err := repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: p}); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NotEqual(t, first.ID, second.ID)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		partial := models.PerformerPartial{Name: models.NewOptionalString("Zebra")}
		require.NoError(t, performer.ValidateUpdate(ctx, first.ID, partial, repo.Performer))
		p, err := repo.Performer.UpdatePartial(ctx, first.ID, partial)
		require.NoError(t, err)
		require.True(t, p.IgnorePrimaryNameAutoTag)
		require.NoError(t, p.LoadAliases(ctx, repo.Performer))
		require.Equal(t, []models.PerformerAlias{{Alias: "Shared name"}, {Alias: "Alpha"}}, p.Aliases.List())
		return nil
	}))
	// An invalid replacement must not erase the current set or its policies.
	require.Error(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Performer.UpdatePartial(ctx, first.ID, models.PerformerPartial{Aliases: &models.UpdatePerformerAliases{
			Mode: models.RelationshipUpdateModeSet, Values: []models.PerformerAlias{{Alias: "duplicate"}, {Alias: "duplicate"}},
		}})
		return err
	}))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		p, err := repo.Performer.Find(ctx, first.ID)
		require.NoError(t, err)
		require.Equal(t, "Zebra", p.Name)
		require.True(t, p.IgnorePrimaryNameAutoTag)
		require.NoError(t, p.LoadAliases(ctx, repo.Performer))
		require.Equal(t, []models.PerformerAlias{{Alias: "Shared name"}, {Alias: "Alpha"}}, p.Aliases.List())
		return nil
	}))
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.Performer.Destroy(ctx, first.ID)
	}))
	raw := openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performer_names"))
}
