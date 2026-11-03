package sqlite_test

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceAccountStatusSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	prior, err := os.ReadFile("migrations/1000055_source_run_services.up.sql")
	require.NoError(t, err)
	definition := strings.Split(strings.Split(string(prior), "CREATE TABLE source_run_attempt_failures (")[1], "INSERT INTO native_migration_history")[0]
	_, err = raw.Exec(`CREATE TEMP TABLE retained_failures AS SELECT * FROM source_run_attempt_failures;
 DROP TABLE source_run_attempt_failures;
 CREATE TABLE source_run_attempt_failures (` + definition + `
 INSERT INTO source_run_attempt_failures SELECT * FROM retained_failures;
 DROP TABLE retained_failures;
 DELETE FROM native_migration_history WHERE version=1000138;`)
	require.NoError(t, err)
}

func TestSourceAccountStatusMigrationPreservesExistingFailuresAndRequests(t *testing.T) {
	f := deletedRedgifsFixture(t)
	run := f.claim(t, f.submit(t, f.request()))
	reportRedgifsDeletionRequest(t, f, run, "listing", true)
	_, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{
		State: "retry", ErrorCode: "not_found", ErrorScope: "service:redgifs"})
	require.NoError(t, err)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceAccountStatusSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000137,dirty=0")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"source_runs", "source_run_attempts", "source_run_attempt_failures", "source_run_requests"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(path), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.Open(path))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	_, err = raw.Exec("UPDATE source_run_attempt_failures SET error_code='account_deleted'")
	require.ErrorContains(t, err, "immutable")
}
