package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func TestLibraryJobsReopenRecoversPendingWithoutReplaying(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	db := NewDatabase()
	require.NoError(t, db.Open(path))
	history := NewLibraryJobHistory(db)
	completed := job.Job{ID: uuid.NewString(), Status: job.StatusReady, Description: "completed", AddTime: time.Now()}
	queued := job.Job{ID: uuid.NewString(), Status: job.StatusReady, Description: "queued", AddTime: time.Now()}
	running := job.Job{ID: uuid.NewString(), Status: job.StatusReady, Description: "running", AddTime: time.Now()}
	for _, j := range []job.Job{completed, queued, running} {
		require.NoError(t, history.Create(t.Context(), j))
	}
	now := time.Now()
	completed.Status, completed.EndTime = job.StatusFinished, &now
	require.NoError(t, history.Save(t.Context(), completed))
	require.NoError(t, history.Save(t.Context(), completed), "identical outcome retry is idempotent")
	conflicting := completed
	conflicting.Status = job.StatusFailed
	require.Error(t, history.Save(t.Context(), conflicting), "a terminal outcome cannot be replaced")
	running.Status, running.StartTime = job.StatusRunning, &now
	require.NoError(t, history.Save(t.Context(), running))
	require.NoError(t, db.Close())
	db = NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	history = NewLibraryJobHistory(db)
	m := job.NewManager()
	t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
	require.NoError(t, m.EnableHistory(t.Context(), history))
	for _, id := range []string{queued.ID, running.ID} {
		value, err := m.Find(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, job.StatusInterrupted, value.Status)
		require.NotNil(t, value.EndTime)
		require.Contains(t, *value.Error, "not resumed")
	}
	value, err := m.Find(t.Context(), completed.ID)
	require.NoError(t, err)
	require.Equal(t, job.StatusFinished, value.Status)
	require.Empty(t, m.GetQueue())
	m.SetSync(true)
	id, err := m.Add(t.Context(), "new execution", job.MakeJobExec(func(context.Context, *job.Progress) error { return nil }))
	require.NoError(t, err)
	require.NotContains(t, []string{completed.ID, queued.ID, running.ID}, id)
	value, err = history.Find(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, job.StatusFinished, value.Status)
	page, err := m.History(t.Context(), 2, 0)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, id, page[0].ID)
	_, err = history.List(t.Context(), 101, 0)
	require.Error(t, err)
}

func TestLibraryJobHistoryBoundsOnlyTerminalRecords(t *testing.T) {
	db := NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	history := NewLibraryJobHistory(db)
	pending := job.Job{ID: uuid.NewString(), Status: job.StatusReady, AddTime: time.Unix(1, 0)}
	require.NoError(t, history.Create(t.Context(), pending))
	require.NoError(t, txn.WithTxn(t.Context(), db, func(ctx context.Context) error {
		for i := range 1002 {
			_, err := dbWrapper.Exec(ctx, `INSERT INTO library_jobs(id,status,description,progress,added_at,ended_at) VALUES(?,'FINISHED','test',1,?,?)`, uuid.NewString(), i+2, i+3)
			if err != nil {
				return err
			}
		}
		return pruneLibraryJobHistory(ctx)
	}))
	require.NoError(t, txn.WithReadTxn(t.Context(), db, func(ctx context.Context) error {
		var count int
		if err := dbWrapper.Get(ctx, &count, `SELECT count(*) FROM library_jobs`); err != nil {
			return err
		}
		require.Equal(t, 1001, count)
		return nil
	}))
	value, err := history.Find(t.Context(), pending.ID)
	require.NoError(t, err)
	require.Equal(t, job.StatusReady, value.Status)
}
