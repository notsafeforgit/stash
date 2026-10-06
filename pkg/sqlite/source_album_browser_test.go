package sqlite_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func readSourceAlbum(t *testing.T, repo models.Repository, post string, after, limit int) *models.SourceAlbumPage {
	t.Helper()
	var ret *models.SourceAlbumPage
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.ReadAlbum(ctx, post, after, limit)
		return err
	}))
	return ret
}

func readGalleryPosts(t *testing.T, repo models.Repository, gallery, after string, limit int) *models.SourceGalleryPosts {
	t.Helper()
	var ret *models.SourceGalleryPosts
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.PostsForGallery(ctx, gallery, after, limit)
		return err
	}))
	return ret
}

func TestSourceAlbumBrowserKeepsMixedOrderRepeatedSlotsAndMissingRanges(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, "INSERT INTO scenes_files(scene_id,file_id,[primary]) VALUES(31,21,1)")
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "ordered"}, "")
	video := sourceAttachmentEntry(2, "video")
	video.MediaKind = "video"
	count := 1000000
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, ExpectedCount: &count,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), video, sourceAttachmentEntry(3, "image"), sourceAttachmentEntry(7, "unknown")}})
	image := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	scene := chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	before := readSourceAlbum(t, repo, post.UUID, -1, 100)
	require.Nil(t, before.Album, "reading does not create the eligible gallery")
	require.Len(t, before.Slots, 7, "large missing ranges are compact")
	require.False(t, before.Selection.Complete)
	require.Equal(t, 4, before.Selection.EntryCount)
	require.Equal(t, count, *before.Selection.ExpectedCount)
	require.Equal(t, []int{0, 1, 2, 3, 4, 7, 8}, albumSlotPositions(before.Slots))
	require.Equal(t, image.UUID, before.Slots[0].Media.UUID)
	require.Equal(t, scene.UUID, before.Slots[2].Media.UUID)
	require.Equal(t, 1, before.Slots[2].RegisteredFiles)
	require.Equal(t, image.UUID, before.Slots[3].Media.UUID)
	require.Equal(t, "video", before.Slots[2].MediaKind)
	require.Nil(t, before.Slots[1].Attachment)
	require.Equal(t, 6, before.Slots[4].Through)
	require.Equal(t, count-1, before.Slots[6].Through)
	require.Equal(t, "unselected", before.Slots[5].SelectionState)
	require.Zero(t, before.Slots[5].RegisteredFiles)
	require.Equal(t, before, readSourceAlbum(t, repo, post.UUID, -1, 100))
	var paged []models.SourceAlbumSlot
	after := -1
	for {
		page := readSourceAlbum(t, repo, post.UUID, after, 2)
		require.Equal(t, before.Signature, page.Signature)
		paged = append(paged, page.Slots...)
		if page.NextAfter == nil {
			break
		}
		after = *page.NextAfter
	}
	require.Equal(t, before.Slots, paged)
	insideGap := readSourceAlbum(t, repo, post.UUID, 5, 1)
	require.Equal(t, 6, insideGap.Slots[0].Position)
	require.Equal(t, 6, insideGap.Slots[0].Through)
	require.Empty(t, readSourceAlbum(t, repo, post.UUID, count-1, 25).Slots)
	result := syncSourceGallery(t, repo, post.UUID)
	afterSync := readSourceAlbum(t, repo, post.UUID, -1, 100)
	require.NotEqual(t, before.Signature, afterSync.Signature)
	for _, i := range []int{0, 2, 3} {
		require.Equal(t, "included", afterSync.Slots[i].GalleryMembership)
	}
	require.Equal(t, result.GalleryUUID, afterSync.Album.Gallery.UUID)
	encoded, err := json.Marshal(afterSync)
	require.NoError(t, err)
	for _, key := range []string{"payload", "settings", "original_text", "profile"} {
		require.NotContains(t, string(encoded), key)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, afterSync, readSourceAlbum(t, db.Repository(), post.UUID, -1, 100))
}

func albumSlotPositions(slots []models.SourceAlbumSlot) []int {
	ret := make([]int, 0, len(slots))
	for _, slot := range slots {
		ret = append(ret, slot.Position)
	}
	return ret
}

func TestSourceAlbumBrowserSeparatesSelectionPostChoicesAndManualMembership(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "choices"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "video")}})
	image := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	video := chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	result := syncSourceGallery(t, repo, post.UUID)
	initial := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Image.UpdatePartial(ctx, 41, models.ImagePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*result.GalleryID}}})
		if err != nil {
			return err
		}
		return repo.Gallery.AddSceneIDs(ctx, *result.GalleryID, []int{31})
	}))
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, video.UUID, "unlinked"))
	require.NoError(t, err)
	page := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NotEqual(t, initial.Signature, page.Signature)
	require.Equal(t, image.UUID, page.Slots[0].Media.UUID)
	require.Equal(t, "excluded", page.Slots[0].GalleryMembership)
	require.Equal(t, "linked", page.Slots[0].SelectionState)
	require.Equal(t, "undecided", page.Slots[0].PostLinkState)
	require.Equal(t, "linked", page.Slots[1].SelectionState)
	require.Equal(t, "unlinked", page.Slots[1].PostLinkState)
	require.Equal(t, "included", page.Slots[1].GalleryMembership, "an explicit post unlink cannot erase manual membership")
	attachment := findAttachment(t, repo, selection.Entries[0].Attachment.UUID)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
		ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"}))
	unlinked := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NotEqual(t, page.Signature, unlinked.Signature)
	require.Equal(t, "unlinked", unlinked.Slots[0].SelectionState)
	require.NotNil(t, unlinked.Slots[0].DecisionUUID)
	require.Nil(t, unlinked.Slots[0].Media)
}

