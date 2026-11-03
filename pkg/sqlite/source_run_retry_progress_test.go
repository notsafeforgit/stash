package sqlite_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

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
	}{{"not_found", 30 * 24 * time.Hour}, {"access_denied", 24 * time.Hour}} {
		t.Run(test.code, func(t *testing.T) {
			f := newSourceRunFixture(t)
			request := f.request()
			run := f.claim(t, f.submit(t, request))
			run, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: test.code})
			require.NoError(t, err)
			require.Equal(t, test.delay, run.AvailableAt.Sub(f.now))
			require.Zero(t, run.Failures)
			require.Empty(t, run.Completed)
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
