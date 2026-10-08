package sqlite_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceTraversalSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceServiceOriginsSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER IF EXISTS source_run_basis_insert;
DROP TRIGGER IF EXISTS source_run_basis_update;
DROP TRIGGER IF EXISTS source_run_attempt_basis;
DELETE FROM native_migration_history WHERE version=1000098`)
	require.NoError(t, err)
}

func TestSourceTraversalCoalescesAndPreservesNewRequestsAcrossRestart(t *testing.T) {
	f := newSourceRunFixture(t)
	input := f.request()
	input.Window = models.SourceWindow{Basis: scrape.TraversalBasis, Until: f.now}
	r := f.submit(t, input)
	require.Equal(t, r, f.submit(t, input))
	first := f.claim(t, r)
	require.Equal(t, input.Window, *first.Window)
	f.now = f.now.Add(10 * time.Second)
	later := input
	later.RequestUUID, later.Window.Until = uuid.NewString(), f.now
	pending := f.submit(t, later)
	require.Equal(t, r.UUID, pending.UUID)
	require.Equal(t, []models.SourceWindow{later.Window}, pending.Pending)
	require.Nil(t, f.claim(t, r), "the active scan excludes another worker")
	// An older repeated request is already covered by this attempt, but the
	// newer request must survive successful completion of the older scan.
	older := input
	older.RequestUUID = uuid.NewString()
	require.Equal(t, pending.Pending, f.submit(t, older).Pending)
	done := f.finish(t, first, "succeeded")
	require.Equal(t, "queued", done.State)
	require.Equal(t, []models.SourceWindow{input.Window}, done.Completed)
	require.Equal(t, []models.SourceWindow{later.Window}, done.Pending)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	f.service = ingest.New(f.repo)
	f.coordinator = ingest.NewRunCoordinator(f.service)
	f.coordinator.Now = func() time.Time { return f.now }
	require.Equal(t, done, f.find(t, done.UUID))
	f.now = done.AvailableAt
	next := f.claim(t, done)
	require.Equal(t, later.Window, *next.Window)
	retry := f.finish(t, next, "retry")
	require.Equal(t, []models.SourceWindow{input.Window}, retry.Completed)
	f.now = retry.AvailableAt
	replayed := f.claim(t, retry)
	require.Equal(t, later.Window, *replayed.Window)
	done = f.finish(t, replayed, "succeeded")
	require.Equal(t, "succeeded", done.State)
	require.Empty(t, done.Pending)
	require.Equal(t, []models.SourceWindow{later.Window}, done.Completed)
	require.Equal(t, done, f.submit(t, later), "an admission replay cannot create another scan")
}

func TestSourceTraversalCannotSupplyPublishedBackfillCoverage(t *testing.T) {
	f := newSourceRunFixture(t)
	published := f.request()
	published.Window.Since = nil
	dateRun := f.submit(t, published)
	scan := published
	scan.RequestUUID, scan.Window.Basis = uuid.NewString(), scrape.TraversalBasis
	scanRun := f.submit(t, scan)
	require.NotEqual(t, dateRun.UUID, scanRun.UUID, "even identical policy hashes cannot mix bases")
	active := f.claim(t, scanRun)
	require.Nil(t, f.claim(t, dateRun), "the different work basis must not bypass destination exclusion")
	done := f.finish(t, active, "succeeded")
	require.Equal(t, []models.SourceWindow{published.Window}, scrape.Subtract([]models.SourceWindow{published.Window}, done.Completed))
	f.now = done.AvailableAt
	require.NotNil(t, f.claim(t, dateRun), "the publication request remains runnable")
	scan.RequestUUID, scan.Operation = uuid.NewString(), "enrich"
	_, err := f.coordinator.Submit(t.Context(), f.token, scan)
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID)
	require.NoError(t, err)
	_, err = f.service.CompleteBackfill(t.Context(), f.token, models.BackfillCompletion{
		UUID: uuid.NewString(), BackfillSubject: models.BackfillSubject{RootUUID: f.root.UUID, Platform: "reddit", Account: "example"},
		Component: "reddit-profile-new", Window: scan.Window, PolicySHA256: scan.PolicySHA256,
	})
	require.ErrorIs(t, err, models.ErrBackfillInvalid)
}

func TestSourceTraversalMigrationPreservesExistingPublishedRuns(t *testing.T) {
	f := newSourceRunFixture(t)
	old := f.finish(t, f.claim(t, f.submit(t, f.request())), "succeeded")
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	beforeRuns := albumJobRows(t, raw, "source_runs")
	beforeAttempts := albumJobRows(t, raw, "source_run_attempts")
	beforeRequests := albumJobRows(t, raw, "source_run_requests")
	removeSourceTraversalSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000097,dirty=0")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(path), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.Equal(t, beforeRuns, albumJobRows(t, raw, "source_runs"))
	require.Equal(t, beforeAttempts, albumJobRows(t, raw, "source_run_attempts"))
	require.Equal(t, beforeRequests, albumJobRows(t, raw, "source_run_requests"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	for _, completed := range []string{
		`[{"since":null,"until":"2026-10-01T00:00:00Z","basis":"unknown"}]`,
		`[{"since":"2026-09-01T00:00:00Z","until":"2026-10-01T00:00:00Z","basis":"traversal"}]`,
		`[{"since":null,"until":"2026-10-01T00:00:00Z"},{"since":null,"until":"2026-10-01T00:00:00Z","basis":"traversal"}]`,
	} {
		_, err := raw.Exec("UPDATE source_runs SET completed=? WHERE uuid=?", completed, old.UUID)
		require.ErrorContains(t, err, "coverage bas")
	}
	require.Equal(t, beforeRuns, albumJobRows(t, raw, "source_runs"))
}
