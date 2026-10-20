package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeLibraryJobSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeFileMoveSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE IF EXISTS library_jobs;
DELETE FROM native_migration_history WHERE version=1000123`)
	require.NoError(t, err)
}

func TestLibraryJobMigrationPreservesLibraryAndRequiresCollisionRecovery(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "performers", "archive_entities", "archive_jobs", "source_accounts", "shares", "share_sessions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	removeLibraryJobSchema(t, raw)
	_, err := raw.Exec(`UPDATE schema_migrations SET version=1000122,dirty=0;
CREATE TABLE library_jobs(original TEXT);
INSERT INTO library_jobs VALUES('retained')`)
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.ErrorContains(t, db.RunAllMigrations(), "library_jobs already exists")
	var original string
	require.NoError(t, raw.QueryRow("SELECT original FROM library_jobs").Scan(&original))
	require.Equal(t, "retained", original)
	require.EqualValues(t, 1000123, queryUint(t, raw, "SELECT version FROM schema_migrations"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000123"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name IN ('library_jobs_recent','library_jobs_pending')"))
	require.ErrorContains(t, db.Open(path), "incomplete")
	require.ErrorContains(t, db.RunAllMigrations(), "incomplete")
	// Simulate explicit operator recovery in this fixture, after proving the
	// migration wrote no objects or history. Ordinary retry must never clear a
	// dirty ledger or discard the colliding table automatically.
	_, err = raw.Exec(`ALTER TABLE library_jobs RENAME TO retained_foreign_jobs;
UPDATE schema_migrations SET version=1000122,dirty=0`)
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM library_jobs"))
	require.NoError(t, raw.QueryRow("SELECT original FROM retained_foreign_jobs").Scan(&original))
	require.Equal(t, "retained", original)
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
}
