package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func bulkStoreFixture(t *testing.T) (*Database, models.Repository, *LibraryJobHistory) {
	t.Helper()
	db := NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "bulk.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db, db.Repository(), NewLibraryJobHistory(db)
}

func bulkAdmission(count int) models.LibraryBulkAdmission {
	input := models.LibraryBulkAdmission{
		ID: uuid.NewString(), Generation: 1, Description: "fixture", AddedAt: time.Now(),
		CommandVersion: 1, CommandKind: "scene.date_from_mtime", CommandJSON: []byte("{}"), ReferencesJSON: []byte("[]"),
	}
	for i := range count {
		input.Targets = append(input.Targets, models.LibraryBulkTarget{EntityID: i + 1, EntityUUID: uuid.NewString()})
	}
	return input
}

func runBulkHistory(t *testing.T, h *LibraryJobHistory, id string) job.Job {
	t.Helper()
	status, err := h.Find(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, status)
	now := time.Now()
	status.Status, status.StartTime = job.StatusRunning, &now
	require.NoError(t, h.Save(t.Context(), *status))
	return *status
}

func TestDurableBulkAdmissionIsAtomicAndBounded(t *testing.T) {
	_, r, h := bulkStoreFixture(t)
	input := bulkAdmission(1)
	require.ErrorContains(t, r.LibraryBulkJob.Admit(t.Context(), input), "managed write")
	for _, name := range []string{"duplicate", "command", "notice", "references", "targets"} {
		t.Run(name, func(t *testing.T) {
			input := bulkAdmission(1)
			switch name {
			case "duplicate":
				input.Targets = append(input.Targets, input.Targets[0])
			case "command":
				input.CommandJSON = []byte(`{"data":"` + strings.Repeat("x", models.MaxLibraryBulkBytes) + `"}`)
			case "notice":
				input.NoticeJSON = []byte(strings.Repeat("x", models.MaxLibraryBulkBytes+1))
			case "references":
				input.ReferencesJSON = []byte("null")
			case "targets":
				input.Targets = make([]models.LibraryBulkTarget, 100001)
			}
			require.Error(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }))
			saved, err := h.Find(t.Context(), input.ID)
			require.NoError(t, err)
			require.Nil(t, saved, "a rejected admission leaves no job or command")
		})
	}
	var first string
	for i := range models.MaxLibraryBulkJobs {
		input := bulkAdmission(0)
		if i == 0 {
			first = input.ID
		}
		require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }))
	}
	require.ErrorContains(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }), "limit reached")
	status := runBulkHistory(t, h, first)
	now := time.Now()
	status.Status, status.EndTime = job.StatusFinished, &now
	require.NoError(t, h.Save(t.Context(), status))
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }))
	require.NoError(t, r.WithReadTxn(t.Context(), func(ctx context.Context) error {
		saved, err := r.LibraryBulkJob.Find(ctx, first)
		require.NoError(t, err)
		require.Nil(t, saved)
		var targets int
		require.NoError(t, dbWrapper.Get(ctx, &targets, "SELECT count(*) FROM library_bulk_targets WHERE job_id=?", first))
		require.Zero(t, targets)
		return nil
	}))
}

