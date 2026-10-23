package ingest_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/dedup"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type redownloadFixture struct {
	intakePublicationFixture
	original *ingest.PublishedIntake
	kept     *ingest.PublishedMedia
	service  *dedup.Service
}

func newRedownloadFixture(t *testing.T) redownloadFixture {
	t.Helper()
	f := redownloadFixture{intakePublicationFixture: newIntakePublicationFixture(t, true)}
	f.original = f.publishIntake(t, f.prepared, f.input)
	other, target := f.prepareOther(t, "keep.png")
	repo := f.serviceRepo()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.kept, err = other.PublishMedia(ctx, repo, *target, models.ArchiveImage)
		return err
	}))
	f.service = dedup.New(repo, "")
	f.remove(t)
	require.NoError(t, os.WriteFile(f.path, intakePNG(t), 0600))
	return f
}

func (f redownloadFixture) serviceRepo() models.Repository {
	return f.intakePublicationFixture.service.Repo
}

func (f redownloadFixture) remove(t *testing.T) {
	t.Helper()
	input := models.FileDeduplicationInput{RootUUID: f.root.UUID, KeepPath: "keep.png", RemovePath: "image.png"}
	preview, err := f.service.Preview(t.Context(), input)
	require.NoError(t, err)
	require.True(t, preview.Eligible, preview.BlockedReason)
	_, err = f.service.Apply(t.Context(), dedup.Request{FileDeduplicationInput: input, RequestUUID: uuid.NewString(), Signature: preview.Signature})
	require.NoError(t, err)
	require.NoFileExists(t, f.path)
}

func TestFileWorkerRedownloadUsesDeduplicationOwnerAndResumesEffects(t *testing.T) {
	f := newRedownloadFixture(t)
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
	require.True(t, pending.RegistrationCommitted)
	require.Equal(t, f.original.Result.MediaUUID, pending.Publication.MediaUUID)
	require.NotEqual(t, f.original.Result.FileUUID, pending.Publication.FileUUID, "a re-download has a new file lifetime")
	require.False(t, pending.Publication.MediaCreated)
	require.True(t, pending.Publication.FileLinked)
	require.Equal(t, &ingest.FileFailure{Stage: "effects", Reason: "file_processing_unavailable"}, pending.Failure)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	worker = f.worker(t, func(ctx context.Context, _ ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Equal(t, pending.Publication, &result)
		return guard(ctx)
	})
	worker.Durable.Now = func() time.Time { return time.Now().Add(time.Minute) }
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, done := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.True(t, done.MediaIngested)
	require.Equal(t, pending.Publication, done.Publication)
	// A later dedupe/re-download cycle accounts for both removals without
	// reviving either redirected file UUID or creating another image.
	f.remove(t)
	require.NoError(t, os.WriteFile(f.path, intakePNG(t), 0600))
	event.EventUUID = uuid.NewString()
	_, err = f.submitFile(t, event)
	require.NoError(t, err)
	worker.Effects = func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		return guard(ctx)
	}
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, again := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	require.Equal(t, f.original.Result.MediaUUID, again.Publication.MediaUUID)
	require.NotEqual(t, done.Publication.FileUUID, again.Publication.FileUUID)
	repo := f.serviceRepo()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		old, err := repo.ArchiveEntity.Resolve(ctx, f.original.Result.FileUUID)
		require.NoError(t, err)
		require.Equal(t, f.kept.File.Identity.UUID, old.UUID, "original deduplication redirect remains intact")
		files, err := repo.Image.GetFiles(ctx, *f.original.Media.Media.LocalID)
		require.Len(t, files, 2)
		return err
	}))
}

func TestRedownloadRejectsUnaccountedRemovalsAndChangedSurvivors(t *testing.T) {
	for _, change := range []string{"extra removal", "stale admission", "different bytes", "changed survivor", "deleted owner", "ambiguous owner", "other selected owner", "late owner change"} {
		t.Run(change, func(t *testing.T) {
			f := newRedownloadFixture(t)
			repo := f.serviceRepo()
			if change == "different bytes" {
				require.NoError(t, os.WriteFile(f.path, append(intakePNG(t), []byte("changed content")...), 0600))
			}
			prepared, target := f.prepareOther(t, "image.png")
			input := f.input
			input.UUID, input.Target = uuid.NewString(), *target
			if change == "stale admission" {
				input.Target = *f.target
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				switch change {
				case "extra removal":
					file := prepared.File()
					file.Base().ParentFolderID = f.kept.File.File.Base().ParentFolderID
					if err := repo.File.Create(ctx, file); err != nil {
						return err
					}
					return repo.File.Destroy(ctx, file.Base().ID)
				case "changed survivor":
					_, err := repo.FileContent.Advance(ctx, f.kept.File.Identity.UUID, f.kept.File.File.Base().Generation)
					return err
				case "deleted owner":
					return repo.Image.Destroy(ctx, *f.original.Media.Media.LocalID)
				case "ambiguous owner", "other selected owner":
					other := models.NewImage()
					create := &models.CreateImageInput{Image: &other}
					if change == "ambiguous owner" {
						create.FileIDs = []models.FileID{f.kept.File.File.Base().ID}
					}
					if err := repo.Image.Create(ctx, create); err != nil {
						return err
					}
					if change == "other selected owner" {
						attachment, err := repo.SourceAttachment.Find(ctx, input.Source.AttachmentUUID)
						if err != nil {
							return err
						}
						owner, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, other.ID)
						if err != nil {
							return err
						}
						_, err = repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: owner.UUID, ExpectedMediaRevision: owner.Revision, Origin: "review"})
						return err
					}
				}
				return nil
			}))
			if change == "extra removal" {
				input.Target = *f.captureTarget(t, "image.png", true)
			}
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.PublishIntake(ctx, repo, input)
				if err != nil {
					return err
				}
				if change == "late owner change" {
					return repo.Image.Destroy(ctx, *f.original.Media.Media.LocalID)
				}
				return nil
			})
			require.Error(t, err)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				file, err := repo.File.FindByPath(ctx, f.path, true)
				require.Nil(t, file, "unsafe re-download cannot recreate the removed location")
				return err
			}))
			require.FileExists(t, f.path)
		})
	}
}
