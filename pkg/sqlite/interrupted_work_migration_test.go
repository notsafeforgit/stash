package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Historical migration fixtures must have the actual pre-107 job shape.
func removeInterruptedWorkSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeGalleryCoverSchema(t, raw)
	var definition string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='archive_jobs'").Scan(&definition))
	if !strings.Contains(definition, "failures INTEGER") {
		return
	}
	rows, err := raw.Query("SELECT sql FROM sqlite_schema WHERE tbl_name='archive_jobs' AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY name")
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
	definition = strings.ReplaceAll(definition, " failures INTEGER NOT NULL DEFAULT 0 CHECK(typeof(failures)='integer' AND failures>=0),\n", "")
	definition = strings.ReplaceAll(definition, "CHECK(failures<=fence AND failures<=max_attempts AND (state NOT IN ('queued','running') OR failures<max_attempts))", "CHECK(fence<=max_attempts AND (state!='queued' OR fence<max_attempts))")
	tx, err := raw.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	columns := "id,uuid,kind,work_key,resource_key,arguments,state,revision,priority,fence,max_attempts,available_at_ms,owner_uuid,lease_until_ms,progress,result,error_code,created_at_ms,updated_at_ms"
	_, err = tx.Exec("PRAGMA defer_foreign_keys=ON; CREATE TABLE pre107_jobs AS SELECT " + columns + " FROM archive_jobs; DROP TABLE archive_jobs; " + definition + "; INSERT INTO archive_jobs SELECT * FROM pre107_jobs; DROP TABLE pre107_jobs;")
	require.NoError(t, err)
	for _, s := range objects {
		if strings.Contains(s, "CREATE TRIGGER archive_job_transition") {
			start := strings.Index(s, " OR NEW.failures")
			require.NotEqual(t, -1, start)
			body := strings.Index(s, "BEGIN SELECT")
			require.Positive(t, body)
			s = s[:start] + s[body:]
		}
		_, err = tx.Exec(s)
		require.NoError(t, err)
	}
	_, err = tx.Exec("DELETE FROM native_migration_history WHERE version=1000107")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestInterruptedWorkMigrationRetainsJobsAndCountsOnlyFailures(t *testing.T) {
	f := newDurableJobFixture(t)
	input := jobSubmission("migration", "resource")
	input.MaxAttempts = 8
	f.submit(t, input)
	first := f.claim(t, uuid.NewString())
	require.NotNil(t, first)
	f.now = *first.LeaseUntil
	_, err := f.service.Recover(t.Context(), 100)
	require.NoError(t, err)
	second := f.claim(t, uuid.NewString())
	require.NotNil(t, second)
	queued := f.outcome(t, second, models.ArchiveJobOutcome{State: "retry", ErrorCode: "source_timeout", Result: json.RawMessage(`{}`), RetryAt: f.now.Add(time.Minute)})
	f.now = queued.AvailableAt
	running := f.claim(t, uuid.NewString())
	require.NotNil(t, running)
	require.EqualValues(t, 3, running.Fence)
	require.Equal(t, 1, running.Failures)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeInterruptedWorkSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000106")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"archive_jobs", "archive_job_attempts", "archive_job_submissions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(path), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Equal(t, running, f.find(t, running.UUID))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	// Neither direct counter edits nor a terminal success may consume a failure.
	_, err = raw.Exec("UPDATE archive_jobs SET failures=failures+1,revision=revision+1 WHERE uuid=?", running.UUID)
	require.Error(t, err)
	done := f.outcome(t, running, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{}`)})
	require.Equal(t, 1, done.Failures)
}
