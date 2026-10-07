package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationReviewRequest(t *testing.T, repo models.Repository, input models.PostConsolidationReviewInput) models.PostConsolidationReviewApplyInput {
	t.Helper()
	preview := consolidationTestPlan(t, repo, input).preview
	require.True(t, preview.Ready, "%+v", preview.Blockers)
	return models.PostConsolidationReviewApplyInput{PostConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
}

func applyConsolidationReview(t *testing.T, repo models.Repository, input models.PostConsolidationReviewApplyInput) (*models.PostConsolidationReview, bool) {
	t.Helper()
	var result *models.PostConsolidationReview
	var replayed bool
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = repo.SourceEvidence.ApplyConsolidationReview(ctx, input, time.Now().UTC())
		return err
	}))
	return result, replayed
}

func TestPostConsolidationApplyCreatesAlbumWithAtomicReceiptAndNotification(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
	evidence := identityRows(t, repo, "source_captures", "source_attachment_manifests", "source_attachment_entries")
	input := consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b, Reason: "Reviewed same source post"})
	first, replayed := applyConsolidationReview(t, repo, input)
	require.False(t, replayed)
	require.True(t, first.Result.Gallery.Created)
	require.NotEmpty(t, first.Result.NotificationJobUUID)
	require.Len(t, first.Result.Members, 2)
	require.NotEmpty(t, first.Result.SelectionUUID)
	require.NotEmpty(t, first.Result.GalleryDecisionUUID)
	require.Equal(t, b, identityRead(t, repo, a).CanonicalUUID)
	require.Equal(t, evidence, identityRows(t, repo, "source_captures", "source_attachment_manifests", "source_attachment_entries"))
	beforeReplay := identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_gallery_decisions", "galleries", "archive_jobs", "archive_job_submissions", "post_consolidation_reviews")
	second, replayed := applyConsolidationReview(t, repo, input)
	require.True(t, replayed)
	require.Equal(t, first, second)
	require.Equal(t, beforeReplay, identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_gallery_decisions", "galleries", "archive_jobs", "archive_job_submissions", "post_consolidation_reviews"))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		job, err := repo.ArchiveJob.Find(ctx, first.Result.NotificationJobUUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveJobNotifyPostMerge, job.Kind)
		require.Equal(t, "queued", job.State)
		return nil
	}))
	c := identityPost(t, repo, "legacy:catalog:fixture", "later-survivor")
	later := consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{SourceUUID: b, DestinationUUID: c, Reason: "Later merge"})
	applyConsolidationReview(t, repo, later)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	third, replayed := applyConsolidationReview(t, repo, input)
	require.True(t, replayed)
	require.Equal(t, first, third)
	input.Reason = "Changed saved request"
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := repo.SourceEvidence.ApplyConsolidationReview(ctx, input, time.Now().UTC())
		return err
	}), models.ErrSourcePostConsolidationReplay)
}

func TestPostConsolidationApplyNoEffectsAndStaleReviewLeaveNoPartialMerge(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	input := consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b})
	identityAlbum(t, repo, a)
	before := identityRows(t, repo, "source_posts", "source_post_identities", "source_post_consolidations", "galleries", "archive_jobs")
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := repo.SourceEvidence.ApplyConsolidationReview(ctx, input, time.Now().UTC())
		return err
	}), models.ErrPostConsolidationReviewConflict)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "source_post_identities", "source_post_consolidations", "galleries", "archive_jobs"))
	input = consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b,
		Selection: &models.PostConsolidationSelectionChoice{Mode: "disabled"}, Gallery: &models.PostConsolidationGalleryChoice{State: "disabled"}})
	result, _ := applyConsolidationReview(t, repo, input)
	require.Empty(t, result.Result.NotificationJobUUID)
	require.False(t, result.Result.Gallery.Changed())
	require.Equal(t, "disabled", result.Result.Gallery.Action)
}

