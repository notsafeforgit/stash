package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestManualFileRetryKeepsPublicationCoalescesAndRecoversAfterRestart(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	policy := f.policy(t, 0, map[string]models.MetadataMapping{})
	request := f.request(t)
	_, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	var event string
	worker := f.worker(t, func(_ context.Context, work ingest.FileWork, _ ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
		event = work.Publication.UUID
		return errors.New("effects unavailable")
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	pending := f.status(t, request.RequestUUID)
	cancelled, err := f.service.CancelManualFile(t.Context(), request.RequestUUID, pending.Revision)
	require.NoError(t, err)
	f.policy(t, policy.Revision, map[string]models.MetadataMapping{"title": {Value: json.RawMessage(`"Later title"`)}})
	retryRequest := uuid.NewString()
	retried, err := f.service.RetryManualFile(t.Context(), request.RequestUUID, cancelled.Revision, retryRequest)
	require.NoError(t, err)
	require.NotEqual(t, cancelled.JobUUID, retried.JobUUID)
	require.Equal(t, retryRequest, retried.RequestUUID)
	require.Equal(t, cancelled.JobUUID, retried.ResumeFromJobUUID)
	require.Equal(t, cancelled.Publication, retried.Publication)
	require.True(t, retried.RegistrationCommitted)
	aliasRequest := uuid.NewString()
	alias, err := f.service.RetryManualFile(t.Context(), request.RequestUUID, cancelled.Revision, aliasRequest)
	require.NoError(t, err)
	require.Equal(t, retried.JobUUID, alias.JobUUID, "equivalent concurrent retries share one active job")
	require.Equal(t, aliasRequest, alias.RequestUUID)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	worker = f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Equal(t, event, work.Publication.UUID)
		require.Equal(t, cancelled.Publication, &result)
		return guard(ctx)
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	done := f.status(t, retryRequest)
	require.True(t, done.MediaIngested)
	require.Equal(t, "cancelled", f.status(t, request.RequestUUID).State)
	require.True(t, f.status(t, aliasRequest).MediaIngested)
	require.JSONEq(t, `"Purchased image"`, string(intakeField(t, f.service.Repo, done.Publication.MediaUUID, "title").Value))
	require.NoError(t, os.Remove(f.path))
	recovered, err := f.service.RetryManualFile(t.Context(), request.RequestUUID, cancelled.Revision, retryRequest)
	require.NoError(t, err)
	require.Equal(t, done, recovered)
	_, err = f.service.RetryManualFile(t.Context(), request.RequestUUID, cancelled.Revision+1, retryRequest)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
}

func TestManualFileRetryBeforeAndAfterPublicationRetainsOriginalEvent(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	original := f.request(t)
	admitted, err := f.service.SubmitManualFile(t.Context(), original)
	require.NoError(t, err)
	cancelled, err := f.service.CancelManualFile(t.Context(), original.RequestUUID, admitted.Revision)
	require.NoError(t, err)
	firstID := uuid.NewString()
	first, err := f.service.RetryManualFile(t.Context(), original.RequestUUID, cancelled.Revision, firstID)
	require.NoError(t, err)
	first, err = f.service.CancelManualFile(t.Context(), firstID, first.Revision)
	require.NoError(t, err)
	secondID := uuid.NewString()
	_, err = f.service.RetryManualFile(t.Context(), firstID, first.Revision, secondID)
	require.NoError(t, err)
	var event string
	worker := f.worker(t, func(_ context.Context, work ingest.FileWork, _ ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
		event = work.Publication.UUID
		return ingest.ErrInvalid
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	failed := f.status(t, secondID)
	require.Equal(t, "failed", failed.State)
	require.True(t, failed.RegistrationCommitted)
	thirdID := uuid.NewString()
	third, err := f.service.RetryManualFile(t.Context(), secondID, failed.Revision, thirdID)
	require.NoError(t, err)
	// Cancellation before a resumed worker starts must still report the original registration.
	third, err = f.service.CancelManualFile(t.Context(), thirdID, third.Revision)
	require.NoError(t, err)
	require.True(t, third.RegistrationCommitted)
	lastID := uuid.NewString()
	_, err = f.service.RetryManualFile(t.Context(), thirdID, third.Revision, lastID)
	require.NoError(t, err)
	worker = f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Equal(t, event, work.Publication.UUID)
		require.Equal(t, failed.Publication, &result)
		return guard(ctx)
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, f.status(t, lastID).MediaIngested)
}

func TestManualFileRetryRejectsChangedUnpublishedReviewAndCommitRace(t *testing.T) {
	for _, duringCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "commit"}[duringCommit], func(t *testing.T) {
			f := newManualFileFixture(t, models.ArchiveImage)
			request := f.request(t)
			current, err := f.service.SubmitManualFile(t.Context(), request)
			require.NoError(t, err)
			current, err = f.service.CancelManualFile(t.Context(), request.RequestUUID, current.Revision)
			require.NoError(t, err)
			mutate := func() {
				require.NoError(t, os.Rename(f.path, f.path+".old"))
				require.NoError(t, os.WriteFile(f.path, intakePNG(t), 0600))
			}
			if duringCommit {
				f.service.Repo.ArchiveJob = changingManualAdmission{ArchiveJobReaderWriter: f.service.Repo.ArchiveJob, mutate: mutate}
			} else {
				mutate()
			}
			id := uuid.NewString()
			_, err = f.service.RetryManualFile(t.Context(), request.RequestUUID, current.Revision, id)
			require.ErrorIs(t, err, ingest.ErrManualFileChanged)
			_, err = f.service.ManualFileStatus(t.Context(), id)
			require.ErrorIs(t, err, ingest.ErrNotFound)
		})
	}
}

func TestManualFileWorkerRejectsMalformedArgumentsWithTerminalFailure(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	request := uuid.NewString()
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		args := json.RawMessage(`{"unsupported":true}`)
		_, err := f.service.Repo.ArchiveJob.Submit(ctx, models.ArchiveJobSubmission{RequestUUID: request, Kind: models.ArchiveJobVerifyMedia,
			WorkKey: ingest.Digest(args), ResourceKey: ingest.Digest([]byte(f.path)), Arguments: args, MaxAttempts: 8}, time.Now(), 10000)
		return err
	}))
	worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		t.Fatal("invalid work cannot run effects")
		return nil
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.service.Repo.ArchiveJob.FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		require.Equal(t, "failed", current.State)
		require.Equal(t, "invalid_file_work", current.ErrorCode)
		return nil
	}))
}
