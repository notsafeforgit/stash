package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceRunReviewDrainedOutboxPreservesCrawlAndSharedPacing(t *testing.T) {
	f := newSourceRunFixture(t)
	run := f.claim(t, f.submit(t, f.request()))
	progress := models.SourceRunProgress{ItemsSeen: 9000, FilesCompleted: 18, Cursor: "page-93"}
	_, err := f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
	require.NoError(t, err)
	run, err = f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "outbox_capacity"})
	require.NoError(t, err)
	require.True(t, run.AvailableAt.After(f.now))
	require.Zero(t, run.Failures, "local queue capacity must not consume website retries")
	require.Equal(t, 30*time.Second, run.AvailableAt.Sub(f.now))
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, run.UUID, 0)
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	blockedUntil := f.now.Add(time.Minute)
	_, err = raw.Exec("UPDATE source_pacing SET available_at_ms=? WHERE scope='service:reddit'", blockedUntil.UnixMilli())
	require.NoError(t, err)
	review := func(revision int64) (*models.SourceRun, error) {
		var result *models.SourceRun
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = f.repo.SourceRun.Review(ctx, run.UUID, revision, "retry", f.now)
			return err
		})
		return result, err
	}
	_, err = review(run.Revision - 1)
	require.ErrorIs(t, err, models.ErrSourceRunConflict)
	ready, err := review(run.Revision)
	require.NoError(t, err)
	require.Equal(t, f.now, ready.AvailableAt)
	require.Equal(t, run.Pending, ready.Pending)
	require.Equal(t, run.Completed, ready.Completed)
	require.Equal(t, progress, ready.Progress)
	require.Equal(t, run.Fence, ready.Fence)
	require.Zero(t, ready.Failures)
	require.Empty(t, ready.ErrorCode)
	remaining, err := f.coordinator.Attempts(t.Context(), f.token, run.UUID, 0)
	require.NoError(t, err)
	require.Equal(t, attempts, remaining)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_run_reviews WHERE action='retry'"))
	require.Nil(t, f.claim(t, ready), "clearing a local capacity delay must not bypass shared site pacing")
	f.now = blockedUntil
	active := f.claim(t, ready)
	require.NotNil(t, active)
	require.Equal(t, progress, active.Progress)
}

func TestSourceRunReviewCannotBypassWebsiteRetryDelays(t *testing.T) {
	for _, code := range []string{"rate_limited", "not_found", "access_denied", "network_unreachable"} {
		t.Run(code, func(t *testing.T) {
			f := newSourceRunFixture(t)
			run := f.claim(t, f.submit(t, f.request()))
			run, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: code})
			require.NoError(t, err)
			err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.SourceRun.Review(ctx, run.UUID, run.Revision, "retry", f.now)
				return err
			})
			require.ErrorIs(t, err, models.ErrSourceRunConflict)
			require.Equal(t, run, f.find(t, run.UUID))
		})
	}
}

func TestSourceRunNewScheduledWindowFinishesInterruptedCrawlFirst(t *testing.T) {
	f := newSourceRunFixture(t)
	request := f.request()
	request.Window.Since = nil
	run := f.claim(t, f.submit(t, request))
	progress := models.SourceRunProgress{ItemsSeen: 9994, FilesCompleted: 30, Cursor: "last-attachment"}
	_, err := f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
	require.NoError(t, err)
	f.finish(t, run, "retry")
	f.now = f.now.Add(24 * time.Hour)
	later := request
	later.RequestUUID = uuid.NewString()
	later.Window.Until = f.now
	run = f.submit(t, later)
	require.Len(t, run.Pending, 1)
	require.Equal(t, later.Window, run.Pending[0])
	run = f.claim(t, run)
	require.NotNil(t, run)
	require.Equal(t, request.Window, *run.Window)
	require.Equal(t, progress, run.Progress)
	require.Equal(t, []models.SourceWindow{{Since: &request.Window.Until, Until: later.Window.Until}}, run.Pending)
	run = f.finish(t, run, "succeeded")
	require.Equal(t, "queued", run.State, "newer requested coverage remains unfinished")
	require.Equal(t, []models.SourceWindow{request.Window}, run.Completed)
	f.now = run.AvailableAt.Add(time.Second)
	run = f.claim(t, run)
	require.NotNil(t, run)
	require.Equal(t, request.Window.Until, *run.Window.Since)
	require.Equal(t, later.Window.Until, run.Window.Until)
	require.Equal(t, models.SourceRunProgress{}, run.Progress)
	run = f.finish(t, run, "succeeded")
	require.Equal(t, "succeeded", run.State)
	require.Equal(t, []models.SourceWindow{later.Window}, run.Completed)
	require.Empty(t, run.Pending)
}

func TestSourceRunMissingProfileRechecksMonthlyAndPrivateProfileDaily(t *testing.T) {
	for _, test := range []struct {
		code  string
		delay time.Duration
	}{{"not_found", 30 * 24 * time.Hour}, {"access_denied", 24 * time.Hour},
		{"account_not_found", 30 * 24 * time.Hour}, {"account_suspended", 30 * 24 * time.Hour},
		{"account_deactivated", 30 * 24 * time.Hour}, {"account_unavailable", 30 * 24 * time.Hour}} {
		t.Run(test.code, func(t *testing.T) {
			f := newSourceRunFixture(t)
			request := f.request()
			run := f.claim(t, f.submit(t, request))
			run, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: test.code, ErrorScope: "service:reddit"})
			require.NoError(t, err)
			require.Equal(t, test.delay, run.AvailableAt.Sub(f.now))
			require.Zero(t, run.Failures)
			require.Empty(t, run.Completed)
			attempts, err := f.coordinator.Attempts(t.Context(), f.token, run.UUID, 0)
			require.NoError(t, err)
			require.Equal(t, test.code, attempts[0].ErrorCode)
			require.Equal(t, "service:reddit", attempts[0].ErrorScope)
			request.RequestUUID = uuid.NewString()
			request.Window.Since = nil
			repeated := f.submit(t, request)
			require.Equal(t, run.AvailableAt, repeated.AvailableAt)
			f.now = run.AvailableAt.Add(-time.Second)
			require.Nil(t, f.claim(t, repeated))
			f.now = run.AvailableAt
			require.NotNil(t, f.claim(t, repeated))
		})
	}
}
