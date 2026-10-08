package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceRunServicesSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceFairnessSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER source_run_attempt_pacing_bind;
 DROP TABLE source_run_attempt_failures; DROP TABLE source_run_attempt_pacing;
 DELETE FROM native_migration_history WHERE version=1000055;`)
	require.NoError(t, err)
}

func TestSourceRunServicesReserveBeforeChildAndReleaseAfterFinish(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	enriching := f.claim(t, job.UUID, 0)
	d := newPacingDownload(t, f, "https://x.com/different")
	r := d.claim(t)
	require.NotNil(t, r)
	reserve := func(url string) *models.SourceRunServiceReservation {
		p, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), url)
		require.NoError(t, err)
		require.Equal(t, r.UUID, p.RunUUID)
		require.Equal(t, r.Fence, p.Fence)
		return p
	}
	require.True(t, reserve("https://twitter.com/different").Ready)
	child := "https://www.redgifs.com/watch/example"
	require.True(t, reserve(child).Ready)
	ready, err := f.worker.ReserveSource(t.Context(), f.tokens[0], enriching.Lease(), child)
	require.NoError(t, err)
	require.False(t, ready, "an enrichment attempt on another parent cannot use the downloader's linked service")
	require.True(t, reserve(child).Ready, "reservation replay keeps the same owner")
	_, err = d.coordinator.ReserveSource(t.Context(), f.tokens[0], r.Lease(), child)
	require.Error(t, err)
	stale := r.Lease()
	stale.Fence++
	_, err = d.coordinator.ReserveSource(t.Context(), d.token, stale, child)
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	_, err = d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://user:password@unrelated.invalid/private")
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	_, err = d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.NoError(t, err)
	ready, err = f.worker.ReserveSource(t.Context(), f.tokens[0], enriching.Lease(), child)
	require.NoError(t, err)
	require.True(t, ready)
	_, err = d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), child)
	require.ErrorIs(t, err, models.ErrSourceRunLease)
}

func TestSourceRunServicesCompetingChildReservationsAreAtomic(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	enriching := f.claim(t, job.UUID, 0)
	d := newPacingDownload(t, f, "https://x.com/different")
	r := d.claim(t)
	var download *models.SourceRunServiceReservation
	var enrichment bool
	var one, two error
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Go(func() {
		<-start
		download, one = d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/one")
	})
	group.Go(func() {
		<-start
		enrichment, two = f.worker.ReserveSource(t.Context(), f.tokens[0], enriching.Lease(), "https://redgifs.com/watch/two")
	})
	close(start)
	group.Wait()
	require.NoError(t, one)
	require.NoError(t, two)
	require.NotEqual(t, download.Ready, enrichment)
}

func TestSourceRunServicesBusyDependencyWaitsBeforeRetryingParent(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	enriching := f.claim(t, job.UUID, 0)
	ready, err := f.worker.ReserveSource(t.Context(), f.tokens[0], enriching.Lease(), "https://redgifs.com/watch/one")
	require.NoError(t, err)
	require.True(t, ready)
	d := newPacingDownload(t, f, "https://x.com/different")
	r := d.claim(t)
	p, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/two")
	require.NoError(t, err)
	require.False(t, p.Ready)
	_, err = d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "timeout", ErrorScope: p.Scope})
	require.ErrorIs(t, err, models.ErrSourceRunInvalid, "an unheld service cannot be blamed for network failure")
	queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "source_busy", ErrorScope: p.Scope})
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT available_at_ms FROM source_pacing WHERE scope='service:redgifs'"))
	until := f.now.Add(time.Hour)
	_, err = raw.Exec("UPDATE source_pacing SET available_at_ms=? WHERE scope='service:redgifs'", until.UnixMilli())
	require.NoError(t, err)
	f.now = queued.AvailableAt
	require.Nil(t, d.claim(t), "retry must wait for its remembered child before touching Twitter")
	attempts, err := d.coordinator.Attempts(t.Context(), d.token, r.UUID, 0)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, p.Scope, attempts[0].ErrorScope)
	f.now = until
	retried := d.claim(t)
	require.NotNil(t, retried)
	require.EqualValues(t, 2, retried.Fence)
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_run_attempt_pacing WHERE run_uuid='"+r.UUID+"' AND fence=2 AND reserved=1"))
	require.EqualValues(t, until.UnixMilli(), queryUint(t, raw, "SELECT last_started_at_ms FROM source_pacing WHERE scope='service:redgifs'"))
}

func TestSourceRunServicesFailureOnlyCoolsTheContactedService(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://www.reddit.com/user/example")
	r := d.claim(t)
	p, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/one")
	require.NoError(t, err)
	require.True(t, p.Ready)
	for _, outcome := range []models.SourceRunOutcome{
		{State: "retry", ErrorCode: "timeout", ErrorScope: "service:instagram"},
		{State: "retry", ErrorCode: "worker_storage_unavailable", ErrorScope: p.Scope},
		{State: "succeeded", ErrorScope: p.Scope},
	} {
		_, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), outcome)
		require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	}
	queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "rate_limited", ErrorScope: p.Scope})
	require.NoError(t, err)
	require.Equal(t, f.now.Add(time.Hour), queued.AvailableAt)
	other := newPacingDownload(t, f, "https://reddit.com/user/other")
	require.NotNil(t, other.claim(t), "Reddit was not the failing service")
	child := newPacingDownload(t, f, "https://redgifs.com/watch/other")
	require.Nil(t, child.claim(t))
	f.now = f.now.Add(30 * time.Minute)
	_, err = d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "rate_limited", ErrorScope: p.Scope})
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	f.now = queued.AvailableAt
	require.NotNil(t, child.claim(t), "a stale finish cannot extend the service cooldown")
}

func TestSourceRunServicesExpiredAttemptRetainsBlockedDependency(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://x.com/example")
	r := d.claim(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	until := f.now.Add(time.Hour)
	_, err := raw.Exec("INSERT INTO source_pacing(scope,available_at_ms) VALUES('service:redgifs',?)", until.UnixMilli())
	require.NoError(t, err)
	p, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/one")
	require.NoError(t, err)
	require.False(t, p.Ready)
	f.now = r.LeaseUntil.Add(time.Second)
	require.Nil(t, d.claim(t), "expiry recovers the attempt into backoff")
	f.now = f.now.Add(6 * time.Minute)
	require.Nil(t, d.claim(t), "loss of the worker did not lose its blocked dependency")
	f.now = until
	retried := d.claim(t)
	require.NotNil(t, retried)
	p, err = d.coordinator.ReserveSource(t.Context(), d.token, retried.Lease(), "https://redgifs.com/watch/one")
	require.NoError(t, err)
	require.True(t, p.Ready)
}

func TestSourceRunServicesWidenedTraversalDoesNotInheritPreviousChild(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://x.com/example")
	r := d.claim(t)
	_, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/one")
	require.NoError(t, err)
	queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "source_busy", ErrorScope: "service:redgifs"})
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_pacing SET available_at_ms=? WHERE scope='service:redgifs'", f.now.Add(time.Hour).UnixMilli())
	require.NoError(t, err)
	f.now = queued.AvailableAt
	_, err = d.coordinator.Submit(t.Context(), d.token, models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: r.CollectionUUID,
		CollectionRevision: r.CollectionRevision, Operation: r.Operation, PolicySHA256: r.PolicySHA256,
		Window: models.SourceWindow{Until: f.now}, CooldownSeconds: r.CooldownSeconds})
	require.NoError(t, err)
	retried := d.claim(t)
	require.NotNil(t, retried, "a new traversal must discover which linked services it needs")
	require.NotEqual(t, *r.Window, *retried.Window)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_run_attempt_pacing WHERE run_uuid='"+r.UUID+"' AND fence=2"))
}

func TestSourceRunServicesWriteFailureCannotCommitPartialState(t *testing.T) {
	for _, step := range []string{"reservation", "failure"} {
		t.Run(step, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			d := newPacingDownload(t, f, "https://x.com/example")
			r := d.claim(t)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			if step == "reservation" {
				_, err := raw.Exec("CREATE TRIGGER break_reservation BEFORE UPDATE OF last_started_at_ms ON source_pacing BEGIN SELECT RAISE(ABORT,'fixture'); END")
				require.NoError(t, err)
			} else {
				p, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/one")
				require.NoError(t, err)
				require.True(t, p.Ready)
				_, err = raw.Exec("CREATE TRIGGER break_failure BEFORE INSERT ON source_run_attempt_failures BEGIN SELECT RAISE(ABORT,'fixture'); END")
				require.NoError(t, err)
			}
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if step == "reservation" {
					_, err := f.repo.SourceRun.ReserveSource(ctx, r.Lease(), "https://redgifs.com/watch/one", f.now)
					require.Error(t, err)
				} else {
					_, err := f.repo.SourceRun.Finish(ctx, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "timeout", ErrorScope: "service:redgifs"}, f.now)
					require.Error(t, err)
				}
				return nil // A caller swallowing an error must not commit partial work.
			})
			require.ErrorIs(t, err, models.ErrSourcePacingAtomic)
			current, err := d.coordinator.Find(t.Context(), d.token, r.UUID)
			require.NoError(t, err)
			require.Equal(t, r, current)
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_attempt_failures"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_pacing WHERE available_at_ms>0"))
			if step == "reservation" {
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_pacing WHERE scope='service:redgifs'"))
			}
		})
	}
}

func TestSourceRunServicesMigrationPreservesExistingAttempts(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "collision"}[collision], func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			d := newPacingDownload(t, f, "https://x.com/example")
			require.NotNil(t, d.claim(t))
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			removeSourceRunServicesSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000054")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"source_runs", "source_run_attempts", "source_run_pacing", "source_pacing"} {
				before[table] = albumJobRows(t, raw, table)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if collision {
				_, err = raw.Exec("CREATE TABLE source_run_attempt_pacing(original TEXT); INSERT INTO source_run_attempt_pacing VALUES('retained')")
				require.NoError(t, err)
			}
			err = f.db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM source_run_attempt_pacing").Scan(&original))
				require.Equal(t, "retained", original)
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_run_attempt_pacing WHERE scope='service:twitter' AND reserved=1"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_attempt_failures"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
