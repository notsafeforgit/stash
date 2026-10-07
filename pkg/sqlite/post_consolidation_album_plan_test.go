package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationTestGalleryPreview(t *testing.T, repo models.Repository, post string) *models.SourceGalleryPreview {
	t.Helper()
	var ret *models.SourceGalleryPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.Preview(ctx, post)
		return err
	}))
	return ret
}

func TestPostConsolidationPlanGalleryEffectsMatchPublishedChoicesAndProtectEdits(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, b)
	gallery := consolidationGalleryChoice(t, repo, a)
	sourceScene, manualScene := consolidationTestMedia(t, repo), consolidationTestMedia(t, repo)
	consolidationTestAttachmentChoice(t, repo, a, "linked", sourceScene)
	var galleryID int
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "INSERT INTO images(id,created_at,updated_at) VALUES(41,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
		require.NoError(t, err)
		cover, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, 41)
		require.NoError(t, err)
		attachment, err := repo.SourceAttachment.Lookup(ctx, a, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"})
		require.NoError(t, err)
		_, err = repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
			ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: cover.UUID, ExpectedMediaRevision: cover.Revision, Origin: "review"})
		require.NoError(t, err)
		preview, err := repo.SourceGallery.Preview(ctx, a)
		require.NoError(t, err)
		_, err = repo.SourceGallery.Sync(ctx, a, preview.Signature)
		require.NoError(t, err)
		galleryID = *preview.Gallery.LocalID
		require.NoError(t, repo.Gallery.AddSceneIDs(ctx, galleryID, []int{*manualScene.LocalID}))
		require.NoError(t, repo.Gallery.SetCover(ctx, galleryID, 41))
		_, err = repo.Gallery.UpdatePartial(ctx, galleryID, models.GalleryPartial{Title: models.NewOptionalString("My album"), Details: models.NewOptionalString("My description")})
		return err
	}))
	later := postSelectionCapture(t, repo, b, 2)
	selected := postSelectionApply(t, repo, b, later.UUID, "pinned", "review")
	input := models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b,
		Selection: &models.PostConsolidationSelectionChoice{Mode: "choose", DecisionUUID: selected.Decision.UUID},
		Gallery:   &models.PostConsolidationGalleryChoice{State: "linked", GalleryUUID: *gallery.GalleryUUID}}
	tables := []string{"galleries", "galleries_images", "scenes_galleries", "gallery_membership_events", "metadata_field_decisions"}
	before := identityRows(t, repo, tables...)
	plan := consolidationTestPlan(t, repo, input)
	require.True(t, plan.preview.Ready)
	require.Equal(t, "sync", plan.preview.Album.Action)
	require.Empty(t, plan.preview.Album.Title, "merging never reapplies source text to an edited gallery")
	require.Empty(t, plan.preview.Album.Details)
	require.Empty(t, plan.preview.Album.Add)
	require.Len(t, plan.preview.Album.Remove, 1)
	require.Equal(t, sourceScene.UUID, plan.preview.Album.Remove[0].UUID, "the cover and manual member remain protected")
	require.Equal(t, before, identityRows(t, repo, tables...))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	applyConsolidationSelection(t, repo, b, later.UUID, "pinned", merge.UUID)
	choice, expected := consolidationGalleryRequest(t, repo, b, *gallery.GalleryUUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedPostGallery(ctx, choice, merge.UUID, expected)
		return err
	}))
	actual := &postConsolidationPlan{preview: &models.PostConsolidationReviewPreview{}, selection: plan.selection}
	require.NoError(t, actual.setAlbumPlan(consolidationTestGalleryPreview(t, repo, b)))
	require.Equal(t, plan.preview.Album, actual.preview.Album, "a read-only proposed merge agrees with ordinary gallery preview after publishing its choices")
	require.Equal(t, before, identityRows(t, repo, tables...))
	result := consolidationGallerySync(t, repo, b)
	require.Equal(t, []string{sourceScene.UUID}, result.Removed)
	require.Empty(t, result.Added)
}

func TestPostConsolidationPlanCreatesAlbumFromOriginalCaptureWithoutMutating(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
	before := identityRows(t, repo, "source_posts", "source_captures", "galleries", "post_gallery_links")
	plan := consolidationTestPlan(t, repo, models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b})
	require.True(t, plan.preview.Ready)
	require.Equal(t, "create", plan.preview.Album.Action)
	require.Empty(t, plan.preview.Album.GalleryUUID)
	require.Len(t, plan.preview.Album.Entries, 2)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "source_captures", "galleries", "post_gallery_links"))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	applyConsolidationSelection(t, repo, b, capture.UUID, "pinned", merge.UUID)
	actual := &postConsolidationPlan{preview: &models.PostConsolidationReviewPreview{}, selection: plan.selection}
	require.NoError(t, actual.setAlbumPlan(consolidationTestGalleryPreview(t, repo, b)))
	require.Equal(t, plan.preview.Album, actual.preview.Album)
}

func TestPostConsolidationPlanProtectsOutsideGalleryClaimsAcrossRedirects(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	c := identityPost(t, repo, "native:reddit", "different-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, c)
	ga, gc := consolidationGalleryChoice(t, repo, a), consolidationGalleryChoice(t, repo, c)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		from, err := repo.ArchiveEntity.Find(ctx, *ga.GalleryUUID)
		require.NoError(t, err)
		if err := repo.ArchiveEntity.Redirect(ctx, from.UUID, *gc.GalleryUUID, from.Revision); err != nil {
			return err
		}
		return repo.Gallery.Destroy(ctx, *from.LocalID)
	}))
	input := models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b}
	plan := consolidationTestPlan(t, repo, input)
	require.False(t, plan.preview.Ready)
	require.Equal(t, []string{"gallery_claimed"}, consolidationTestPlanBlockers(plan))
	input.Gallery = &models.PostConsolidationGalleryChoice{State: "disabled"}
	require.True(t, consolidationTestPlan(t, repo, input).preview.Ready)
}

func TestPostConsolidationPlanGalleryMetadataAndClaimsUseBoundedIndexes(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		owner, metadata, err := sourceGalleryCaptureMetadata(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, a, owner)
		require.Equal(t, capture.Metadata, metadata)
		query, args := postConsolidationGalleryClaimQuery([]string{"gallery", "old-gallery"}, []string{"source", "destination", "earlier-source"})
		checks := []struct {
			query   string
			args    []any
			indexes []string
		}{
			{sourceGalleryCaptureMetadataQuery, []any{capture.UUID}, []string{"SEARCH c USING INDEX sqlite_autoindex_source_captures_1 (uuid=?)", "SEARCH r USING INDEX sqlite_autoindex_source_post_revisions_3 (post_uuid=? AND uuid=?)"}},
			{query, args, []string{"SEARCH post_gallery_links USING COVERING INDEX post_gallery_links_gallery (gallery_uuid=?)"}},
		}
		for _, check := range checks {
			var rows []struct {
				ID, Parent, Notused int
				Detail              string
			}
			require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+check.query, check.args...))
			plan := fmt.Sprint(rows)
			for _, index := range check.indexes {
				require.Contains(t, plan, index)
			}
			for _, table := range []string{"c", "r", "post_gallery_links"} {
				require.NotContains(t, plan, "SCAN "+table+" ")
			}
			require.NotContains(t, plan, "source_payloads")
			require.NotContains(t, plan, "source_profile_bodies")
		}
		return nil
	}))
}
