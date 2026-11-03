package sqlite_test

import (
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourceRunRetryMigrationPreservesHistoryAndReschedulesMissingProfiles(t *testing.T) {
	f := newSourceRunFixture(t)
	run := f.claim(t, f.submit(t, f.request()))
	run, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "not_found"})
	require.NoError(t, err)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceRunConsolidationSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000134,dirty=0; UPDATE source_runs SET available_at_ms=updated_at_ms+86400000,revision=revision+1")
	require.NoError(t, err)
	attempts := albumJobRows(t, raw, "source_run_attempts")
	requests := albumJobRows(t, raw, "source_run_requests")
	var pending, completed string
	require.NoError(t, raw.QueryRow("SELECT pending,completed FROM source_runs WHERE uuid=?", run.UUID).Scan(&pending, &completed))
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(path), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	after := f.find(t, run.UUID)
	require.Equal(t, run.AvailableAt, after.AvailableAt)
	require.Equal(t, 30*24*time.Hour, after.AvailableAt.Sub(f.now))
	require.Equal(t, run.Revision+2, after.Revision)
	require.Equal(t, run.Pending, after.Pending)
	require.Equal(t, run.Completed, after.Completed)
	require.Equal(t, "queued", after.State)
	require.Equal(t, attempts, albumJobRows(t, raw, "source_run_attempts"))
	require.Equal(t, requests, albumJobRows(t, raw, "source_run_requests"))
	var actualPending, actualCompleted string
	require.NoError(t, raw.QueryRow("SELECT pending,completed FROM source_runs WHERE uuid=?", run.UUID).Scan(&actualPending, &actualCompleted))
	require.Equal(t, pending, actualPending)
	require.Equal(t, completed, actualCompleted)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_consolidations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_attachment_retries"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	require.Equal(t, after, f.find(t, run.UUID))
}
