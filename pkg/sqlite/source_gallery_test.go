package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func selectAlbum(t *testing.T, repo models.Repository, post string, manifest models.SourceAttachmentManifestInput) *models.AttachmentSelection {
	t.Helper()
	capture := selectionCapture(t, repo, post, manifest)
	return applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: selectionPost(t, repo, post).Revision,
		CaptureUUID: capture, Mode: "pinned", Origin: "review"})
}

func sourceGalleryPreview(t *testing.T, repo models.Repository, post string) *models.SourceGalleryPreview {
	t.Helper()
	var ret *models.SourceGalleryPreview
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.Preview(ctx, post)
		return err
	}))
	return ret
}

func syncSourceGallery(t *testing.T, repo models.Repository, post string) *models.SourceGallerySyncResult {
	t.Helper()
	preview := sourceGalleryPreview(t, repo, post)
	var ret *models.SourceGallerySyncResult
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.Sync(ctx, post, preview.Signature)
		return err
	}))
	return ret
}

func chooseAlbumMedia(t *testing.T, repo models.Repository, attachment string, kind models.ArchiveEntityKind, id int) *models.ArchiveEntity {
	t.Helper()
	media := archiveFind(t, repo, kind, id)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{
		AttachmentUUID: attachment, ExpectedAttachmentRevision: findAttachment(t, repo, attachment).Revision,
		State: "linked", MediaUUID: media.UUID, ExpectedMediaRevision: media.Revision, Origin: "review",
	}))
	return media
}

func sourceGalleryMemberships(t *testing.T, repo models.Repository, gallery int, images, scenes []int) {
	t.Helper()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		actualImages, err := repo.Gallery.GetImageIDs(ctx, gallery)
		require.NoError(t, err)
		require.ElementsMatch(t, images, actualImages)
		actualScenes, err := repo.Gallery.GetSceneIDs(ctx, gallery)
		require.NoError(t, err)
		require.ElementsMatch(t, scenes, actualScenes)
		return nil
	}))
}

func sourceGalleryHistory(t *testing.T, repo models.Repository, gallery string) []models.GalleryMembershipEvent {
	t.Helper()
	var ret []models.GalleryMembershipEvent
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.MembershipHistory(ctx, gallery, 0, 100)
		return err
	}))
	return ret
}

