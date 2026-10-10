package sqlite_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func removeSourceSiteConcurrencySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP INDEX source_runs_running_destination;
 CREATE UNIQUE INDEX source_runs_running_destination ON source_runs(destination) WHERE state='running' AND destination!='';
 DELETE FROM native_migration_history WHERE version=1000110;`)
	require.NoError(t, err)
}

func TestSourceSiteConcurrencyMigrationPreservesQueuedAndRunningWork(t *testing.T) {
	f := newSourceRunFixture(t)
	running := f.claim(t, f.submit(t, f.request()))
	request := f.request()
	request.PolicySHA256 = strings.Repeat("b", 64)
	queued := f.submit(t, request)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	removeSourceSiteConcurrencySchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000109")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Error(t, f.db.Open(path))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	require.Equal(t, running, f.find(t, running.UUID))
	require.Equal(t, queued, f.find(t, queued.UUID))
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, `SELECT "unique" FROM pragma_index_list('source_runs') WHERE name='source_runs_running_destination'`))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
