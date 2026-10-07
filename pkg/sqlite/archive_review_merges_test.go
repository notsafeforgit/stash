package sqlite

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func internalArchiveQueue(t *testing.T, repo models.Repository) *models.ArchiveReviewPage {
	t.Helper()
	var page *models.ArchiveReviewPage
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		page, err = repo.ArchiveReview.Queue(ctx, models.ArchiveReviewFilter{Kind: "media", Limit: 25})
		return err
	}))
	return page
}

func TestArchiveReviewQueueCanonicalPostChoicesRetireOnlyAfterResolution(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "review-a")
	b := identityPost(t, repo, "legacy:catalog:fixture", "review-b")
	c := identityPost(t, repo, "native:reddit", "review-c")
	media := consolidationTestMedia(t, repo)
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, a, media.UUID, "linked", false), "")
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "unlinked", false), "")
	require.Empty(t, internalArchiveQueue(t, repo).Items)
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	page := internalArchiveQueue(t, repo)
	require.Len(t, page.Items, 1)
	require.Equal(t, b, page.Items[0].UUID)
	require.Equal(t, []string{"post_link_conflict"}, page.Items[0].Reasons)
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "unlinked", true), merge.UUID)
	require.Empty(t, internalArchiveQueue(t, repo).Items, "a retained merge record is not itself an unresolved review")
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, c, media.UUID, "linked", false), "")
	publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	page = internalArchiveQueue(t, repo)
	require.Len(t, page.Items, 1)
	require.Equal(t, c, page.Items[0].UUID, "successive redirects appear once under the current identity")
	before := identityRows(t, repo, "post_media_links", "post_media_decisions", "source_posts")
	require.Equal(t, page, internalArchiveQueue(t, repo))
	require.Equal(t, before, identityRows(t, repo, "post_media_links", "post_media_decisions", "source_posts"), "queue reads cannot settle or mutate decisions")
}

func TestArchiveReviewQueueMergedAttachmentRejectionsDoNotHideConflicts(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "attachment-a")
	b := identityPost(t, repo, "native:reddit", "attachment-b")
	postSelectionCapture(t, repo, a, 0)
	postSelectionCapture(t, repo, b, 0)
	media := consolidationTestMedia(t, repo)
	originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, a, "linked", media.UUID))
	originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, b, "unlinked", ""))
	require.Empty(t, internalArchiveQueue(t, repo).Items)
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	page := internalArchiveQueue(t, repo)
	require.Len(t, page.Items, 1)
	require.Equal(t, b, page.Items[0].UUID)
	require.Contains(t, page.Items[0].Reasons, "attachment_link_conflict")
	snapshot := attachmentConsolidationSnapshot(t, repo, b)
	input := attachmentConsolidationInput(t, repo, b, "unlinked", "")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, input)
		return err
	}))
	require.Empty(t, internalArchiveQueue(t, repo).Items)
}