func TestPostConsolidationApplyResolvesSourceGalleryPostAndAttachmentChoicesTogether(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, b)
	media := consolidationTestMedia(t, repo)
	consolidationTestAttachmentChoice(t, repo, a, "unlinked", nil)
	consolidationTestAttachmentChoice(t, repo, b, "linked", media)
	consolidationTestMediaChoice(t, repo, a, media, "unlinked")
	consolidationTestMediaChoice(t, repo, b, media, "linked")
	input := models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b, Reason: "Resolve all reviewed choices",
		Media:       []models.PostConsolidationMediaChoice{{MediaUUID: media.UUID, State: "linked"}},
		Attachments: []models.PostConsolidationAttachmentChoice{{Namespace: "native:reddit", Value: "second", State: "linked", MediaUUID: media.UUID}}}
	preview := consolidationTestPlan(t, repo, input)
	for _, post := range preview.preview.Posts {
		if post.UUID == a {
			input.Gallery = &models.PostConsolidationGalleryChoice{State: "linked", GalleryUUID: post.Album.Gallery.UUID}
		}
		if post.UUID == b {
			input.Selection = &models.PostConsolidationSelectionChoice{Mode: "choose", DecisionUUID: post.Selection.DecisionUUID}
		}
	}
	originals := identityRows(t, repo, "source_captures", "post_media_decisions", "attachment_media_decisions", "post_attachment_decisions", "post_gallery_decisions")
	request := consolidationReviewRequest(t, repo, input)
	result, replayed := applyConsolidationReview(t, repo, request)
	require.False(t, replayed)
	require.Len(t, result.Result.MediaDecisionUUIDs, 1)
	require.Len(t, result.Result.AttachmentDecisionUUIDs, 1)
	require.Equal(t, input.Gallery.GalleryUUID, result.Result.Gallery.GalleryUUID)
	require.Equal(t, []string{media.UUID}, result.Result.Gallery.Added)
	require.NotEmpty(t, result.Result.NotificationJobUUID)
	after := identityRows(t, repo, "source_captures", "post_media_decisions", "attachment_media_decisions", "post_attachment_decisions", "post_gallery_decisions")
	for table, rows := range originals {
		require.Subset(t, after[table], rows, table)
	}
	require.Equal(t, originals["source_captures"], after["source_captures"])
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		selected, err := consolidatedPostSelectionRows(ctx, b)
		require.NoError(t, err)
		require.Len(t, selected, 1)
		galleries, err := consolidatedPostGalleryHeads(ctx, b)
		require.NoError(t, err)
		require.Len(t, galleries, 1)
		for _, post := range []string{a, b} {
			association, err := repo.SourcePostMedia.Association(ctx, post, media.UUID)
			require.NoError(t, err)
			require.Equal(t, "linked", association.State)
			require.Len(t, association.Decisions, 1)
			attachment, err := repo.SourceAttachment.Lookup(ctx, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"})
			require.NoError(t, err)
			choice, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
			require.NoError(t, err)
			require.Equal(t, result.Result.AttachmentDecisionUUIDs[0], choice.UUID)
		}
		return nil
	}))
}

func TestPostConsolidationApplyCaughtLateFailureRollsBackEveryEffect(t *testing.T) {
	for _, table := range []string{"archive_job_submissions", "post_consolidation_reviews", "post_consolidation_review_members"} {
		t.Run(table, func(t *testing.T) {
			_, repo := postIdentityFixture(t)
			a := identityPost(t, repo, "legacy:catalog:fixture", "first")
			b := identityPost(t, repo, "native:reddit", "one-post")
			capture := postSelectionCapture(t, repo, a, 0, 1)
			postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
			input := consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b})
			tables := []string{"source_posts", "source_post_identities", "source_post_consolidations", "source_post_consolidation_context", "post_attachment_decisions", "post_attachment_selections", "post_gallery_decisions", "post_gallery_links", "galleries", "archive_entities", "metadata_field_decisions", "gallery_membership_events", "archive_jobs", "archive_job_submissions", "post_consolidation_reviews", "post_consolidation_review_members"}
			before := identityRows(t, repo, tables...)
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := dbWrapper.Exec(ctx, "CREATE TRIGGER fail_post_merge BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'injected late failure'); END")
				return err
			}))
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, _, failure := repo.SourceEvidence.ApplyConsolidationReview(ctx, input, time.Now().UTC())
				require.Error(t, failure)
				return nil // A caught error must not commit earlier merge/gallery writes.
			})
			require.Error(t, err)
			require.Equal(t, before, identityRows(t, repo, tables...))
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := dbWrapper.Exec(ctx, "DROP TRIGGER fail_post_merge")
				return err
			}))
			result, replayed := applyConsolidationReview(t, repo, input)
			require.False(t, replayed)
			require.True(t, result.Result.Gallery.Created)
		})
	}
}
