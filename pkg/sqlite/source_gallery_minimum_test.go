package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceGalleryLocalMinimumAndRecreation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "local-minimum"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "video")}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	waiting := syncSourceGallery(t, repo, post.UUID)
	require.Equal(t, "ineligible", waiting.Action)
	require.Nil(t, waiting.GalleryID)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	album := syncSourceGallery(t, repo, post.UUID)
	require.True(t, album.Created)
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, 31) }))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		g, err := repo.Gallery.Find(ctx, *album.GalleryID)
		require.NoError(t, err)
		require.Nil(t, g, "a media deletion prunes the now-small automatic gallery")
		a, err := repo.SourceGallery.Association(ctx, post.UUID)
		require.NoError(t, err)
		require.Nil(t, a, "automatic pruning does not permanently disable this post")
		image, err := repo.Image.Find(ctx, 41)
		require.NoError(t, err)
		require.NotNil(t, image)
		return nil
	}))
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 32)
	reborn := syncSourceGallery(t, repo, post.UUID)
	require.True(t, reborn.Created)
	require.NotEqual(t, album.GalleryUUID, reborn.GalleryUUID)
	sourceGalleryMemberships(t, repo, *reborn.GalleryID, []int{41}, []int{32})
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryRepeatedSlotsDoNotMeetLocalMinimum(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "repeated-slots"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "one"), sourceAttachmentEntry(1, "two")}})
	for _, e := range selection.Entries {
		chooseAlbumMedia(t, repo, e.Attachment.UUID, models.ArchiveImage, 41)
	}
	require.Equal(t, "ineligible", syncSourceGallery(t, repo, post.UUID).Action)
}

func TestSourceGalleryPruneChecksRevisionAndPreservesMedia(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "old-small-gallery"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "video")}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	album := syncSourceGallery(t, repo, post.UUID)
	old := archiveFind(t, repo, models.ArchiveGallery, *album.GalleryID)
	// A pre-upgrade database could retain a gallery after cascading deletion.
	attachmentSQL(t, db, "DELETE FROM scenes WHERE id=31; DELETE FROM source_gallery_performer_pending")
	current := archiveFind(t, repo, models.ArchiveGallery, *album.GalleryID)
	manual := createArchiveGallery(t, repo, "Single manual gallery")
	manualID := archiveFind(t, repo, models.ArchiveGallery, manual.ID)
	prune := func(inputs []models.SourceGalleryPruneInput) []models.SourceGalleryPruneResult {
		var results []models.SourceGalleryPruneResult
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			results, err = repo.SourceGallery.PruneSmall(ctx, inputs)
			return err
		}))
		return results
	}
	require.Equal(t, "changed", prune([]models.SourceGalleryPruneInput{{GalleryUUID: old.UUID, Revision: old.Revision}})[0].Status)
	results := prune([]models.SourceGalleryPruneInput{{GalleryUUID: current.UUID, Revision: current.Revision}, {GalleryUUID: manualID.UUID, Revision: manualID.Revision}})
	require.Equal(t, "removed", results[0].Status)
	require.Equal(t, "protected", results[1].Status)
	require.Equal(t, "missing", prune([]models.SourceGalleryPruneInput{{GalleryUUID: current.UUID, Revision: current.Revision}})[0].Status)
	require.Equal(t, "ineligible", syncSourceGallery(t, repo, post.UUID).Action)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM images WHERE id=41"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_posts"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_attachments"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	for _, inputs := range [][]models.SourceGalleryPruneInput{nil, {{GalleryUUID: uuid.NewString(), Revision: 0}}, {{GalleryUUID: "invalid", Revision: 1}}} {
		require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.PruneSmall(ctx, inputs)
			return err
		}))
	}
}
