package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeDurableBulkSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceBackoffSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE library_bulk_targets;
DROP TABLE library_bulk_jobs;
ALTER TABLE library_jobs RENAME TO library_jobs_with_bulk;
DROP INDEX library_jobs_recent;
DROP INDEX library_jobs_pending;
DELETE FROM native_migration_history WHERE version=1000126`)
	require.NoError(t, err)
	original, err := os.ReadFile("migrations/1000123_library_jobs.up.sql")
	require.NoError(t, err)
	schema, _, _ := strings.Cut(string(original), "INSERT INTO native_migration_history")
	_, err = raw.Exec(schema + `
INSERT INTO library_jobs(id,status,description,progress,added_at,started_at,ended_at,error)
SELECT id,status,description,progress,added_at,started_at,ended_at,error FROM library_jobs_with_bulk;
DROP TABLE library_jobs_with_bulk`)
	require.NoError(t, err)
}

func TestDurableBulkMigrationPreservesExistingJobs(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeDurableBulkSchema(t, raw)
	_, err := raw.Exec(`UPDATE schema_migrations SET version=1000125,dirty=0;
INSERT INTO library_jobs(id,status,description,progress,added_at) VALUES
('ea677988-3fce-4e61-a9ee-563d354707f3','READY','previous process',-1,42)`)
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "archive_entities", "file_moves", "scene_cover_upgrade", "scene_cover_claims"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT generation FROM library_jobs"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM library_jobs WHERE status='READY' AND description='previous process' AND added_at=42"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM library_bulk_jobs"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	_, err = raw.Exec("DROP INDEX library_bulk_targets_pending")
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(path), "library_bulk_targets_pending")
}
