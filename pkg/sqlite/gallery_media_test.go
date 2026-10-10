package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func readGalleryMedia(t *testing.T, repo models.Repository, id, offset, limit int) *models.GalleryMediaReferences {
	t.Helper()
	var ret *models.GalleryMediaReferences
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.LibraryMedia(ctx, id, offset, limit)
		return err
	}))
	return ret
}

func TestGalleryMediaPagesMixedSourceOrderAndManualMembers(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, "INSERT INTO images(id, title, created_at, updated_at) VALUES (42, 'Manual image', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "mixed"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{
		sourceAttachmentEntry(0, "first"), sourceAttachmentEntry(2, "second"), sourceAttachmentEntry(4, "first"),
	}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveScene, 31)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveImage, 41)
	result := syncSourceGallery(t, repo, post.UUID)
	id := *result.GalleryID
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.AddImages(ctx, id, 42)
	}))
	first := readGalleryMedia(t, repo, id, 0, 1)
	require.Equal(t, 3, first.Count, "repeated source positions don't duplicate gallery membership")
	require.Equal(t, 1, *first.NextOffset)
	require.Equal(t, models.ArchiveScene, first.Items[0].Kind)
	require.Equal(t, 31, first.Items[0].LocalID)
	require.Equal(t, 0, *first.Items[0].SourcePosition)
	require.Equal(t, post.UUID, *first.Items[0].SourcePostUUID)
	second := readGalleryMedia(t, repo, id, 1, 2)
	require.Equal(t, first.Signature, second.Signature)
	require.Nil(t, second.NextOffset)
	require.Equal(t, 41, second.Items[0].LocalID)
	require.Equal(t, 2, *second.Items[0].SourcePosition, "source gaps aren't renumbered")
	require.Equal(t, 42, second.Items[1].LocalID)
	require.Nil(t, second.Items[1].SourcePosition)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.RemoveImages(ctx, id, 41)
	}))
	changed := readGalleryMedia(t, repo, id, 0, 100)
	require.Equal(t, 2, changed.Count)
	require.NotEqual(t, first.Signature, changed.Signature)
	require.Equal(t, 42, changed.Items[1].LocalID, "excluded source members stay out")
	// Changing attachment order without changing membership invalidates pages.
	selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{
		sourceAttachmentEntry(1, "first"), sourceAttachmentEntry(3, "second"),
	}})
	reordered := readGalleryMedia(t, repo, id, 0, 100)
	require.NotEqual(t, changed.Signature, reordered.Signature)
	require.Equal(t, 1, *reordered.Items[0].SourcePosition)
}

func TestGalleryMediaIncludesManualVideoOnlyAndEmptyGalleries(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	gallery := createArchiveGallery(t, repo, "Videos")
	require.Empty(t, readGalleryMedia(t, repo, gallery.ID, 0, 60).Items)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.AddSceneIDs(ctx, gallery.ID, []int{31, 32})
	}))
	page := readGalleryMedia(t, repo, gallery.ID, 0, 60)
	require.Equal(t, 2, page.Count)
	require.Len(t, page.Items, 2)
	for _, item := range page.Items {
		require.Equal(t, models.ArchiveScene, item.Kind)
		require.Nil(t, item.SourcePostUUID)
	}
	for _, input := range [][2]int{{-1, 1}, {0, 0}, {0, 101}} {
		require.Error(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.LibraryMedia(ctx, gallery.ID, input[0], input[1])
			return err
		}))
	}
}

func TestGalleryMediaOrdersSharedThreadByPostThenAttachment(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	last := threadPost(t, repo, "1001", "99", "100", "42", "42")
	threadMedia(t, repo, last, "last", models.ArchiveScene, 32)
	first := threadPost(t, repo, "99", "99", "", "42", "")
	threadMedia(t, repo, first, "first", models.ArchiveScene, 31)
	middle := threadPost(t, repo, "100", "99", "99", "42", "42")
	threadMedia(t, repo, middle, "middle", models.ArchiveImage, 41)
	gallery := syncSourceGallery(t, repo, middle)
	page := readGalleryMedia(t, repo, *gallery.GalleryID, 0, 60)
	require.Len(t, page.Items, 3)
	require.Equal(t, []int{31, 41, 32}, []int{page.Items[0].LocalID, page.Items[1].LocalID, page.Items[2].LocalID})
	require.Equal(t, first, *page.Items[0].SourcePostUUID)
	require.Equal(t, middle, *page.Items[1].SourcePostUUID)
	require.Equal(t, last, *page.Items[2].SourcePostUUID)
}
