package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceRunRepeatedRestartsRetainProgressAndFailureBudget(t *testing.T) {
	f := newSourceRunFixture(t)
	run := f.submit(t, f.request())
	progress := models.SourceRunProgress{ItemsSeen: 17, FilesCompleted: 11, Cursor: "post-17"}
	for attempt := range 10 {
		active := f.claim(t, run)
		require.NotNil(t, active)
		require.EqualValues(t, attempt+1, active.Fence)
		if attempt == 0 {
			_, err := f.coordinator.Progress(t.Context(), f.token, active.Lease(), progress)
			require.NoError(t, err)
		} else {
			require.Equal(t, progress, active.Progress)
		}
		require.NoError(t, f.db.Close())
		require.NoError(t, f.db.Open(f.db.DatabasePath()))
		f.now = active.LeaseUntil.Add(time.Second)
		require.Nil(t, f.claim(t, run))
		run = f.find(t, run.UUID)
		require.Equal(t, "queued", run.State)
		require.Zero(t, run.Failures)
		require.LessOrEqual(t, run.AvailableAt.Sub(f.now), time.Minute)
		_, err := f.coordinator.Progress(t.Context(), f.token, active.Lease(), progress)
		require.ErrorIs(t, err, models.ErrSourceRunLease)
		f.now = run.AvailableAt
	}
	active := f.claim(t, run)
	require.NotNil(t, active)
	require.Equal(t, progress, active.Progress)
	require.Equal(t, "succeeded", f.finish(t, active, "succeeded").State)
}

func TestArchiveJobRestartsDoNotConsumeFailuresOrPermitStaleWorkers(t *testing.T) {
	f := newDurableJobFixture(t)
	input := jobSubmission("restartable", "destination")
	input.MaxAttempts = 2
	current := f.submit(t, input)
	for attempt := range 10 {
		active := f.claim(t, uuid.NewString())
		require.NotNil(t, active)
		require.EqualValues(t, attempt+1, active.Fence)
		_, err := f.service.Progress(t.Context(), active.Lease(), json.RawMessage(`{"checkpoint":17}`))
		require.NoError(t, err)
		require.NoError(t, f.db.Close())
		require.NoError(t, f.db.Open(f.db.DatabasePath()))
		f.now = *active.LeaseUntil
		_, err = f.service.Recover(t.Context(), 100)
		require.NoError(t, err)
		current = f.find(t, current.UUID)
		require.Equal(t, "queued", current.State)
		require.Zero(t, current.Failures)
		require.JSONEq(t, `{"checkpoint":17}`, string(current.Progress))
		require.Equal(t, f.now, current.AvailableAt)
		_, err = f.service.Publish(t.Context(), active.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
			t.Fatal("an expired worker must not publish")
			return models.ArchiveJobOutcome{}, nil
		})
		require.ErrorIs(t, err, models.ErrArchiveJobLease)
		f.now = current.AvailableAt
	}
	// Real failures still exhaust the configured budget after interruptions.
	for failure := 1; failure <= input.MaxAttempts; failure++ {
		active := f.claim(t, uuid.NewString())
		require.NotNil(t, active)
		current = f.outcome(t, active, models.ArchiveJobOutcome{State: "retry", ErrorCode: "fixture_failure", Result: json.RawMessage(`{}`), RetryAt: f.now.Add(time.Minute)})
		require.Equal(t, failure, current.Failures)
		f.now = current.AvailableAt
	}
	require.Equal(t, "failed", current.State)
	require.EqualValues(t, 12, current.Fence)
	require.Nil(t, f.claim(t, uuid.NewString()))
}
