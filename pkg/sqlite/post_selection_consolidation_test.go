package sqlite

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationSelectionRequest(t *testing.T, repo models.Repository, post, capture, mode string) (models.AttachmentSelectionInput, []string, []string) {
	t.Helper()
	input := models.AttachmentSelectionInput{PostUUID: post, CaptureUUID: capture, Mode: mode, Origin: "review", Reason: "Resolve reviewed album order"}
	var expected, manifests []string
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.SourceEvidence.FindPost(ctx, post)
		require.NoError(t, err)
		input.ExpectedPostRevision = p.Revision
		heads, err := consolidatedPostSelectionRows(ctx, post)
		require.NoError(t, err)
		for _, head := range heads {
			expected = append(expected, head.UUID)
			if mode == "automatic" {
				ids, err := selectionManifestIDs(ctx, head)
				require.NoError(t, err)
				manifests = append(manifests, ids...)
			}
		}
		if mode != "disabled" {
			_, id, err := selectionCapture(ctx, post, capture)
			require.NoError(t, err)
			manifests = append(manifests, id)
		}
		return nil
	}))
	slices.Sort(manifests)
	return input, expected, slices.Compact(manifests)
}

func TestPostSelectionConsolidationCombinesListsPreservingOriginalReceiptAndHistory(t *testing.T) {
	db, repo := postIdentityFixture(t)
	postSelectionConsolidationQueryPlan(t, repo)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "legacy:catalog:fixture", "b")
	c := identityPost(t, repo, "native:reddit", "one-post")
	first := postSelectionCapture(t, repo, a, 0, 2)
	second := postSelectionCapture(t, repo, b, 1)
	var oldRequest models.AttachmentSelectionReviewApplyInput
	var oldReceipt *models.AttachmentSelectionReview
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.SourceEvidence.FindPost(ctx, a)
		require.NoError(t, err)
		preview, err := repo.SourceAttachment.PreviewSelectionReview(ctx, models.AttachmentSelectionReviewInput{PostUUID: a,
			PostRevision: p.Revision, CaptureUUID: first.UUID, Mode: "pinned", Reason: "Original reviewed order"})
		require.NoError(t, err)
		oldRequest = models.AttachmentSelectionReviewApplyInput{AttachmentSelectionReviewInput: preview.Input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
		oldReceipt, _, err = repo.SourceAttachment.ApplySelectionReview(ctx, oldRequest)
		return err
	}))
	oldSecond := postSelectionApply(t, repo, b, second.UUID, "automatic", "ingest")
	evidence := identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries", "attachment_selection_reviews")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	input, expected, manifests := consolidationSelectionRequest(t, repo, b, second.UUID, "automatic")
	require.Len(t, expected, 2)
	var selected *models.AttachmentSelection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		selected, err = publishConsolidatedPostSelection(ctx, input, merge.UUID, expected, manifests)
		require.NoError(t, err)
		require.Len(t, selected.Entries, 3)
		require.Equal(t, []string{a, b, a}, []string{selected.Entries[0].Attachment.PostUUID, selected.Entries[1].Attachment.PostUUID, selected.Entries[2].Attachment.PostUUID})
		require.Equal(t, second.UUID, *selected.Decision.CaptureUUID)
		heads, err := consolidatedPostSelectionRows(ctx, b)
		require.NoError(t, err)
		require.Len(t, heads, 1)
		require.Equal(t, selected.Decision.UUID, heads[0].UUID)
		original, err := (&SourceAttachmentStore{}).selectionForOriginal(ctx, a)
		require.NoError(t, err)
		require.Nil(t, original, "only the current pointer is retired")
		history, err := repo.SourceAttachment.SelectionHistory(ctx, b, 0, 100)
		require.NoError(t, err)
		require.Equal(t, []models.AttachmentSelectionDecision{oldSecond.Decision, selected.Decision}, history)
		return nil
	}))
	merge = publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	input, expected, manifests = consolidationSelectionRequest(t, repo, c, first.UUID, "pinned")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		pinned, err := publishConsolidatedPostSelection(ctx, input, merge.UUID, expected, manifests)
		require.NoError(t, err)
		require.Len(t, pinned.Entries, 2)
		require.Equal(t, a, pinned.Entries[0].Attachment.PostUUID)
		preview, err := repo.SourceAttachment.PreviewSelection(ctx, c, second.UUID)
		require.NoError(t, err)
		require.True(t, preview.Protected)
		return nil
	}))
	require.Equal(t, evidence, identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries", "attachment_selection_reviews"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replayed, replay, err := repo.SourceAttachment.ApplySelectionReview(ctx, oldRequest)
		require.NoError(t, err)
		require.True(t, replay)
		require.Equal(t, oldReceipt, replayed)
		history, err := repo.SourceAttachment.SelectionHistory(ctx, a, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 1)
		capture, err := repo.SourceEvidence.RecordCapture(ctx, first)
		require.NoError(t, err)
		require.Equal(t, a, capture.PostUUID)
		return nil
	}))
}

