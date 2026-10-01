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
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func (f intakePublicationFixture) worker(t *testing.T, effects func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error) *ingest.FileWorker {
	t.Helper()
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&imagefile.Decorator{FFProbe: intakeProbe(t)}}}
	return ingest.NewFileWorker(f.service, func(models.ArchiveEntityKind) *file.Scanner { return scanner }, effects)
}

type workerResult struct {
	RegistrationCommitted bool                            `json:"registration_committed"`
	MediaIngested         bool                            `json:"media_ingested"`
	Publication           *ingest.IntakePublicationResult `json:"publication"`
}

func (f intakePublicationFixture) fileStatus(t *testing.T, event string) (*ingest.ReceiptStatus, workerResult) {
	t.Helper()
	status, err := f.service.ReceiptStatus(t.Context(), f.token, event)
	require.NoError(t, err)
	var result workerResult
	require.NoError(t, json.Unmarshal(status.Result, &result))
	return status, result
}

func TestFileWorkerCommitsAlbumBeforeEffectsAndSurvivesTokenRotation(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	accepted, err := f.submitFile(t, event)
	require.NoError(t, err)
	repo := f.service.Repo
	_, replacement, err := f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Ingest.RevokeCredential(ctx, f.credential.UUID) }))
	f.token = replacement
	called := 0
	worker := f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
		called++
		require.Equal(t, event.EventUUID, work.EventUUID)
		require.True(t, result.MediaCreated)
		require.NotEmpty(t, result.GalleryUUID)
		// A separate transaction can see publication: effects never run inside
		// the file/album write transaction, and its result is not yet complete.
		status, pending := f.fileStatus(t, event.EventUUID)
		require.Equal(t, "running", status.State)
		require.True(t, pending.RegistrationCommitted)
		require.False(t, pending.MediaIngested)
		require.Equal(t, &result, pending.Publication)
		return repo.WithReadTxn(ctx, func(ctx context.Context) error {
			gallery, err := repo.ArchiveEntity.Find(ctx, result.GalleryUUID)
			if err != nil {
				return err
			}
			members, err := repo.Gallery.GetImageIDs(ctx, *gallery.LocalID)
			require.Len(t, members, 1)
			return err
		})
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, 1, called)
	status, result := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.True(t, result.MediaIngested)
	require.True(t, result.RegistrationCommitted)
	require.Equal(t, accepted, status.Receipt)
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.False(t, processed)
	replay, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.Equal(t, accepted, replay)
}

func TestFileWorkerResumesCommittedPublicationAfterFailureAndRestart(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	_, err := f.submitFile(t, event)
	require.NoError(t, err)
	worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		return errors.New("fixture effect unavailable")
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, pending := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "queued", status.State)
	require.Equal(t, "file_processing_unavailable", status.ErrorCode)
	require.True(t, pending.RegistrationCommitted)
	require.False(t, pending.MediaIngested)
	require.NotNil(t, pending.Publication)
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.False(t, processed, "retry delay survives submission and worker loops")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	worker = f.worker(t, func(_ context.Context, _ ingest.FileWork, result ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
		require.Equal(t, pending.Publication, &result)
		require.True(t, result.MediaCreated, "the original creation remains a fact when effects resume")
		return nil
	})
	now := time.Now().Add(time.Minute)
	worker.Durable.Now = func() time.Time { return now }
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, completed := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.EqualValues(t, 2, status.Attempt)
	require.Equal(t, pending.Publication, completed.Publication)
	require.True(t, completed.MediaIngested)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		intakes, err := f.service.Repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
		require.Len(t, intakes, 1, "restart must not create another item or intake")
		return err
	}))
}

