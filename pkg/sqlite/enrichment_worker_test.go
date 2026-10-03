package sqlite_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentWorkerFailureRetainsCheckpointsAndReplaysOriginalAttempt(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	ready, err := f.worker.Ready(t.Context(), f.tokens[0], f.collection.UUID, 10)
	require.NoError(t, err)
	require.Equal(t, []models.EnrichmentTarget{*f.target}, ready)
	job := f.admit(t)
	described, err := f.worker.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, f.target.URL, described.Target.URL)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[1], running.Lease(), "timeout")
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	for _, invalid := range []string{"", "succeeded", "https://private.invalid/token", "arbitrary_failure"} {
		_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), invalid)
		require.ErrorIs(t, err, ingest.ErrInvalid)
	}
	first, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	require.Equal(t, "retry", first.Outcome)
	require.Equal(t, f.producers[0].UUID, first.ProducerUUID)
	require.NotNil(t, first.EndedAt)
	queued, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "queued", queued.State)
	require.Equal(t, f.now.Add(5*time.Minute), queued.AvailableAt)
	retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *head, retained.EnrichmentCheckpointReceipt)
	publication, err := f.worker.Publication(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Nil(t, publication)
	f.now = queued.AvailableAt
	next := f.claim(t, job.UUID, 1)
	replay, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	require.Equal(t, first, replay)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	current, err := f.worker.Find(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, next, current, "old failure replay cannot stop the successor")
	terminal, err := f.worker.Fail(t.Context(), f.tokens[1], next.Lease(), "authentication")
	require.NoError(t, err)
	require.Equal(t, "failed", terminal.Outcome)
	ready, err = f.worker.Ready(t.Context(), f.tokens[1], f.collection.UUID, 10)
	require.NoError(t, err)
	require.Empty(t, ready, "terminal jobs require explicit owner retry")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	f.worker.Service = ingest.New(f.repo)
	replay, err = f.worker.Fail(t.Context(), f.tokens[1], next.Lease(), "authentication")
	require.NoError(t, err)
	require.Equal(t, terminal, replay)
	retained, err = f.worker.CheckpointHead(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *head, retained.EnrichmentCheckpointReceipt)
}

type enrichmentFailureBeforeCommit struct {
	models.ArchiveJobReaderWriter
	beforeCommit func()
}

func (s enrichmentFailureBeforeCommit) Finish(ctx context.Context, lease models.ArchiveJobLease, now time.Time, outcome models.ArchiveJobOutcome) (*models.ArchiveJob, error) {
	txn.AddPreCommitHook(ctx, func(context.Context) error { s.beforeCommit(); return nil })
	return s.ArchiveJobReaderWriter.Finish(ctx, lease, now, outcome)
}

func TestEnrichmentWorkerLateFailureCannotCommitAfterLeaseExpiry(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	f.service.Repo.ArchiveJob = enrichmentFailureBeforeCommit{f.repo.ArchiveJob, func() { f.now = f.now.Add(2 * time.Minute) }}
	_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, running, current)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Nil(t, attempts[0].EndedAt)
		return nil
	}))
}

func TestEnrichmentWorkerFailureBudgetAndCredentialReplay(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	for attempt := 1; attempt <= job.MaxAttempts; attempt++ {
		running := f.claim(t, job.UUID, 0)
		require.EqualValues(t, attempt, running.Fence)
		receipt, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
		require.NoError(t, err)
		replay, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
		require.NoError(t, err)
		require.Equal(t, receipt, replay)
		current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
		require.NoError(t, err)
		if attempt < job.MaxAttempts {
			require.Equal(t, "retry", receipt.Outcome)
			require.Equal(t, "queued", current.State)
			require.Equal(t, f.now.Add(5*time.Minute*time.Duration(1<<(attempt-1))), current.AvailableAt)
			f.now = current.AvailableAt
			continue
		}
		require.Equal(t, "failed", receipt.Outcome)
		require.Equal(t, "failed", current.State)
		_, replacement, err := f.service.IssueCredential(t.Context(), f.producers[0].UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID}}, nil)
		require.NoError(t, err)
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.Ingest.RevokeCredential(ctx, f.tokens[0][7:43]) }))
		_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
		require.ErrorIs(t, err, ingest.ErrUnauthorized)
		replay, err = f.worker.Fail(t.Context(), replacement, running.Lease(), "rate_limited")
		require.NoError(t, err)
		require.Equal(t, receipt, replay)
	}
}

func TestEnrichmentWorkerConcurrentFailureAcknowledgements(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	var workers sync.WaitGroup
	const count = 8
	receipts := make([]*ingest.EnrichmentFailureReceipt, count)
	errors := make([]error, count)
	for i := range count {
		workers.Go(func() {
			receipts[i], errors[i] = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
		})
	}
	workers.Wait()
	for i := range count {
		require.NoError(t, errors[i])
		require.NotNil(t, receipts[i])
		require.Equal(t, receipts[0], receipts[i])
	}
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, running.Revision+1, current.Revision)
	require.Equal(t, "queued", current.State)
}
