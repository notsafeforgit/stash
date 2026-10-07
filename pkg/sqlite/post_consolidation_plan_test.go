package sqlite

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationTestPlan(t *testing.T, repo models.Repository, input models.PostConsolidationReviewInput) *postConsolidationPlan {
	t.Helper()
	var ret *postConsolidationPlan
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = preparePostConsolidationReview(ctx, input)
		return err
	}))
	return ret
}

func consolidationTestAttachmentChoice(t *testing.T, repo models.Repository, post, state string, media *models.ArchiveEntity) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		attachment, err := repo.SourceAttachment.Lookup(ctx, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"})
		if err != nil {
			return err
		}
		input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: state, Origin: "review"}
		if media != nil {
			input.MediaUUID, input.ExpectedMediaRevision = media.UUID, media.Revision
		}
		_, err = repo.SourceAttachment.DecideMedia(ctx, input)
		return err
	}))
}

func consolidationTestPlanBlockers(plan *postConsolidationPlan) []string {
	ret := []string{}
	for _, blocker := range plan.preview.Blockers {
		ret = append(ret, blocker.Kind)
	}
	return ret
}

func TestPostConsolidationPlanResolvesAllEarlierAliasChoicesReadOnly(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "legacy:catalog:fixture", "second")
	c := identityPost(t, repo, "legacy:catalog:fixture", "third")
	d := identityPost(t, repo, "native:reddit", "one-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, c)
	media := consolidationTestMedia(t, repo)
	consolidationTestAttachmentChoice(t, repo, a, "unlinked", nil)
	consolidationTestAttachmentChoice(t, repo, c, "linked", media)
	consolidationTestMediaChoice(t, repo, a, media, "unlinked")
	consolidationTestMediaChoice(t, repo, c, media, "linked")
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	publishPostIdentity(t, repo, identityRequest(t, repo, c, d))
	tables := []string{"source_posts", "source_post_identities", "source_captures", "post_attachment_decisions", "post_attachment_selections", "attachment_media_decisions", "attachment_media_links", "post_media_decisions", "post_media_links", "post_gallery_decisions", "post_gallery_links", "galleries", "scenes"}
	before := identityRows(t, repo, tables...)
	input := models.PostConsolidationReviewInput{SourceUUID: b, DestinationUUID: d, Reason: "Two retained copies of the same post"}
	blocked := consolidationTestPlan(t, repo, input)
	require.False(t, blocked.preview.Ready)
	require.ElementsMatch(t, []string{"source_list_choice", "gallery_choice", "media_choice", "attachment_choice"}, consolidationTestPlanBlockers(blocked))
	require.Len(t, blocked.preview.Posts, 4)
	for _, post := range blocked.preview.Posts {
		if post.UUID == c {
			input.Selection = &models.PostConsolidationSelectionChoice{Mode: "choose", DecisionUUID: post.Selection.DecisionUUID}
			input.Gallery = &models.PostConsolidationGalleryChoice{State: "linked", GalleryUUID: post.Album.Gallery.UUID}
		}
	}
	input.Media = []models.PostConsolidationMediaChoice{{MediaUUID: media.UUID, State: "linked"}}
	input.Attachments = []models.PostConsolidationAttachmentChoice{{Namespace: "native:reddit", Value: "second", State: "linked", MediaUUID: media.UUID}}
	ready := consolidationTestPlan(t, repo, input)
	require.True(t, ready.preview.Ready)
	require.Empty(t, ready.preview.Blockers)
	require.Equal(t, "pinned", ready.preview.Selection.Mode)
	require.Equal(t, c, ready.selection.Decision.PostUUID, "the original chosen capture and list keep their owner")
	require.Equal(t, input.Gallery, ready.preview.Gallery)
	require.Equal(t, input.Media, ready.preview.Media)
	require.Equal(t, input.Attachments, ready.preview.Attachments)
	require.Equal(t, before, identityRows(t, repo, tables...), "all preview reads must be side-effect free")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, ready.preview, consolidationTestPlan(t, repo, input).preview)
	input.Media[0].State = "unlinked"
	contradictory := consolidationTestPlan(t, repo, input)
	require.False(t, contradictory.preview.Ready)
	require.Equal(t, []string{"attachment_post_unlink"}, consolidationTestPlanBlockers(contradictory))
	input.Attachments[0] = models.PostConsolidationAttachmentChoice{Namespace: "native:reddit", Value: "second", State: "unlinked"}
	require.True(t, consolidationTestPlan(t, repo, input).preview.Ready)
	input.Media[0].State = "linked"
	input.Attachments[0] = ready.preview.Attachments[0]
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "UPDATE scenes SET title=? WHERE id=?", "Edited while review was open", *media.LocalID)
		return err
	}))
	require.NotEqual(t, ready.preview.Digest, consolidationTestPlan(t, repo, input).preview.Digest, "selected library revisions fence apply")
}

