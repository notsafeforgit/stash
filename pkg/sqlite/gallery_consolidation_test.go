package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationPreview(t *testing.T, repo models.Repository, id string) *models.GalleryConsolidationPreview {
	t.Helper()
	var ret *models.GalleryConsolidationPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.PreviewConsolidation(ctx, id)
		return err
	}))
	return ret
}

func consolidationApply(t *testing.T, repo models.Repository, id string, p *models.GalleryConsolidationPreview) *models.GalleryConsolidationResult {
	t.Helper()
	var ret *models.GalleryConsolidationResult
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.Consolidate(ctx, id, p.Signature)
		return err
	}))
	return ret
}

func consolidationGallery(t *testing.T, repo models.Repository, title string, images, scenes []int) *models.ArchiveEntity {
	t.Helper()
	g := models.NewGallery()
	g.Title = title
	g.Origin = models.GalleryOriginSource
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g}); err != nil {
			return err
		}
		if len(images) > 0 {
			if err := repo.Gallery.AddImages(ctx, g.ID, images...); err != nil {
				return err
			}
		}
		if len(scenes) > 0 {
			return repo.Gallery.AddSceneIDs(ctx, g.ID, scenes)
		}
		return nil
	}))
	return archiveFind(t, repo, models.ArchiveGallery, g.ID)
}

func TestGalleryConsolidationTransitiveMixedUnionAndRedirects(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a := consolidationGallery(t, repo, "First caption", []int{41}, []int{31})
	b := consolidationGallery(t, repo, "Another caption", nil, []int{31, 32})
	c := consolidationGallery(t, repo, "Another caption", nil, []int{32})
	manual := createArchiveGallery(t, repo, "Manual selection")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.AddImages(ctx, manual.ID, 41) }))
	p := consolidationPreview(t, repo, c.UUID)
	require.Len(t, p.Galleries, 3)
	require.Equal(t, a.UUID, p.GalleryUUID)
	require.Equal(t, 1, p.ImageCount)
	require.Equal(t, 2, p.SceneCount)
	result := consolidationApply(t, repo, c.UUID, p)
	require.Equal(t, a.UUID, result.GalleryUUID)
	require.Len(t, result.MergedUUIDs, 2)
	sourceGalleryMemberships(t, repo, *a.LocalID, []int{41}, []int{31, 32})
	sourceGalleryMemberships(t, repo, manual.ID, []int{41}, nil)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range []string{b.UUID, c.UUID} {
			current, err := repo.ArchiveEntity.Resolve(ctx, id)
			require.NoError(t, err)
			require.Equal(t, a.UUID, current.UUID)
		}
		titles, err := repo.SourceGallery.AlternateTitles(ctx, *a.LocalID)
		require.NoError(t, err)
		require.Equal(t, []string{"Another caption"}, titles)
		return nil
	}))
	require.Empty(t, consolidationApply(t, repo, c.UUID, consolidationPreview(t, repo, c.UUID)).MergedUUIDs)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_merge_context"))
}

func TestGalleryConsolidationRejectsStalePreviewAndCuratedConflicts(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := consolidationGallery(t, repo, "One", []int{41}, nil)
	b := consolidationGallery(t, repo, "Two", []int{41}, nil)
	p := consolidationPreview(t, repo, a.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Gallery.UpdatePartial(ctx, *b.LocalID, models.GalleryPartial{Code: models.NewOptionalString("changed")})
		return err
	}))
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Consolidate(ctx, a.UUID, p.Signature)
		return err
	}), models.ErrSourceGalleryConflict)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Gallery.UpdatePartial(ctx, *a.LocalID, models.GalleryPartial{Code: models.NewOptionalString("other")})
		return err
	}))
	p = consolidationPreview(t, repo, a.UUID)
	require.Equal(t, []string{"code"}, p.Conflicts)
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Consolidate(ctx, a.UUID, p.Signature)
		return err
	}), models.ErrSourceGalleryConflict)
	sourceGalleryMemberships(t, repo, *a.LocalID, []int{41}, nil)
	sourceGalleryMemberships(t, repo, *b.LocalID, []int{41}, nil)
}

func TestGalleryConsolidationReplacesInheritedPlaceholderWithCaption(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := consolidationGallery(t, repo, "Source album", []int{41}, nil)
	consolidationGallery(t, repo, "A useful caption", []int{41}, nil)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
			EntityUUID: a.UUID, ExpectedEntityRevision: a.Revision, Field: "title", Mode: "inherit", Origin: "migration", Value: []byte(`"Source album"`),
		})
		return err
	}))
	consolidationApply(t, repo, a.UUID, consolidationPreview(t, repo, a.UUID))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		g, err := repo.Gallery.Find(ctx, *a.LocalID)
		require.NoError(t, err)
		require.Equal(t, "A useful caption", g.Title)
		titles, err := repo.SourceGallery.AlternateTitles(ctx, *a.LocalID)
		require.NoError(t, err)
		require.Empty(t, titles, "a generated placeholder is not an alternate post title")
		return nil
	}))
}

