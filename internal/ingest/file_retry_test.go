package ingest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestProducerFileRetryKeepsOriginalWorkAndCommittedPublication(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "registration", true: "effects"}[published], func(t *testing.T) {
			f := newIntakePublicationFixture(t, true)
			f.linkSecondImage(t)
			repo := f.service.Repo
			if !published {
				f.service.Repo.SourceGallery = conflictingIntakeGallery{repo.SourceGallery}
			}
			event := f.fileEvent(t)
			accepted, err := f.submitFile(t, event)
			require.NoError(t, err)
			worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
				return errors.New("effects unavailable")
			})
			_, err = worker.ProcessNext(t.Context())
			require.NoError(t, err)
			var parent *models.ArchiveJob
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				var err error
				parent, err = repo.ArchiveJob.Find(ctx, accepted.JobUUID)
				return err
			}))
			if published {
				parent, err = job.NewDurable(repo).Cancel(t.Context(), parent.UUID, parent.Revision)
				require.NoError(t, err)
			}
			f.service.Repo.SourceGallery = repo.SourceGallery
			request := uuid.NewString()
			_, err = f.service.RetryFile(t.Context(), parent.UUID, parent.Revision+1, request)
			require.ErrorIs(t, err, models.ErrArchiveJobConflict)
			retried, err := f.service.RetryFile(t.Context(), parent.UUID, parent.Revision, request)
			require.NoError(t, err)
			require.NotEqual(t, parent.UUID, retried.JobUUID)
			require.Equal(t, published, retried.RegistrationCommitted)
			replay, err := f.service.RetryFile(t.Context(), parent.UUID, parent.Revision, request)
			require.NoError(t, err)
			require.Equal(t, retried, replay)
			alias, err := f.service.RetryFile(t.Context(), parent.UUID, parent.Revision, uuid.NewString())
			require.NoError(t, err)
			require.Equal(t, retried.JobUUID, alias.JobUUID, "equivalent retry requests coalesce")
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			worker = f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
				require.Equal(t, event.EventUUID, work.EventUUID)
				if published {
					require.Equal(t, retried.Publication, &result)
				}
				return guard(ctx)
			})
			processed, err := worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			done, err := f.service.FileRetryStatus(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, "succeeded", done.State)
			require.True(t, done.MediaIngested)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				original, err := repo.ArchiveJob.Find(ctx, parent.UUID)
				require.NoError(t, err)
				require.Equal(t, parent, original, "terminal attempt history remains unchanged")
				intakes, err := repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
				require.Len(t, intakes, 1, "retry retains original intake identity")
				return err
			}))
			replay, err = f.service.RetryFile(t.Context(), parent.UUID, parent.Revision, request)
			require.NoError(t, err)
			require.Equal(t, done, replay)
		})
	}
}

func TestProducerFileRetryDoesNotRefreshRemovedPathFence(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	accepted, err := f.submitFile(t, event)
	require.NoError(t, err)
	repo := f.service.Repo
	file := f.createScannedFile(t)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.File.Destroy(ctx, file.Base().ID) }))
	worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		t.Fatal("removed paths cannot publish")
		return nil
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	var parent *models.ArchiveJob
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		parent, err = repo.ArchiveJob.Find(ctx, accepted.JobUUID)
		return err
	}))
	require.Equal(t, "failed", parent.State)
	retry, err := f.service.RetryFile(t.Context(), parent.UUID, parent.Revision, uuid.NewString())
	require.NoError(t, err)
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	done, err := f.service.FileRetryStatus(t.Context(), retry.RequestUUID)
	require.NoError(t, err)
	require.Equal(t, "failed", done.State)
	require.Equal(t, "file_changed", done.ErrorCode)
	require.Equal(t, "path_removed_or_moved", done.Failure.Reason)
}