func TestFileWorkerRejectsChangedClaimsBeforePublishing(t *testing.T) {
	for _, mode := range []string{"wrong digest", "changed bytes", "disabled collection"} {
		t.Run(mode, func(t *testing.T) {
			f := newIntakePublicationFixture(t, true)
			event := f.fileEvent(t)
			if mode == "wrong digest" {
				event.SHA256 = ingest.Digest([]byte("different"))
			}
			_, err := f.submitFile(t, event)
			require.NoError(t, err)
			if mode == "changed bytes" {
				require.NoError(t, os.WriteFile(f.path, []byte("replacement"), 0600))
			}
			if mode == "disabled collection" {
				require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
					definition := f.collection.SourceCollectionDefinition
					definition.State = "disabled"
					_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
					return err
				}))
			}
			worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
				t.Error("effects must not run")
				return nil
			})
			processed, err := worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			status, result := f.fileStatus(t, event.EventUUID)
			require.Equal(t, "failed", status.State)
			require.False(t, result.MediaIngested)
			require.False(t, result.RegistrationCommitted)
			require.Nil(t, result.Publication)
			require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				media, err := f.service.Repo.File.FindByPath(ctx, f.path, true)
				require.Nil(t, media)
				return err
			}))
		})
	}
}

type changingFileJobStore struct {
	models.ArchiveJobReaderWriter
	path string
}

func (s changingFileJobStore) Finish(ctx context.Context, lease models.ArchiveJobLease, now time.Time, outcome models.ArchiveJobOutcome) (*models.ArchiveJob, error) {
	ret, err := s.ArchiveJobReaderWriter.Finish(ctx, lease, now, outcome)
	if err == nil && outcome.State == "succeeded" {
		err = os.WriteFile(s.path, []byte("changed immediately before commit"), 0600)
	}
	return ret, err
}

func TestFileWorkerRechecksFileBeforeFinalCommit(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	_, err := f.submitFile(t, event)
	require.NoError(t, err)
	worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		return nil
	})
	worker.Durable.Repo.ArchiveJob = changingFileJobStore{ArchiveJobReaderWriter: worker.Durable.Repo.ArchiveJob, path: f.path}
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, result := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "failed", status.State)
	require.Equal(t, "file_changed", status.ErrorCode)
	require.False(t, result.MediaIngested)
	require.True(t, result.RegistrationCommitted, "an earlier committed phase is not hidden by a later failure")
}

func TestFileWorkerShutdownLeavesRecoverableEffectsAndCancellationStopsCompletion(t *testing.T) {
	for _, mode := range []string{"shutdown", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newIntakePublicationFixture(t, true)
			event := f.fileEvent(t)
			accepted, err := f.submitFile(t, event)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			worker := f.worker(t, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
				if mode == "shutdown" {
					cancel()
					return ctx.Err()
				}
				var current *models.ArchiveJob
				repo := f.service.Repo
				if err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
					var err error
					current, err = repo.ArchiveJob.Find(ctx, accepted.JobUUID)
					return err
				}); err != nil {
					return err
				}
				_, err := job.NewDurable(repo).Cancel(ctx, current.UUID, current.Revision)
				return err
			})
			processed, err := worker.ProcessNext(ctx)
			require.True(t, processed)
			require.Error(t, err)
			status, pending := f.fileStatus(t, event.EventUUID)
			require.True(t, pending.RegistrationCommitted)
			require.False(t, pending.MediaIngested)
			if mode == "cancel" {
				require.Equal(t, "cancelled", status.State)
				return
			}
			require.Equal(t, "running", status.State)
			worker = f.worker(t, func(_ context.Context, _ ingest.FileWork, result ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
				require.Equal(t, pending.Publication, &result)
				return nil
			})
			now := time.Now().Add(2 * time.Minute)
			worker.Durable.Now = func() time.Time { return now }
			processed, err = worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			status, completed := f.fileStatus(t, event.EventUUID)
			require.Equal(t, "succeeded", status.State)
			require.True(t, completed.MediaIngested)
		})
	}
}

func TestFileWorkerRenewsLeaseWhileEffectsRun(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	_, err := f.submitFile(t, event)
	require.NoError(t, err)
	worker := f.worker(t, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
		timer := time.NewTimer(5500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		other, err := job.NewDurable(f.service.Repo).Claim(ctx, models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
		require.Nil(t, other, "renewal keeps a second worker from recovering a live attempt")
		return err
	})
	worker.LeaseDuration = 5 * time.Second
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, result := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.True(t, result.MediaIngested)
	require.EqualValues(t, 1, status.Attempt)
}