func TestSourceGalleryCreatesMixedAlbumSharesMediaAndSurvivesReplay(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "mixed-album"}, "")
	captureInput := sourceTestCapture(t, post.UUID, 1, "biography")
	details, date := "Post description", "2026-09-28"
	captureInput.Metadata.OriginalText, captureInput.Metadata.PublishedAt = &details, &date
	capture := recordSourceTestCapture(t, repo, captureInput)
	video := sourceAttachmentEntry(1, "video")
	video.MediaKind = "video"
	recordAttachmentManifest(t, repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), video, sourceAttachmentEntry(2, "image")}})
	selection := applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision,
		CaptureUUID: capture.UUID, Mode: "automatic", Origin: "ingest"})
	image := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	scene := chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	manual := createArchiveGallery(t, repo, "Shared title")
	preview := sourceGalleryPreview(t, repo, post.UUID)
	require.Equal(t, preview, sourceGalleryPreview(t, repo, post.UUID), "preview is read only")
	require.Equal(t, "create", preview.Action)
	require.Len(t, preview.Add, 2, "repeated attachment slots reuse one entity")
	require.Equal(t, []int{0, 1, 2}, []int{preview.Entries[0].Position, preview.Entries[1].Position, preview.Entries[2].Position})
	require.Equal(t, image.UUID, *preview.Entries[0].MediaUUID)
	require.Equal(t, scene.UUID, *preview.Entries[1].MediaUUID)
	require.Equal(t, image.UUID, *preview.Entries[2].MediaUUID)
	result := syncSourceGallery(t, repo, post.UUID)
	require.True(t, result.Created)
	require.NotEqual(t, manual.ID, *result.GalleryID, "matching titles never authorize adoption")
	sourceGalleryMemberships(t, repo, *result.GalleryID, []int{41}, []int{31})
	history := sourceGalleryHistory(t, repo, result.GalleryUUID)
	require.Len(t, history, 2)
	for _, event := range history {
		require.Equal(t, "source", event.Origin)
		require.Equal(t, "included", event.State)
		require.Equal(t, post.UUID, *event.PostUUID)
		require.Equal(t, selection.Decision.UUID, *event.SelectionUUID)
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		gallery, err := repo.Gallery.Find(ctx, *result.GalleryID)
		require.NoError(t, err)
		require.Equal(t, models.GalleryOriginSource, gallery.Origin)
		require.False(t, gallery.IsUserCreated())
		require.Equal(t, "Shared title", gallery.Title)
		require.Equal(t, details, gallery.Details)
		require.Equal(t, date, gallery.Date.String())
		require.Nil(t, gallery.FolderID)
		require.Nil(t, gallery.PrimaryFileID)
		found, err := repo.Gallery.FindUserGalleryByTitle(ctx, "Shared title")
		require.NoError(t, err)
		require.Len(t, found, 1)
		require.Equal(t, manual.ID, found[0].ID)
		association, err := repo.SourceGallery.Association(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, result.GalleryUUID, *association.GalleryUUID)
		require.Equal(t, selection.Decision.UUID, *association.SelectionUUID)
		return nil
	}))
	identity := archiveFind(t, repo, models.ArchiveGallery, *result.GalleryID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	replay := syncSourceGallery(t, repo, post.UUID)
	require.False(t, replay.Created)
	require.Equal(t, result.GalleryUUID, replay.GalleryUUID)
	require.Empty(t, replay.Added)
	require.Empty(t, replay.Removed)
	require.Equal(t, identity, archiveFind(t, repo, models.ArchiveGallery, *result.GalleryID))
	require.Equal(t, history, sourceGalleryHistory(t, repo, result.GalleryUUID))
	// Reposts may reference the same library records without copying media.
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "other-post"}, "")
	otherSelection := selectAlbum(t, repo, other.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image")}})
	chooseAlbumMedia(t, repo, otherSelection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	otherGallery := syncSourceGallery(t, repo, other.UUID)
	require.NotEqual(t, result.GalleryUUID, otherGallery.GalleryUUID)
	sourceGalleryMemberships(t, repo, *otherGallery.GalleryID, []int{41}, nil)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM images"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM scenes"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM files"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performers_galleries"), "publisher is not a depicted performer")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_write_context"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryEligibilityMissingMediaAndLateDownloads(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "partial-album"}, "")
	require.Equal(t, "ineligible", syncSourceGallery(t, repo, post.UUID).Action)
	manifest := models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "one")}}
	selection := selectAlbum(t, repo, post.UUID, manifest)
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	require.Equal(t, "ineligible", syncSourceGallery(t, repo, post.UUID).Action, "a normal single-media post needs no gallery")
	manifest.Complete, manifest.DeclaredAlbum = false, true
	manifest.Entries = append(manifest.Entries, sourceAttachmentEntry(2, "later"), sourceAttachmentEntry(3, "unlinked"))
	selection = selectAlbum(t, repo, post.UUID, manifest)
	unlinked := findAttachment(t, repo, selection.Entries[2].Attachment.UUID)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: unlinked.UUID, ExpectedAttachmentRevision: unlinked.Revision,
		State: "unlinked", Origin: "review"}))
	preview := sourceGalleryPreview(t, repo, post.UUID)
	require.Equal(t, "unselected", preview.Entries[1].Status)
	require.Equal(t, "unlinked", preview.Entries[2].Status)
	result := syncSourceGallery(t, repo, post.UUID)
	sourceGalleryMemberships(t, repo, *result.GalleryID, []int{41}, nil)
	stale := sourceGalleryPreview(t, repo, post.UUID)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	require.ErrorIs(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Sync(ctx, post.UUID, stale.Signature)
		return err
	}), models.ErrSourceGalleryConflict)
	late := syncSourceGallery(t, repo, post.UUID)
	require.Equal(t, result.GalleryUUID, late.GalleryUUID)
	require.Len(t, late.Added, 1)
	sourceGalleryMemberships(t, repo, *result.GalleryID, []int{41}, []int{31})
	emptyPost := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "declared-empty"}, "")
	selectAlbum(t, repo, emptyPost.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true})
	empty := syncSourceGallery(t, repo, emptyPost.UUID)
	require.True(t, empty.Created, "a declared album does not pretend its downloads are complete")
	sourceGalleryMemberships(t, repo, *empty.GalleryID, nil, nil)
}