func TestGalleryConsolidationNewImportsKeepPostsAndLateMedia(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	posts := []string{}
	var gallery *models.SourceGallerySyncResult
	var selections []*models.AttachmentSelection
	for range 2 {
		post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
		posts = append(posts, post.UUID)
		selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "video")}})
		selections = append(selections, selection)
		chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
		result := syncSourceGallery(t, repo, post.UUID)
		if gallery == nil {
			gallery = result
		} else {
			require.False(t, result.Created)
			require.Equal(t, gallery.GalleryUUID, result.GalleryUUID)
		}
	}
	chooseAlbumMedia(t, repo, selections[1].Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	result := syncSourceGallery(t, repo, posts[1])
	require.Equal(t, gallery.GalleryUUID, result.GalleryUUID)
	sourceGalleryMemberships(t, repo, *gallery.GalleryID, []int{41}, []int{31})
	// The second post contributed the current membership head. Removing its
	// image selection must leave the first post's image in the union.
	attachment := findAttachment(t, repo, selections[1].Entries[0].Attachment.UUID)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"}))
	syncSourceGallery(t, repo, posts[1])
	sourceGalleryMemberships(t, repo, *gallery.GalleryID, []int{41}, []int{31})
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := repo.SourceGallery.PostsForGallery(ctx, gallery.GalleryUUID, "", 100)
		require.NoError(t, err)
		require.Len(t, page.Posts, 2)
		for _, post := range posts {
			a, err := repo.SourceGallery.Association(ctx, post)
			require.NoError(t, err)
			require.Equal(t, gallery.GalleryUUID, *a.GalleryUUID)
		}
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_posts"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM galleries WHERE origin='source'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestGalleryConsolidationFailureCannotCommitPartialMerge(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a := consolidationGallery(t, repo, "One", []int{41}, nil)
	b := consolidationGallery(t, repo, "Two", []int{41}, []int{31})
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER fail_gallery_merge BEFORE DELETE ON galleries BEGIN SELECT RAISE(ABORT,'injected merge failure'); END`)
	require.NoError(t, err)
	p := consolidationPreview(t, repo, a.UUID)
	require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Consolidate(ctx, a.UUID, p.Signature)
		require.ErrorContains(t, err, "injected merge failure")
		return nil // The transaction guard must catch even a swallowed error.
	}))
	require.Equal(t, p, consolidationPreview(t, repo, a.UUID))
	sourceGalleryMemberships(t, repo, *a.LocalID, []int{41}, nil)
	sourceGalleryMemberships(t, repo, *b.LocalID, []int{41}, []int{31})
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM gallery_alternate_titles"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_merge_context"))
}

func TestGalleryConsolidationKeepsCoverAndManualExclusion(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := consolidationGallery(t, repo, "One", []int{41}, []int{32})
	b := consolidationGallery(t, repo, "Two", []int{41}, []int{31})
	cover := archiveFind(t, repo, models.ArchiveScene, 31)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Gallery.SetCover(ctx, *b.LocalID, cover.UUID); err != nil {
			return err
		}
		_, err := repo.Scene.UpdatePartial(ctx, 32, models.ScenePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*a.LocalID}}})
		return err
	}))
	consolidationApply(t, repo, b.UUID, consolidationPreview(t, repo, b.UUID))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		g, err := repo.Gallery.Find(ctx, *a.LocalID)
		require.NoError(t, err)
		require.Equal(t, &cover.UUID, g.CoverMediaUUID)
		history, err := repo.SourceGallery.MembershipHistory(ctx, a.UUID, 0, 100)
		require.NoError(t, err)
		var last *models.GalleryMembershipEvent
		for i := range history {
			if history[i].State == "excluded" {
				last = &history[i]
			}
		}
		require.NotNil(t, last)
		require.Equal(t, "library", last.Origin)
		return nil
	}))
}

func TestGalleryConsolidationThreadCanReceiveLaterReplyAfterRepostMerge(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	// Create an older repost album so the later thread will be absorbed by it.
	post, repost := singleSourceAlbum(t, repo)
	root := threadPost(t, repo, "100", "100", "", "99", "")
	threadMedia(t, repo, root, "500", models.ArchiveScene, 31)
	reply := threadPost(t, repo, "102", "100", "100", "99", "99")
	threadMedia(t, repo, reply, "502", models.ArchiveImage, 41)
	result := syncSourceGallery(t, repo, reply)
	require.Equal(t, repost.GalleryUUID, result.GalleryUUID)
	late := threadPost(t, repo, "103", "100", "102", "99", "99")
	threadMedia(t, repo, late, "503", models.ArchiveScene, 32)
	result = syncSourceGallery(t, repo, late)
	require.Equal(t, repost.GalleryUUID, result.GalleryUUID)
	sourceGalleryMemberships(t, repo, *repost.GalleryID, []int{41}, []int{31, 32})
	require.Equal(t, "sync", sourceGalleryPreview(t, repo, post).Action)
}
