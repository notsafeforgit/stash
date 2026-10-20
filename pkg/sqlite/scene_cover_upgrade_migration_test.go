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

func removeCoverUpgradeSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER IF EXISTS scene_cover_upgrade_artwork;
DROP TRIGGER IF EXISTS scene_cover_upgrade_selected;
DROP TRIGGER IF EXISTS scene_cover_upgrade_reselected;
DROP TABLE IF EXISTS scene_cover_claims;
DROP TABLE IF EXISTS scene_cover_upgrade;
DELETE FROM native_migration_history WHERE version=1000125`)
	require.NoError(t, err)
}

func TestCoverSelectionMigrationFreezesOnlyUnrecordedCohort(t *testing.T) {
	db, r := archiveTestDatabase(t)
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	var ids []int
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		for i := range 3 {
			scene := models.NewScene()
			require.NoError(t, r.Scene.Create(ctx, &scene, nil))
			ids = append(ids, scene.ID)
			if i == 2 {
				continue
			}
			require.NoError(t, r.Scene.UpdateCover(ctx, scene.ID, []byte("cover")))
			saved, err := r.Scene.Find(ctx, scene.ID)
			require.NoError(t, err)
			if i == 1 {
				require.NoError(t, r.Scene.SetCoverSource(ctx, scene.ID, &models.SceneCoverSource{
					CoverChecksum: saved.CoverChecksum, FileID: 42, At: 7, Fingerprint: models.SceneCoverFingerprint{Version: 1}}))
			}
		}
		return nil
	}))
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "scene_cover_sources", "files", "archive_entities", "library_jobs", "file_moves"} {
		before[table] = albumJobRows(t, raw, table)
	}
	removeCoverUpgradeSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000124,dirty=0")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
	var cohortID int
	require.NoError(t, raw.QueryRow("SELECT scene_id FROM scene_cover_upgrade").Scan(&cohortID))
	require.Equal(t, ids[0], cohortID)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM scene_cover_upgrade"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM scene_cover_claims"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM scene_cover_upgrade"))
	require.NoError(t, db.Close())
	_, err = raw.Exec("DROP TABLE scene_cover_claims")
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(path), "missing scene_cover_claims")
}
