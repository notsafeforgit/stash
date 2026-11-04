package api

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

func TestImageMergeNotifiesOnlyAfterCommitAndKeepsPhysicalFiles(t *testing.T) {
	f := newGalleryDeletionFixture(t)
	source, sourcePath := f.image(t, "source.jpg", false)
	destination, destinationPath := f.image(t, "kept.jpg", false)
	var from, to *models.ArchiveEntity
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		from, err = f.repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, source.ID)
		require.NoError(t, err)
		to, err = f.repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, destination.ID)
		return err
	}))
	input := ImageMergeInput{Source: strconv.Itoa(source.ID), Destination: strconv.Itoa(destination.ID),
		SourceRevision: from.Revision, DestinationRevision: to.Revision + 1}
	_, err := f.r.Mutation().ImageMerge(t.Context(), input)
	require.ErrorIs(t, err, models.ErrArchiveIdentityConflict)
	require.Empty(t, f.hooks.calls)
	f.hooks.check = func(ctx context.Context, call notificationCall) {
		require.NoError(t, f.repo.WithReadTxn(ctx, func(ctx context.Context) error {
			current, err := f.repo.ArchiveEntity.Resolve(ctx, from.UUID)
			require.NoError(t, err)
			require.Equal(t, to.UUID, current.UUID)
			removed, err := f.repo.Image.Find(ctx, source.ID)
			require.NoError(t, err)
			require.Nil(t, removed)
			return nil
		}))
	}
	input.DestinationRevision = to.Revision
	kept, err := f.r.Mutation().ImageMerge(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, destination.ID, kept.ID)
	require.Len(t, f.hooks.calls, 2)
	require.Equal(t, hook.ImageDestroyPost, f.hooks.calls[0].kind)
	removed := f.hooks.calls[0].input.(plugin.ImageDestroyInput)
	require.Nil(t, removed.DeleteFile)
	require.Nil(t, removed.DeleteGenerated)
	require.Equal(t, hook.ImageUpdatePost, f.hooks.calls[1].kind)
	require.Equal(t, []string{"files", "gallery_ids"}, f.hooks.calls[1].fields)
	for _, path := range []string{sourcePath, destinationPath} {
		_, err := os.Stat(path)
		require.NoError(t, err)
	}
}