func TestDurableBulkCheckpointsFenceOldProcessesAndBoundNotifications(t *testing.T) {
	_, r, h := bulkStoreFixture(t)
	input := bulkAdmission(1)
	input.NoticeJSON = []byte(`{"type":"Scene.Update.Post"}`)
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }))
	old := runBulkHistory(t, h, input.ID)
	var target *models.LibraryBulkTarget
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		target, err = r.LibraryBulkJob.Next(ctx, input.ID, old.Generation)
		return err
	}))
	rollback := errors.New("rollback edit and checkpoint")
	require.ErrorIs(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := r.LibraryBulkJob.Applied(ctx, input.ID, old.Generation, *target)
		require.NoError(t, err)
		return rollback
	}), rollback)
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		next, err := r.LibraryBulkJob.Next(ctx, input.ID, old.Generation)
		require.NoError(t, err)
		require.Equal(t, "PENDING", next.State)
		processed, err := r.LibraryBulkJob.Applied(ctx, input.ID, old.Generation, *target)
		require.Equal(t, 1, processed)
		return err
	}))
	require.NoError(t, h.Interrupt(t.Context()))
	current := runBulkHistory(t, h, input.ID)
	require.Equal(t, old.Generation+1, current.Generation)
	require.ErrorIs(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := r.LibraryBulkJob.Next(ctx, input.ID, old.Generation)
		return err
	}), models.ErrLibraryBulkInterrupted)
	now := time.Now()
	old.Status, old.EndTime = job.StatusFinished, &now
	require.ErrorContains(t, h.Save(t.Context(), old), "superseded")
	for attempt := range models.MaxLibraryHookTries {
		require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
			next, err := r.LibraryBulkJob.Next(ctx, input.ID, current.Generation)
			require.NoError(t, err)
			require.Equal(t, "NOTIFY", next.State, "committed metadata is never repeated")
			require.Equal(t, attempt, next.HookAttempts)
			require.NoError(t, r.LibraryBulkJob.StartNotification(ctx, input.ID, current.Generation, *next))
			require.Error(t, r.LibraryBulkJob.StartNotification(ctx, input.ID, current.Generation, *next), "attempt claim is conditional")
			next.HookAttempts++
			failure := "plugin unavailable"
			return r.LibraryBulkJob.FinishNotification(ctx, input.ID, current.Generation, *next, &failure)
		}))
	}
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		next, err := r.LibraryBulkJob.Next(ctx, input.ID, current.Generation)
		require.NoError(t, err)
		require.Nil(t, next)
		saved, err := r.LibraryBulkJob.Find(ctx, input.ID)
		require.NoError(t, err)
		require.Equal(t, 1, saved.Processed)
		require.Equal(t, 1, saved.Updated)
		require.Equal(t, 1, saved.NotificationFailed)
		return nil
	}))
	require.NoError(t, h.Cancel(t.Context(), input.ID, current.Generation))
	require.NoError(t, h.Interrupt(t.Context()))
	cancelled, err := h.Find(t.Context(), input.ID)
	require.NoError(t, err)
	require.Equal(t, job.StatusCancelled, cancelled.Status)
	require.NotNil(t, cancelled.EndTime)
	require.NoError(t, r.WithReadTxn(t.Context(), func(ctx context.Context) error {
		saved, err := r.LibraryBulkJob.Find(ctx, input.ID)
		require.NoError(t, err)
		require.Nil(t, saved, "cancellation after interruption reclaims heavy records")
		return nil
	}))
}

func TestDurableBulkFailureSamplesHaveCountAndByteLimits(t *testing.T) {
	_, r, h := bulkStoreFixture(t)
	input := bulkAdmission(30)
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }))
	current := runBulkHistory(t, h, input.ID)
	for i := range 30 {
		require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
			next, err := r.LibraryBulkJob.Next(ctx, input.ID, current.Generation)
			require.NoError(t, err)
			processed, err := r.LibraryBulkJob.Failed(ctx, input.ID, current.Generation, *next, strings.Repeat("\x00世界", 4000))
			require.Equal(t, i+1, processed)
			return err
		}))
	}
	require.NoError(t, r.WithReadTxn(t.Context(), func(ctx context.Context) error {
		saved, err := r.LibraryBulkJob.Find(ctx, input.ID)
		require.NoError(t, err)
		require.Equal(t, 30, saved.Failed)
		require.LessOrEqual(t, len(saved.ErrorsJSON), 32768)
		var count int
		require.NoError(t, dbWrapper.Get(ctx, &count, "SELECT json_array_length(errors_json) FROM library_bulk_jobs"))
		require.Positive(t, count)
		require.LessOrEqual(t, count, models.MaxLibraryBulkErrors)
		return nil
	}))
}

func TestDurableBulkGlobalTargetBudgetAndReclamation(t *testing.T) {
	_, r, h := bulkStoreFixture(t)
	var first string
	for _, count := range []int{100000, 100000, 50000} {
		input := bulkAdmission(0)
		if first == "" {
			first = input.ID
		}
		require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
			require.NoError(t, r.LibraryBulkJob.Admit(ctx, input))
			// Populate a valid already-admitted cohort efficiently; assertions
			// exercise real admission against its global indexed work budget.
			_, err := dbWrapper.Exec(ctx, `WITH RECURSIVE n(i) AS (VALUES(0) UNION ALL SELECT i+1 FROM n WHERE i+1<?)
INSERT INTO library_bulk_targets(job_id,ordinal,entity_id,entity_uuid)
SELECT ?,i,i+1,printf('%08x-0000-4000-8000-000000000000',i) FROM n`, count, input.ID)
			require.NoError(t, err)
			_, err = dbWrapper.Exec(ctx, "UPDATE library_bulk_jobs SET total=? WHERE job_id=?", count, input.ID)
			return err
		}))
	}
	input := bulkAdmission(1)
	require.ErrorContains(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }), "limit reached")
	current := runBulkHistory(t, h, first)
	now := time.Now()
	current.Status, current.EndTime = job.StatusCancelled, &now
	require.NoError(t, h.Save(t.Context(), current))
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error { return r.LibraryBulkJob.Admit(ctx, input) }))
}
