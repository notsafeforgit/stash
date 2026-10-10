package sqlite_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func archiveQueue(t *testing.T, repo models.Repository, kind, after string, limit int) *models.ArchiveReviewPage {
	t.Helper()
	var page *models.ArchiveReviewPage
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		page, err = repo.ArchiveReview.Queue(ctx, models.ArchiveReviewFilter{Kind: kind, After: after, Limit: limit})
		return err
	}))
	return page
}

func TestArchiveReviewQueueAccountsPreserveUnlinksAndDistinctAmbiguousAccounts(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	one, two := createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit")
	for _, account := range []*models.SourceAccount{one, two} {
		observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "ambiguous"}, accountEvidence())
		putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Subscribed account", Kind: "account", Namespace: account.Namespace, State: "active", AccountUUID: &account.UUID}})
	}
	first := archiveQueue(t, repo, "accounts", "", 1)
	require.Len(t, first.Items, 1)
	require.Equal(t, first.Items[0].UUID, first.Next)
	last := archiveQueue(t, repo, "accounts", first.Next, 1)
	require.Len(t, last.Items, 1)
	require.Empty(t, last.Next)
	require.NotEqual(t, first.Items[0].UUID, last.Items[0].UUID)
	_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, one.UUID, 71)))
	require.NoError(t, err)
	unlink := accountReviewInput(t, repo, two.UUID, 71)
	unlink.State, unlink.PerformerUUID, unlink.PerformerRevision = models.AccountOwnershipUnlinked, "", 0
	_, _, err = applyAccountReview(repo, accountReviewRequest(t, repo, unlink))
	require.NoError(t, err)
	require.Empty(t, archiveQueue(t, repo, "accounts", "", 25).Items)
}

func TestArchiveReviewQueueMediaReflectsEvidenceAndCurrentMergeChoices(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "queued"}, "")
	one, two := archiveFind(t, repo, models.ArchiveScene, 31), archiveFind(t, repo, models.ArchiveScene, 32)
	for range 3 {
		recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: one.UUID, Basis: "legacy"})
	}
	page := archiveQueue(t, repo, "media", "", 25)
	require.Len(t, page.Items, 1)
	require.Equal(t, []string{"media_unselected"}, page.Items[0].Reasons)
	require.Equal(t, post.UUID, page.Items[0].Post.UUID)
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, one.UUID, "linked"))
	require.NoError(t, err)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, "unlinked"))
	require.NoError(t, err)
	require.Empty(t, archiveQueue(t, repo, "media", "", 25).Items)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 31)
	}))
	page = archiveQueue(t, repo, "media", "", 25)
	require.Len(t, page.Items, 1, "two raw settled choices become one unresolved canonical association")
	require.Equal(t, []string{"post_link_conflict"}, page.Items[0].Reasons)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, "unlinked"))
	require.NoError(t, err)
	require.Empty(t, archiveQueue(t, repo, "media", "", 25).Items, "explicit rejection settles the queue without destroying evidence")
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, "undecided"))
	require.NoError(t, err)
	require.Len(t, archiveQueue(t, repo, "media", "", 25).Items, 1)
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.Empty(t, archiveQueue(t, repo, "media", "", 25).Items)
}

func TestArchiveReviewQueueAttachmentDefaultAndExplicitUndecided(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	media := archiveFind(t, repo, models.ArchiveImage, 41)
	recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: capture.PostUUID, MediaUUID: media.UUID, Basis: "legacy"})
	current := findAttachment(t, repo, attachment.UUID)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID,
		ExpectedAttachmentRevision: current.Revision, State: "linked", MediaUUID: media.UUID, ExpectedMediaRevision: media.Revision, Origin: "review"}))
	require.Empty(t, archiveQueue(t, repo, "media", "", 25).Items, "an unambiguous attachment link supplies the default post association")
	current = findAttachment(t, repo, attachment.UUID)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID,
		ExpectedAttachmentRevision: current.Revision, State: "undecided", Origin: "review"}))
	page := archiveQueue(t, repo, "media", "", 25)
	require.Len(t, page.Items, 1)
	require.Contains(t, page.Items[0].Reasons, "attachment_link_undecided")
	_, err := applyPostMedia(repo, postMediaInput(t, repo, capture.PostUUID, media.UUID, "unlinked"))
	require.NoError(t, err)
	page = archiveQueue(t, repo, "media", "", 25)
	require.Equal(t, []string{"attachment_link_undecided"}, page.Items[0].Reasons, "the separate attachment decision still requires a choice")
	current = findAttachment(t, repo, attachment.UUID)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID,
		ExpectedAttachmentRevision: current.Revision, State: "unlinked", Origin: "review"}))
	require.Empty(t, archiveQueue(t, repo, "media", "", 25).Items)
}

func TestArchiveReviewQueueEmptyCandidatePageDoesNotClaimCompletion(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	media := archiveFind(t, repo, models.ArchiveImage, 41)
	ids := []string{}
	for i := range 101 {
		post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: fmt.Sprintf("queue-page-%d", i)}, "")
		ids = append(ids, post.UUID)
		recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID, Basis: "legacy"})
	}
	slices.Sort(ids)
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten' WHERE uuid!=?", ids[100])
	first := archiveQueue(t, repo, "media", "", 25)
	require.Empty(t, first.Items)
	require.Equal(t, 100, first.Checked)
	require.Equal(t, ids[99], first.Next)
	last := archiveQueue(t, repo, "media", first.Next, 25)
	require.Len(t, last.Items, 1)
	require.Equal(t, ids[100], last.Items[0].UUID)
	require.Empty(t, last.Next)
}

func TestArchiveReviewQueueMetadataTracksEachRetainedFieldAndOriginalReceipt(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Retained title","details":"Retained detail"}`)
	page := archiveQueue(t, f.repo, "metadata", "", 25)
	require.Len(t, page.Items, 1)
	require.Equal(t, input.EntityUUID, page.Items[0].UUID)
	require.Equal(t, models.ArchiveScene, page.Items[0].Media.Kind)
	for _, field := range []string{"title", "details"} {
		input.SourceField = field
		preview := previewFileEdit(t, f.repo, input)
		_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
		require.NoError(t, err)
		if field == "title" {
			require.Len(t, archiveQueue(t, f.repo, "metadata", "", 25).Items, 1, "reviewing one field cannot settle another")
		}
	}
	attachmentSQL(t, f.db, "UPDATE scenes SET title='A later user choice' WHERE id=31")
	require.Empty(t, archiveQueue(t, f.repo, "metadata", "", 25).Items, "a later edit does not require re-importing old values")
}

func TestArchiveReviewQueueRejectsInvalidPagesAndCanceledReads(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	for _, input := range []models.ArchiveReviewFilter{
		{Kind: "unknown", Limit: 25}, {Kind: "media", Limit: 0}, {Kind: "accounts", Limit: 51},
		{Kind: "metadata", Limit: 25, After: "bad"}, {Kind: "media", Limit: 25, After: uuid.Nil.String()},
	} {
		err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.ArchiveReview.Queue(ctx, input)
			return err
		})
		require.ErrorIs(t, err, models.ErrArchiveReviewInvalid)
	}
	err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.ArchiveReview.Queue(canceled, models.ArchiveReviewFilter{Kind: "media", Limit: 25})
		return err
	})
	require.ErrorIs(t, err, context.Canceled)
}
