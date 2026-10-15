package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceGalleryPerformerSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query("SELECT name FROM sqlite_schema WHERE type='trigger' AND name LIKE 'source_gallery_performers_%'")
	require.NoError(t, err)
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	for _, name := range names {
		_, err := db.Exec("DROP TRIGGER " + name)
		require.NoError(t, err)
	}
	_, err = db.Exec("DROP TABLE IF EXISTS source_gallery_performer_pending; DELETE FROM native_migration_history WHERE version=1000118")
	require.NoError(t, err)
}

func TestSourceGalleryPerformersMigrationPreservesExistingChoices(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	g := consolidationGallery(t, repo, "Reviewed album", nil, []int{31})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Gallery.UpdatePartial(ctx, *g.LocalID, models.GalleryPartial{PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{}}})
		return err
	}))
	before := metadataState(t, repo, g.UUID, "performers")
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceGalleryPerformerSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000117")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, before, metadataState(t, db.Repository(), g.UUID, "performers"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_performer_pending"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