func TestSourceAlbumBrowserRetainsDisabledDeletedAndForgottenEvidence(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "retained"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "scene")}})
	media := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveScene, 31)
	result := syncSourceGallery(t, repo, post.UUID)
	before := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.Destroy(ctx, 31); err != nil {
			return err
		}
		return repo.Gallery.Destroy(ctx, *result.GalleryID)
	}))
	attachmentSQL(t, db, `INSERT INTO scenes(id,created_at,updated_at) VALUES(31,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	deleted := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NotEqual(t, before.Signature, deleted.Signature)
	require.Equal(t, models.ArchiveEntityDeleted, deleted.Album.Gallery.State)
	require.Equal(t, media.UUID, deleted.Slots[0].Media.UUID)
	require.Nil(t, deleted.Slots[0].Media.LocalID)
	require.Zero(t, deleted.Slots[0].RegisteredFiles)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceGallery.DecideAssociation(ctx, models.SourceGalleryChoiceInput{PostUUID: post.UUID,
			ExpectedPostRevision: current.Revision, State: "disabled", Origin: "review", Reason: "Keep source evidence only"})
		return err
	}))
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", post.UUID)
	disabled := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.Equal(t, "forgotten", disabled.PostState)
	require.Equal(t, "disabled", disabled.Album.State)
	require.Nil(t, disabled.Album.Gallery)
	require.Len(t, disabled.Slots, 1)
	require.Equal(t, before.Selection, disabled.Selection)
	require.Equal(t, "no_gallery", disabled.Slots[0].GalleryMembership)
	require.Equal(t, disabled, readSourceAlbum(t, repo, post.UUID, -1, 25))
}

func TestSourceAlbumBrowserFollowsMediaAndGalleryRedirectsWithoutCombiningPosts(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "first"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "scene")}})
	one := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveScene, 31)
	two := archiveFind(t, repo, models.ArchiveScene, 32)
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, one.UUID, "linked"))
	require.NoError(t, err)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, "unlinked"))
	require.NoError(t, err)
	first := syncSourceGallery(t, repo, post.UUID)
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"}, "")
	selectAlbum(t, repo, other.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true})
	second := syncSourceGallery(t, repo, other.UUID)
	before := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		if err := repo.Scene.Destroy(ctx, 31); err != nil {
			return err
		}
		gallery, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, *first.GalleryID)
		if err != nil {
			return err
		}
		if err := repo.ArchiveEntity.Redirect(ctx, gallery.UUID, second.GalleryUUID, gallery.Revision); err != nil {
			return err
		}
		return repo.Gallery.Destroy(ctx, *first.GalleryID)
	}))
	page := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.NotEqual(t, before.Signature, page.Signature)
	require.Equal(t, two.UUID, page.Slots[0].Media.UUID)
	require.Equal(t, "conflict", page.Slots[0].PostLinkState)
	require.Equal(t, first.GalleryUUID, *page.Album.GalleryUUID)
	require.Equal(t, second.GalleryUUID, page.Album.Gallery.UUID)
	expected := []string{post.UUID, other.UUID}
	slices.Sort(expected)
	for _, id := range []string{first.GalleryUUID, second.GalleryUUID} {
		firstPage := readGalleryPosts(t, repo, id, "", 1)
		require.Equal(t, second.GalleryUUID, firstPage.Gallery.UUID)
		require.Equal(t, expected[0], firstPage.Posts[0].UUID)
		last := readGalleryPosts(t, repo, id, expected[0], 1)
		require.Equal(t, expected[1], last.Posts[0].UUID)
		require.Empty(t, readGalleryPosts(t, repo, id, expected[1], 1).Posts)
	}
}

func TestSourceAlbumBrowserValidatesBoundsAndEmptyStates(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "empty"}, "")
	page := readSourceAlbum(t, repo, post.UUID, -1, 25)
	require.Nil(t, page.Selection)
	require.Nil(t, page.Album)
	require.Empty(t, page.Slots)
	require.NotEmpty(t, page.Signature)
	require.Nil(t, readSourceAlbum(t, repo, uuid.NewString(), -1, 25))
	for _, tc := range []struct{ after, limit int }{{-2, 25}, {1000000, 25}, {-1, 0}, {-1, 101}} {
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.ReadAlbum(ctx, post.UUID, tc.after, tc.limit)
			require.ErrorIs(t, err, models.ErrSourcePostBrowseInvalid)
			return nil
		}))
	}
	gallery := createArchiveGallery(t, repo, "Manual")
	identity := archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
	require.Empty(t, readGalleryPosts(t, repo, identity.UUID, "", 25).Posts)
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.PostsForGallery(ctx, performer.UUID, "", 25)
		require.ErrorIs(t, err, models.ErrSourcePostBrowseInvalid)
		_, err = repo.SourceGallery.PostsForGallery(ctx, identity.UUID, "bad", 25)
		require.ErrorIs(t, err, models.ErrSourcePostBrowseInvalid)
		return nil
	}))
}
