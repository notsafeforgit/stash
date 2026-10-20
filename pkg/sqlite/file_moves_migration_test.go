package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeFileMoveSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeCoverUpgradeSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE IF EXISTS file_moves;
DELETE FROM native_migration_history WHERE version=1000124`)
	require.NoError(t, err)
}

func TestFileMoveMigrationPreservesLibrary(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"files", "folders", "scenes", "images", "archive_entities", "library_jobs", "file_deletions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	removeFileMoveSchema(t, raw)
	_, err := raw.Exec(`UPDATE schema_migrations SET version=1000123,dirty=0`)
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_moves"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, db.Close())
	_, err = raw.Exec("DROP TABLE file_moves")
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(path), "missing file_moves")
}