func TestPostSelectionConsolidationRequiresCompleteReviewAndScopedLists(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	other := identityPost(t, repo, "native:reddit", "another-post")
	first := postSelectionCapture(t, repo, a, 0)
	second := postSelectionCapture(t, repo, b, 1)
	unselected := postSelectionCapture(t, repo, b, 2)
	unrelated := postSelectionCapture(t, repo, other, 0, 1)
	postSelectionApply(t, repo, a, first.UUID, "automatic", "ingest")
	postSelectionApply(t, repo, b, second.UUID, "pinned", "review")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	input, expected, manifests := consolidationSelectionRequest(t, repo, b, first.UUID, "automatic")
	var unselectedManifest, unrelatedManifest string
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		_, unselectedManifest, err = selectionCapture(ctx, b, unselected.UUID)
		require.NoError(t, err)
		_, unrelatedManifest, err = selectionCapture(ctx, other, unrelated.UUID)
		return err
	}))
	before := identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections")
	for _, bad := range []string{"omitted choice", "duplicate choice", "stale post", "wrong merge", "alias target", "ingest", "invalid mode", "disabled capture", "disabled list", "pinned multiple lists", "missing primary", "duplicate list", "unselected list", "unrelated list", "unrelated capture"} {
		t.Run(bad, func(t *testing.T) {
			choice, proof, heads, lists := input, merge.UUID, slices.Clone(expected), slices.Clone(manifests)
			switch bad {
			case "omitted choice":
				heads = heads[:1]
			case "duplicate choice":
				heads = append(heads, heads[0])
			case "stale post":
				choice.ExpectedPostRevision--
			case "wrong merge":
				proof = uuid.NewString()
			case "alias target":
				choice.PostUUID = a
			case "ingest":
				choice.Origin = "ingest"
			case "invalid mode":
				choice.Mode = "unknown"
			case "disabled capture":
				choice.Mode, lists = "disabled", nil
			case "disabled list":
				choice.Mode, choice.CaptureUUID = "disabled", ""
			case "pinned multiple lists":
				choice.Mode = "pinned"
			case "missing primary":
				lists = nil
			case "duplicate list":
				lists = append(lists, lists[0])
			case "unselected list":
				lists = append(lists, unselectedManifest)
			case "unrelated list":
				lists = append(lists, unrelatedManifest)
			case "unrelated capture":
				choice.CaptureUUID = unrelated.UUID
			}
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := publishConsolidatedPostSelection(ctx, choice, proof, heads, lists)
				return err
			})
			require.ErrorIs(t, err, models.ErrAttachmentSelectionConflict)
			require.Equal(t, before, identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections"))
		})
	}
	conflicting := first
	conflicting.UUID, conflicting.PostUUID = uuid.NewString(), b
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, conflicting)
		require.NoError(t, err)
		_, err = repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: conflicting.UUID,
			Entries: []models.SourceAttachmentEntry{{Position: 0, MediaKind: "image", Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "different-first"}}}})
		return err
	}))
	postSelectionApply(t, repo, b, conflicting.UUID, "pinned", "review")
	input, expected, manifests = consolidationSelectionRequest(t, repo, b, first.UUID, "automatic")
	before = identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections")
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedPostSelection(ctx, input, merge.UUID, expected, manifests)
		return err
	})
	require.ErrorIs(t, err, models.ErrAttachmentManifestConflict)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections"))
	input, expected, manifests = consolidationSelectionRequest(t, repo, b, "", "disabled")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		selected, err := publishConsolidatedPostSelection(ctx, input, merge.UUID, expected, manifests)
		require.NoError(t, err)
		require.Equal(t, "disabled", selected.Decision.Mode)
		require.Empty(t, selected.Entries)
		preview, err := repo.SourceAttachment.PreviewSelection(ctx, b, second.UUID)
		require.NoError(t, err)
		require.True(t, preview.Protected)
		return nil
	}))
}

func TestPostSelectionConsolidationCaughtFailureRollsBackIdentityAndChoices(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	old := postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
	identity := identityRequest(t, repo, a, b)
	tables := []string{"source_posts", "source_post_identities", "source_post_consolidations", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections"}
	before := identityRows(t, repo, tables...)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "CREATE TRIGGER fail_consolidated_selection BEFORE INSERT ON post_attachment_selections BEGIN SELECT RAISE(ABORT,'late selection failure'); END")
		require.NoError(t, err)
		merge, err := publishPostConsolidationIdentity(ctx, identity)
		require.NoError(t, err)
		post, err := repo.SourceEvidence.FindPost(ctx, b)
		require.NoError(t, err)
		_, err = publishConsolidatedPostSelection(ctx, models.AttachmentSelectionInput{PostUUID: b, ExpectedPostRevision: post.Revision,
			CaptureUUID: capture.UUID, Mode: "pinned", Origin: "review"}, merge.UUID, []string{old.Decision.UUID}, old.Decision.ManifestUUIDs)
		require.ErrorContains(t, err, "late selection failure")
		return nil // Catching a late error must not commit any part of the merge.
	})
	require.ErrorIs(t, err, models.ErrAttachmentSelectionConflict)
	require.Equal(t, before, identityRows(t, repo, tables...))
}

func postSelectionConsolidationQueryPlan(t *testing.T, repo models.Repository) {
	t.Helper()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var rows []struct {
			ID      int    `db:"id"`
			Parent  int    `db:"parent"`
			NotUsed int    `db:"notused"`
			Detail  string `db:"detail"`
		}
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+consolidatedPostSelectionsQuery, uuid.NewString(), maxPostIdentityMembers+1))
		var details []string
		for _, row := range rows {
			details = append(details, row.Detail)
		}
		plan := strings.Join(details, "\n")
		require.Contains(t, plan, "source_post_identities_canonical")
		require.NotContains(t, plan, "SCAN s")
		require.NotContains(t, plan, "SCAN d")
		return nil
	}))
}
