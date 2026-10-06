package ingest_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostMediaUnlinkSurvivesRepeatedIngestion(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	repo := f.service.Repo
	first := f.publishIntake(t, f.prepared, f.input)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		a, err := repo.SourcePostMedia.Association(ctx, f.receipt.PostUUID, first.Result.MediaUUID)
		if err != nil {
			return err
		}
		_, err = repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: a.PostUUID, MediaUUID: a.MediaUUID, ExpectedPostRevision: a.PostRevision, ExpectedMediaRevision: a.MediaRevision, State: "unlinked", Origin: "review"})
		return err
	}))
	input := f.input
	input.UUID = uuid.NewString()
	later := f.publishIntake(t, f.prepared, input)
	require.Equal(t, first.Result.MediaUUID, later.Result.MediaUUID)
	require.False(t, later.Result.MediaCreated)
	require.Equal(t, "unlinked", later.Result.SourceMedia)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		choice, err := repo.SourceAttachment.MediaDecision(ctx, input.Source.AttachmentUUID)
		require.NoError(t, err)
		require.Equal(t, "linked", choice.State, "retained slot evidence remains inspectable")
		a, err := repo.SourcePostMedia.Association(ctx, f.receipt.PostUUID, later.Result.MediaUUID)
		require.NoError(t, err)
		require.True(t, a.Suppressed())
		gallery, err := repo.ArchiveEntity.Find(ctx, first.Result.GalleryUUID)
		require.NoError(t, err)
		members, err := repo.Gallery.GetImageIDs(ctx, *gallery.LocalID)
		require.NoError(t, err)
		require.Empty(t, members)
		return nil
	}))
}
