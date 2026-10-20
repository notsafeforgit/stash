//go:build integration

package manager

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type failingBulkCheckpoint struct {
	models.LibraryBulkJobReaderWriter
}

func (s failingBulkCheckpoint) Applied(context.Context, string, int, models.LibraryBulkTarget) (int, error) {
	return 0, errors.New("simulated checkpoint failure")
}

func TestBulkFailureIsAtomicAndVisible(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	mgr, jobs := bulkTestManager(t, filepath.Join(t.TempDir(), "bulk.sqlite"))
	jobs.SetSync(true)
	ids := bulkSceneFixtures(t, mgr.Repository, 2)
	mgr.Repository.LibraryBulkJob = failingBulkCheckpoint{mgr.Repository.LibraryBulkJob}
	id, _, err := mgr.BulkUpdate(t.Context(), "checkpoint fixture", bulkIDs(ids...), bulkSceneCommand(), hook.SceneUpdatePost, nil, nil)
	require.NoError(t, err)
	state := jobs.GetJob(id)
	require.Equal(t, job.StatusPaused, state.Status)
	require.Contains(t, *state.Error, "simulated checkpoint failure")
	require.Nil(t, state.EndTime, "unfinished durable work is not terminal")
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range ids {
			s, err := mgr.Repository.Scene.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, "original", s.Title)
		}
		saved, err := mgr.Repository.LibraryBulkJob.Find(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, saved)
		require.Zero(t, saved.Processed)
		return nil
	}))
	require.NoError(t, jobs.Shutdown(context.Background()))
	mgr.Repository = mgr.Database.Repository()
	resumed := job.NewManager()
	t.Cleanup(func() { require.NoError(t, resumed.Shutdown(context.Background())) })
	mgr.JobManager = resumed
	history := sqlite.NewLibraryJobHistory(mgr.Database)
	require.NoError(t, resumed.EnableHistory(t.Context(), history, func(ctx context.Context) ([]job.Restoration, error) { return mgr.restoreBulkUpdates(ctx, history) }))
	waitBulkTerminal(t, resumed, id, job.StatusFinished)
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range ids {
			s, err := mgr.Repository.Scene.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, "updated", s.Title)
		}
		return nil
	}))
}
