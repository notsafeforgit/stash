package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func linkAlbumPostMedia(t *testing.T, repo models.Repository, post string, kind models.ArchiveEntityKind, id int) *models.ArchiveEntity {
	t.Helper()
	media := archiveFind(t, repo, kind, id)
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post, media.UUID, "linked"))
	require.NoError(t, err)
	return media
}

func TestLinkedMediaAlbumGroupsMixedMediaWithoutInventingOrder(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "same-post"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "profile")
	text, date := "Original description", "2026-09-28"
	input.Metadata.OriginalText, input.Metadata.PublishedAt = &text, &date
	capture := recordSourceTestCapture(t, repo, input)
	image := linkAlbumPostMedia(t, repo, post.UUID, models.ArchiveImage, 41)
	scene := linkAlbumPostMedia(t, repo, post.UUID, models.ArchiveScene, 31)
	preview := albumBackfillPreview(t, repo, post.UUID, models.SourceAlbumLinkedMediaV1)
	require.Equal(t, "create", preview.Gallery.Action)
	require.Equal(t, preview, albumBackfillPreview(t, repo, post.UUID, preview.Policy))
	require.Len(t, preview.Gallery.Add, 2)
	require.Empty(t, preview.Gallery.Entries)
	require.Empty(t, preview.Matches)
	require.NotNil(t, preview.Recovery)
	require.False(t, preview.Recovery.Complete)
	require.Nil(t, preview.Recovery.ExpectedCount)
	result := applyAlbumBackfill(t, repo, preview)
	require.True(t, result.Gallery.Created)
	sourceGalleryMemberships(t, repo, *result.Gallery.GalleryID, []int{41}, []int{31})
	require.Equal(t, capture.UUID, *metadataState(t, repo, result.Gallery.GalleryUUID, "details").Decision.CaptureUUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		selection, err := repo.SourceAttachment.Selection(ctx, post.UUID)
		require.NoError(t, err)
		require.True(t, selection.DeclaredAlbum)
		require.False(t, selection.Complete)
		require.Nil(t, selection.ExpectedCount)
		require.Empty(t, selection.Entries)
		media, err := repo.SourceGallery.LibraryMedia(ctx, *result.Gallery.GalleryID, 0, 25)
		require.NoError(t, err)
		require.Equal(t, 2, media.Count)
		for _, item := range media.Items {
			require.Nil(t, item.SourcePosition)
		}
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_capture_attachment_manifests"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_attachments"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	replay := applyAlbumBackfill(t, repo, albumBackfillPreview(t, repo, post.UUID, preview.Policy))
	require.False(t, replay.Gallery.Created)
	require.Empty(t, replay.Gallery.Added)

	// A later real source list gives one member a position without erasing
	// the independently confirmed second member or replacing the gallery.
	newCapture := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{albumEntry(2, "native:reddit", "image-id", "image")}})
	selection := applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID,
		ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision, CaptureUUID: newCapture, Mode: "automatic", Origin: "ingest"})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	ordered := syncSourceGallery(t, repo, post.UUID)
	require.Equal(t, result.Gallery.GalleryUUID, ordered.GalleryUUID)
	sourceGalleryMemberships(t, repo, *result.Gallery.GalleryID, []int{41}, []int{31})

	// A post unlink leaves one item, so the automatic gallery disappears.
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, scene.UUID, "unlinked"))
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, result.Gallery.GalleryUUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, entity.State)
		return nil
	}))
	// Relinking the second item can recreate the album. A subsequent manual
	// membership exclusion is preserved instead of pruning the curated result.
	linkAlbumPostMedia(t, repo, post.UUID, models.ArchiveScene, 31)
	recreated := syncSourceGallery(t, repo, post.UUID)
	require.NotEqual(t, result.Gallery.GalleryUUID, recreated.GalleryUUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.RemoveImages(ctx, *recreated.GalleryID, *image.LocalID)
	}))
	require.Empty(t, syncSourceGallery(t, repo, post.UUID).Added)
	sourceGalleryMemberships(t, repo, *recreated.GalleryID, nil, []int{31})
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	backup := filepath.Join(t.TempDir(), "linked-album.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.AuditForTesting(backup))
}

func TestLinkedMediaAlbumRequiresDistinctConfirmedLinks(t *testing.T) {
	for _, scenario := range []string{"evidence only", "one link", "merged copies", "conflicting merge", "disabled", "capture without selection"} {
		t.Run(scenario, func(t *testing.T) {
			_, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, "")
			recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "profile"))
			one, two := archiveFind(t, repo, models.ArchiveScene, 31), archiveFind(t, repo, models.ArchiveScene, 32)
			want := "ineligible"
			if scenario == "evidence only" {
				for _, media := range []*models.ArchiveEntity{one, two} {
					recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID, Basis: "legacy", Details: []byte(`{}`)})
				}
			} else {
				linkAlbumPostMedia(t, repo, post.UUID, models.ArchiveScene, 31)
				if scenario != "one link" {
					state := "linked"
					if scenario == "conflicting merge" {
						state, want = "unlinked", "review"
					}
					_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, state))
					require.NoError(t, err)
				}
			}
			switch scenario {
			case "merged copies", "conflicting merge":
				require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
					if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
						return err
					}
					return repo.Scene.Destroy(ctx, 31)
				}))
			case "disabled":
				applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision, Mode: "disabled", Origin: "review"})
				want = "disabled"
			case "capture without selection":
				selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{albumEntry(0, "native:reddit", "known-order", "video")}})
				want = "review"
			}
			preview := albumBackfillPreview(t, repo, post.UUID, models.SourceAlbumLinkedMediaV1)
			require.Equal(t, want, preview.Gallery.Action)
			require.Nil(t, preview.Recovery)
		})
	}
}

func TestLinkedMediaAlbumDiscoveryAndChangedPreview(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, "")
	recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "profile"))
	linkAlbumPostMedia(t, repo, post.UUID, models.ArchiveImage, 41)
	scene := linkAlbumPostMedia(t, repo, post.UUID, models.ArchiveScene, 31)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := repo.SourceGallery.LinkedMediaBackfillPosts(ctx, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.SelectedSourcePost{{PostUUID: post.UUID, PostState: "active", Mode: "unselected"}}, rows)
		rows, err = repo.SourceGallery.LinkedMediaBackfillPosts(ctx, post.UUID, 1)
		require.NoError(t, err)
		require.Empty(t, rows)
		return nil
	}))
	preview := albumBackfillPreview(t, repo, post.UUID, models.SourceAlbumLinkedMediaV1)
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, scene.UUID, "unlinked"))
	require.NoError(t, err)
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Backfill(ctx, post.UUID, preview.Policy, preview.Signature)
		return err
	}), models.ErrSourceGalleryConflict)
}
