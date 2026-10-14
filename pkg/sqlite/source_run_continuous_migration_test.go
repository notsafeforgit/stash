package sqlite_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeContinuousSourceSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	removeGalleryConsolidationSchema(t, db)
	var definition string
	require.NoError(t, db.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_runs'").Scan(&definition))
	const unrestricted = "CHECK(typeof(failures)='integer' AND failures>=0)"
	if !strings.Contains(definition, unrestricted) {
		return
	}
	rows, err := db.Query("SELECT sql FROM sqlite_schema WHERE tbl_name='source_runs' AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY rowid")
	require.NoError(t, err)
	defer rows.Close()
	var objects []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		objects = append(objects, s)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	definition = strings.Replace(definition, unrestricted, "CHECK(failures BETWEEN 0 AND 8)", 1)
	_, err = tx.Exec("PRAGMA defer_foreign_keys=ON; CREATE TABLE pre116_source_rows AS SELECT * FROM source_runs; DROP TABLE source_runs; " + definition + "; INSERT INTO source_runs SELECT * FROM pre116_source_rows; DROP TABLE pre116_source_rows;")
	require.NoError(t, err)
	for _, s := range objects {
		_, err = tx.Exec(s)
		require.NoError(t, err)
	}
	_, err = tx.Exec("DELETE FROM native_migration_history WHERE version=1000116")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestContinuousSourceMigrationPreservesAttemptsCheckpointsAndHolds(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.submit(t, f.request())
	for i := 0; i < 8; i++ {
		active := f.claim(t, r)
		require.NotNil(t, active)
		r = f.finish(t, active, "retry")
		f.now = r.AvailableAt
	}
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeContinuousSourceSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000115")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"source_runs", "source_run_attempts", "source_run_requests", "source_run_pacing"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(path), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	r = f.finish(t, f.claim(t, r), "retry")
	require.Equal(t, 9, r.Failures)
	require.Equal(t, "queued", r.State)
	// The immutable definition/transition guards still apply after rebuilding.
	_, err = raw.Exec("UPDATE source_runs SET policy_sha256=? WHERE uuid=?", strings.Repeat("f", 64), r.UUID)
	require.Error(t, err)
}
