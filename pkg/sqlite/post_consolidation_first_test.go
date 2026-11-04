package sqlite

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestFirstPostConsolidationChoicesRetainEarlierMediaAndGallery(t *testing.T) {
	_, repo := postIdentityFixture(t)
	first := identityPost(t, repo, "legacy:catalog:fixture", "first")
	later := identityPost(t, repo, "native:reddit", "same-post")
	identityAlbum(t, repo, first)
	identityAlbum(t, repo, later)
	firstGallery := consolidationGalleryChoice(t, repo, first)
	consolidationGalleryChoice(t, repo, later)
	firstMedia, laterMedia := consolidationTestMedia(t, repo), consolidationTestMedia(t, repo)
	consolidationTestAttachmentChoice(t, repo, first, "linked", firstMedia)
	consolidationTestAttachmentChoice(t, repo, later, "linked", laterMedia)
	consolidationTestMediaChoice(t, repo, first, firstMedia, "linked")
	consolidationTestMediaChoice(t, repo, later, firstMedia, "unlinked")
	input := models.PostConsolidationReviewInput{SourceUUID: later, DestinationUUID: first, Reason: "Keep first media and metadata"}
	blocked := consolidationTestPlan(t, repo, input)
	require.False(t, blocked.preview.Ready)
	input = firstPostConsolidationChoices(blocked.snapshot, input)
	ready := consolidationTestPlan(t, repo, input)
	require.True(t, ready.preview.Ready, "%+v", ready.preview.Blockers)
	require.Equal(t, first, ready.selection.Decision.PostUUID)
	require.Equal(t, *firstGallery.GalleryUUID, ready.preview.Gallery.GalleryUUID)
	require.Equal(t, []models.PostConsolidationMediaChoice{{MediaUUID: firstMedia.UUID, State: "linked"}}, ready.preview.Media)
	for _, choice := range ready.preview.Attachments {
		if choice.Value == "second" {
			require.Equal(t, firstMedia.UUID, choice.MediaUUID)
		}
	}
	before := identityRows(t, repo, "scenes", "images")
	applyConsolidationReview(t, repo, consolidationReviewRequest(t, repo, input))
	require.Equal(t, before, identityRows(t, repo, "scenes", "images"), "consolidation leaves the retained media metadata intact")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		identity, err := repo.SourceEvidence.PostIdentity(ctx, later)
		require.NoError(t, err)
		require.Equal(t, first, identity.CanonicalUUID)
		return nil
	}))
}