func TestSourceGalleryPreservesManualMembershipMetadataAndCover(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "manual-decisions"}, "")
	manifest := models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "scene")}}
	selection := selectAlbum(t, repo, post.UUID, manifest)
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	result := syncSourceGallery(t, repo, post.UUID)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.Gallery.AddSceneIDs(ctx, *result.GalleryID, []int{31, 32}); err != nil {
			return err
		}
		if err := repo.Gallery.SetCover(ctx, *result.GalleryID, 41); err != nil {
			return err
		}
		_, err := repo.Gallery.UpdatePartial(ctx, *result.GalleryID, models.GalleryPartial{Title: models.NewOptionalString(""), Details: models.NewOptionalString("Manual description")})
		return err
	}))
	// A reviewed new source list may remove automatic members, but not the
	// manually reaffirmed scene, an extra manual scene, or the chosen cover.
	selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true})
	require.Empty(t, sourceGalleryPreview(t, repo, post.UUID).Remove)
	syncSourceGallery(t, repo, post.UUID)
	sourceGalleryMemberships(t, repo, *result.GalleryID, []int{41}, []int{31, 32})
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.ResetCover(ctx, *result.GalleryID) }))
	require.Len(t, syncSourceGallery(t, repo, post.UUID).Removed, 1, "a source-owned image becomes removable after its cover protection is cleared")
	sourceGalleryMemberships(t, repo, *result.GalleryID, nil, []int{31, 32})
	selectAlbum(t, repo, post.UUID, manifest)
	require.Len(t, syncSourceGallery(t, repo, post.UUID).Added, 1)
	// Image/scene-side edits must persist the same intent as gallery-side edits.
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Image.UpdatePartial(ctx, 41, models.ImagePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*result.GalleryID}}})
		if err != nil {
			return err
		}
		_, err = repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*result.GalleryID}}})
		return err
	}))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	preview := sourceGalleryPreview(t, repo, post.UUID)
	require.Equal(t, "excluded", preview.Entries[0].Status)
	require.Equal(t, "excluded", preview.Entries[1].Status)
	require.Empty(t, syncSourceGallery(t, repo, post.UUID).Added)
	sourceGalleryMemberships(t, repo, *result.GalleryID, nil, []int{32})
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		gallery, err := repo.Gallery.Find(ctx, *result.GalleryID)
		if err != nil {
			return err
		}
		require.Empty(t, gallery.Title)
		require.Equal(t, "Manual description", gallery.Details)
		gallery.Origin = models.GalleryOriginManual
		return repo.Gallery.Update(ctx, &models.UpdateGalleryInput{Gallery: gallery})
	}))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		gallery, err := repo.Gallery.Find(ctx, *result.GalleryID)
		require.NoError(t, err)
		require.Equal(t, models.GalleryOriginSource, gallery.Origin, "full updates cannot rewrite creation origin")
		return nil
	}))
	// Explicitly re-adding a previously excluded item replaces the manual choice.
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.AddImages(ctx, *result.GalleryID, 41) }))
	require.Equal(t, "linked", sourceGalleryPreview(t, repo, post.UUID).Entries[0].Status)
	selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true})
	require.Empty(t, syncSourceGallery(t, repo, post.UUID).Removed)
	sourceGalleryMemberships(t, repo, *result.GalleryID, []int{41}, []int{32})
}

func TestSourceGalleryDeletedMediaRemainsTombstoned(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: uuid.NewString()}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image")}})
	image := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	result := syncSourceGallery(t, repo, post.UUID)
	history := sourceGalleryHistory(t, repo, result.GalleryUUID)
	attachmentSQL(t, db, "DELETE FROM images WHERE id=41")
	require.Equal(t, history, sourceGalleryHistory(t, repo, result.GalleryUUID), "cascaded deletion is not a manual membership exclusion")
	attachmentSQL(t, db, "INSERT INTO images(id, title, created_at, updated_at) VALUES (41, 'Replacement', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	preview := sourceGalleryPreview(t, repo, post.UUID)
	require.Equal(t, "deleted", preview.Entries[0].Status)
	require.Equal(t, image.UUID, *preview.Entries[0].MediaUUID)
	require.Empty(t, syncSourceGallery(t, repo, post.UUID).Added)
	sourceGalleryMemberships(t, repo, *result.GalleryID, nil, nil)
}

func TestSourceGalleryPreviewRejectsInterveningLibraryEdits(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post, album := singleSourceAlbum(t, repo)
	for _, edit := range []func(context.Context) error{
		func(ctx context.Context) error {
			_, err := repo.Gallery.UpdatePartial(ctx, *album.GalleryID, models.GalleryPartial{Details: models.NewOptionalString("Changed after preview")})
			return err
		},
		func(ctx context.Context) error {
			_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{Title: models.NewOptionalString("Changed media after preview")})
			return err
		},
	} {
		preview := sourceGalleryPreview(t, repo, post)
		require.NoError(t, repo.WithTxn(context.Background(), edit))
		require.ErrorIs(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.Sync(ctx, post, preview.Signature)
			return err
		}), models.ErrSourceGalleryConflict)
	}
	applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: selectionPost(t, repo, post).Revision, Mode: "disabled", Origin: "review"})
	require.Equal(t, "disabled", syncSourceGallery(t, repo, post).Action)
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, []int{31})
}

func TestSourceGalleryEntitySideAddPreservesExistingManualIntent(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "entity-side-add"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "scene")}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	album := syncSourceGallery(t, repo, post.UUID)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.Gallery.SetCover(ctx, *album.GalleryID, 41); err != nil {
			return err
		}
		keep := &models.UpdateIDs{Mode: models.RelationshipUpdateModeAdd, IDs: []int{*album.GalleryID}}
		if _, err := repo.Image.UpdatePartial(ctx, 41, models.ImagePartial{GalleryIDs: keep}); err != nil {
			return err
		}
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{GalleryIDs: keep})
		return err
	}))
	history := sourceGalleryHistory(t, repo, album.GalleryUUID)
	require.Len(t, history, 4)
	for _, event := range history[2:] {
		require.Equal(t, "library", event.Origin)
		require.Equal(t, "included", event.State)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM galleries_images WHERE cover=1"), "reaffirmation must preserve the existing cover")
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.ResetCover(ctx, *album.GalleryID) }))
	selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true})
	require.Empty(t, syncSourceGallery(t, repo, post.UUID).Removed, "both explicit choices survive even after the source list and cover protection change")
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31})
	require.Equal(t, history, sourceGalleryHistory(t, repo, album.GalleryUUID), "automatic replay adds no membership events")
}