func TestPostConsolidationPlanCombinesAutomaticListsAndHonorsExplicitSelection(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	for _, post := range []string{a, b} {
		capture := identityAlbum(t, repo, post)
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			p, err := repo.SourceEvidence.FindPost(ctx, post)
			if err != nil {
				return err
			}
			_, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: p.Revision, CaptureUUID: capture.UUID, Mode: "automatic", Origin: "review"})
			return err
		}))
	}
	input := models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b, Gallery: &models.PostConsolidationGalleryChoice{State: "disabled"}}
	combined := consolidationTestPlan(t, repo, input)
	require.True(t, combined.preview.Ready)
	require.Equal(t, "automatic", combined.preview.Selection.Mode)
	require.Equal(t, 2, combined.preview.Selection.EntryCount, "matching qualified slots are shared")
	require.Len(t, combined.preview.Selection.ManifestUUIDs, 2)
	require.True(t, combined.preview.Selection.Complete)
	for _, post := range combined.preview.Posts {
		if post.UUID == b {
			require.Equal(t, post.Selection.CaptureUUID, combined.preview.Selection.CaptureUUID)
			input.Selection = &models.PostConsolidationSelectionChoice{Mode: "choose", DecisionUUID: post.Selection.DecisionUUID}
		}
	}
	chosen := consolidationTestPlan(t, repo, input)
	require.Len(t, chosen.preview.Selection.ManifestUUIDs, 1)
	require.NotEqual(t, combined.preview.Digest, chosen.preview.Digest)
	input.Selection.Mode = "combine"
	require.Len(t, consolidationTestPlan(t, repo, input).preview.Selection.ManifestUUIDs, 2)
	input.Selection = &models.PostConsolidationSelectionChoice{Mode: "disabled"}
	disabled := consolidationTestPlan(t, repo, input)
	require.True(t, disabled.preview.Ready)
	require.Equal(t, "disabled", disabled.preview.Selection.Mode)
	require.Empty(t, disabled.preview.Selection.ManifestUUIDs)
	require.Nil(t, disabled.preview.Selection.CaptureUUID)
}

func TestPostConsolidationPlanRejectsUnreviewedChoiceScope(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	input := models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b}
	empty := consolidationTestPlan(t, repo, input)
	require.True(t, empty.preview.Ready)
	require.Nil(t, empty.preview.Selection)
	require.Nil(t, empty.preview.Gallery)
	require.Empty(t, empty.preview.Media)
	require.Empty(t, empty.preview.Attachments)
	cases := []struct {
		name string
		edit func(*models.PostConsolidationReviewInput)
		err  error
	}{
		{"unknown list", func(i *models.PostConsolidationReviewInput) {
			i.Selection = &models.PostConsolidationSelectionChoice{Mode: "choose", DecisionUUID: uuid.NewString()}
		}, models.ErrAttachmentSelectionConflict},
		{"unknown gallery", func(i *models.PostConsolidationReviewInput) {
			i.Gallery = &models.PostConsolidationGalleryChoice{State: "linked", GalleryUUID: uuid.NewString()}
		}, models.ErrSourceGalleryConflict},
		{"unknown media", func(i *models.PostConsolidationReviewInput) {
			i.Media = []models.PostConsolidationMediaChoice{{MediaUUID: uuid.NewString(), State: "linked"}}
		}, models.ErrSourcePostMediaConflict},
		{"unknown attachment", func(i *models.PostConsolidationReviewInput) {
			i.Attachments = []models.PostConsolidationAttachmentChoice{{Namespace: "native:reddit", Value: "missing", State: "unlinked"}}
		}, models.ErrSourceAttachmentConflict},
		{"disabled list with capture", func(i *models.PostConsolidationReviewInput) {
			i.Selection = &models.PostConsolidationSelectionChoice{Mode: "disabled", DecisionUUID: uuid.NewString()}
		}, models.ErrPostConsolidationReviewInvalid},
		{"unlinked attachment with media", func(i *models.PostConsolidationReviewInput) {
			i.Attachments = []models.PostConsolidationAttachmentChoice{{Namespace: "native:reddit", Value: "missing", State: "unlinked", MediaUUID: uuid.NewString()}}
		}, models.ErrPostConsolidationReviewInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := input
			tc.edit(&request)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := preparePostConsolidationReview(ctx, request)
				require.ErrorIs(t, err, tc.err)
				return nil
			}))
		})
	}
}

func TestPostConsolidationPlanKeepsIncompatibleSourceListsForExplicitChoice(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	first := postSelectionCapture(t, repo, a, 0, 2)
	selected := postSelectionApply(t, repo, a, first.UUID, "pinned", "review")
	identityAlbum(t, repo, b)
	before := identityRows(t, repo, "source_captures", "source_attachment_manifests", "source_attachment_entries", "post_attachment_selections")
	input := models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b,
		Selection: &models.PostConsolidationSelectionChoice{Mode: "combine", DecisionUUID: selected.Decision.UUID}}
	blocked := consolidationTestPlan(t, repo, input)
	require.False(t, blocked.preview.Ready)
	require.Equal(t, []string{"source_list_conflict"}, consolidationTestPlanBlockers(blocked))
	input.Selection.Mode = "choose"
	chosen := consolidationTestPlan(t, repo, input)
	require.True(t, chosen.preview.Ready)
	require.Equal(t, first.UUID, *chosen.preview.Selection.CaptureUUID)
	require.Equal(t, selected.Entries, chosen.selection.Entries)
	require.Equal(t, before, identityRows(t, repo, "source_captures", "source_attachment_manifests", "source_attachment_entries", "post_attachment_selections"))
}
