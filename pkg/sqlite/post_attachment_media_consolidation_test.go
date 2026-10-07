package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func attachmentConsolidationInput(t *testing.T, repo models.Repository, owner, state, media string) models.AttachmentMediaDecisionInput {
	t.Helper()
	input := models.AttachmentMediaDecisionInput{State: state, Origin: "review", Reason: "Reviewed shared post attachment"}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		a, err := repo.SourceAttachment.Lookup(ctx, owner, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"})
		require.NoError(t, err)
		input.AttachmentUUID, input.ExpectedAttachmentRevision = a.UUID, a.Revision
		if media != "" {
			m, err := repo.ArchiveEntity.Resolve(ctx, media)
			require.NoError(t, err)
			input.MediaUUID, input.ExpectedMediaRevision = m.UUID, m.Revision
		}
		return nil
	}))
	return input
}

func attachmentConsolidationSnapshot(t *testing.T, repo models.Repository, post string) *postAttachmentChoiceSnapshot {
	t.Helper()
	var ret *postAttachmentChoiceSnapshot
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = inspectPostAttachmentChoices(ctx, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"})
		return err
	}))
	return ret
}

func originalAttachmentChoice(t *testing.T, repo models.Repository, input models.AttachmentMediaDecisionInput) *models.AttachmentMediaDecision {
	t.Helper()
	var ret *models.AttachmentMediaDecision
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.DecideMedia(ctx, input)
		return err
	}))
	return ret
}

func TestPostAttachmentConsolidationRetainsOriginalEvidenceAndReviewReceiptAcrossMerges(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "legacy:catalog:fixture", "b")
	c := identityPost(t, repo, "native:reddit", "one-post")
	captures := map[string]models.SourceCaptureInput{}
	for _, post := range []string{a, b, c} {
		captures[post] = postSelectionCapture(t, repo, post, 0)
	}
	media := consolidationTestMedia(t, repo)
	firstInput := attachmentConsolidationInput(t, repo, a, "linked", media.UUID)
	var request models.AttachmentMediaReviewApplyInput
	var receipt *models.AttachmentMediaReview
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.SourceEvidence.FindPost(ctx, a)
		require.NoError(t, err)
		input := models.AttachmentMediaReviewInput{PostUUID: a, PostRevision: p.Revision, AttachmentUUID: firstInput.AttachmentUUID,
			AttachmentRevision: firstInput.ExpectedAttachmentRevision, State: "linked", MediaUUID: media.UUID, MediaRevision: media.Revision, Reason: "Original review"}
		preview, err := repo.SourceAttachment.PreviewMediaReview(ctx, input)
		require.NoError(t, err)
		request = models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
		receipt, _, err = repo.SourceAttachment.ApplyMediaReview(ctx, request)
		return err
	}))
	oldB := originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, b, "unlinked", ""))
	evidence := identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	snapshot := attachmentConsolidationSnapshot(t, repo, b)
	require.Len(t, snapshot.Members, 2)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.MediaDecision(ctx, firstInput.AttachmentUUID)
		require.ErrorIs(t, err, models.ErrAmbiguousSourceMedia, "conflicting current choices cannot silently select one original")
		return nil
	}))
	selectedInput := attachmentConsolidationInput(t, repo, b, "linked", media.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, selectedInput)
		return err
	}))
	applyConsolidationSelection(t, repo, b, captures[a].UUID, "pinned", merge.UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceGallery.Preview(ctx, b)
		require.NoError(t, err)
		require.Len(t, preview.Entries, 1)
		require.Equal(t, firstInput.AttachmentUUID, preview.Entries[0].AttachmentUUID, "selected source entries retain their original attachment")
		require.Equal(t, media.UUID, *preview.Entries[0].MediaUUID, "album membership follows the shared current choice")
		album, err := repo.SourceGallery.ReadAlbum(ctx, b, -1, 25)
		require.NoError(t, err)
		require.NotEmpty(t, album.Slots)
		require.Equal(t, media.UUID, album.Slots[0].Media.UUID)
		for _, id := range []string{firstInput.AttachmentUUID, oldB.AttachmentUUID} {
			current, err := repo.SourceAttachment.MediaReviewContext(ctx, id)
			require.NoError(t, err)
			require.Equal(t, id, current.RequestedAttachmentUUID)
			require.Equal(t, selectedInput.AttachmentUUID, current.Attachment.UUID)
			require.Equal(t, []string{"image"}, current.SourceMediaKinds, "source hints follow the shared reference in the selected original manifest")
		}
		return nil
	}))
	finalMerge := publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	snapshot = attachmentConsolidationSnapshot(t, repo, c)
	require.Len(t, snapshot.Members, 3)
	finalInput := attachmentConsolidationInput(t, repo, a, "unlinked", "")
	var selected *models.AttachmentMediaDecision
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		selected, err = publishConsolidatedAttachmentMedia(ctx, c, finalMerge.UUID, snapshot.Signature, finalInput)
		return err
	}))
	applyConsolidationSelection(t, repo, c, captures[a].UUID, "pinned", finalMerge.UUID)
	require.Equal(t, evidence, identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		actual, replayed, err := repo.SourceAttachment.ApplyMediaReview(ctx, request)
		require.NoError(t, err)
		require.True(t, replayed)
		require.Equal(t, receipt, actual)
		current, err := repo.SourceAttachment.MediaDecision(ctx, finalInput.AttachmentUUID)
		require.NoError(t, err)
		require.Equal(t, selected, current)
		old, err := repo.SourceAttachment.MediaDecision(ctx, oldB.AttachmentUUID)
		require.NoError(t, err)
		require.Equal(t, selected, old, "an original attachment resolves the shared current choice")
		history, err := repo.SourceAttachment.MediaDecisionHistory(ctx, oldB.AttachmentUUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 2)
		require.Equal(t, *oldB, history[0])
		var heads int
		require.NoError(t, dbWrapper.Get(ctx, &heads, "SELECT count(*) FROM attachment_media_links"))
		require.Equal(t, 1, heads)
		return nil
	}))
	// A new scrape still owns its original attachment, but the shared unlink
	// is visible before it can attempt to associate another downloaded file.
	scraped := attachmentConsolidationInput(t, repo, c, "linked", media.UUID)
	scraped.Origin = "ingest"
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceAttachment.DecideMedia(ctx, scraped); return err })
	require.ErrorIs(t, err, models.ErrSourceAttachmentConflict)
	// Reviewing through an older alias targets the current decision's owner;
	// the v1 saved request therefore keeps its original history-based digest.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceAttachment.MediaReviewContext(ctx, oldB.AttachmentUUID)
		require.NoError(t, err)
		require.Equal(t, oldB.AttachmentUUID, current.RequestedAttachmentUUID)
		require.Equal(t, selected.AttachmentUUID, current.Attachment.UUID)
		require.Equal(t, []string{"image"}, current.SourceMediaKinds)
		stale := models.AttachmentMediaReviewInput{PostUUID: current.PostUUID, PostRevision: current.PostRevision,
			AttachmentUUID: oldB.AttachmentUUID, AttachmentRevision: current.Attachment.Revision, State: "undecided"}
		_, err = repo.SourceAttachment.PreviewMediaReview(ctx, stale)
		require.ErrorIs(t, err, models.ErrSourceAssociationReviewConflict)
		input := models.AttachmentMediaReviewInput{PostUUID: current.PostUUID, PostRevision: current.PostRevision,
			AttachmentUUID: current.Attachment.UUID, AttachmentRevision: current.Attachment.Revision, State: "undecided", Reason: "Review through retained alias"}
		preview, err := repo.SourceAttachment.PreviewMediaReview(ctx, input)
		require.NoError(t, err)
		require.Equal(t, selected.AttachmentUUID, preview.Current.RequestedAttachmentUUID)
		review := models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
		saved, _, err := repo.SourceAttachment.ApplyMediaReview(ctx, review)
		require.NoError(t, err)
		recovered, replayed, err := repo.SourceAttachment.ApplyMediaReview(ctx, review)
		require.NoError(t, err)
		require.True(t, replayed)
		require.Equal(t, saved, recovered)
		return nil
	}))
}

func TestPostAttachmentConsolidationRejectsStaleLibraryAndUnrelatedScope(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	other := identityPost(t, repo, "native:reddit", "other-post")
	for _, post := range []string{a, b, other} {
		postSelectionCapture(t, repo, post, 0)
	}
	media := consolidationTestMedia(t, repo)
	originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, a, "linked", media.UUID))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	snapshot := attachmentConsolidationSnapshot(t, repo, b)
	outside := attachmentConsolidationInput(t, repo, other, "unlinked", "")
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, outside)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceAttachmentConflict)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "UPDATE scenes SET title='Changed competing media' WHERE id=?", *media.LocalID)
		return err
	}))
	input := attachmentConsolidationInput(t, repo, b, "unlinked", "")
	before := identityRows(t, repo, "source_posts", "source_attachments", "attachment_media_decisions", "attachment_media_links")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceAttachmentConflict)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "source_attachments", "attachment_media_decisions", "attachment_media_links"))
}

func TestPostAttachmentConsolidationCaughtLateFailureRollsBackIdentityAndHeads(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	for _, post := range []string{a, b} {
		postSelectionCapture(t, repo, post, 0)
		originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, post, "unlinked", ""))
	}
	identity := identityRequest(t, repo, a, b)
	input := attachmentConsolidationInput(t, repo, b, "undecided", "")
	tables := []string{"source_posts", "source_post_identities", "source_post_consolidations", "source_attachments", "attachment_media_links", "attachment_media_decisions"}
	before := identityRows(t, repo, tables...)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "CREATE TRIGGER fail_attachment_retirement BEFORE DELETE ON attachment_media_links BEGIN SELECT RAISE(ABORT,'late attachment retirement failure'); END")
		require.NoError(t, err)
		merge, err := publishPostConsolidationIdentity(ctx, identity)
		require.NoError(t, err)
		snapshot, err := inspectPostAttachmentChoices(ctx, b, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"})
		require.NoError(t, err)
		_, err = publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, input)
		require.ErrorContains(t, err, "late attachment retirement failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrSourceAttachmentConflict)
	require.Equal(t, before, identityRows(t, repo, tables...))
}

func TestPostAttachmentConsolidationCarriesDeletedChoiceWithoutResurrection(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	for _, post := range []string{a, b} {
		postSelectionCapture(t, repo, post, 0)
	}
	media := consolidationTestMedia(t, repo)
	originalAttachmentChoice(t, repo, attachmentConsolidationInput(t, repo, a, "linked", media.UUID))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, *media.LocalID) }))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	snapshot := attachmentConsolidationSnapshot(t, repo, b)
	input := attachmentConsolidationInput(t, repo, b, "linked", media.UUID)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceAttachment.DecideMedia(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrSourceAttachmentConflict)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, input)
		require.NoError(t, err)
		current, err := repo.ArchiveEntity.Find(ctx, media.UUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, current.State)
		require.Nil(t, current.LocalID)
		var count int
		require.NoError(t, dbWrapper.Get(ctx, &count, "SELECT count(*) FROM scenes"))
		require.Zero(t, count)
		return nil
	}))
}

func TestPostAttachmentConsolidationUsesBoundedCanonicalAndQualifiedReferenceIndexes(t *testing.T) {
	_, repo := postIdentityFixture(t)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var rows []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+postAttachmentChoicesQuery, "root", "native:reddit", "first", maxPostIdentityMembers+1))
		plan := fmt.Sprint(rows)
		require.Contains(t, plan, "source_post_identities_canonical (canonical_uuid=?)")
		require.Contains(t, plan, "(post_uuid=? AND namespace=? AND value=?)")
		require.NotContains(t, plan, "SCAN ")
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+currentAttachmentChoicesQuery, "attachment"))
		plan = fmt.Sprint(rows)
		require.Contains(t, plan, "source_post_identities_canonical (canonical_uuid=?)")
		require.Contains(t, plan, "(post_uuid=? AND namespace=? AND value=?)")
		require.NotContains(t, plan, "SCAN ")
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+currentAlbumAttachmentChoicesQuery+"(?,?) LIMIT ?", "first", "second", 3))
		plan = fmt.Sprint(rows)
		require.Contains(t, plan, "source_post_identities_canonical (canonical_uuid=?)")
		require.Contains(t, plan, "(post_uuid=? AND namespace=? AND value=?)")
		require.NotContains(t, plan, "SCAN ")
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+currentAttachmentMediaKindsQuery, "attachment"))
		plan = fmt.Sprint(rows)
		require.Contains(t, plan, "SEARCH h USING PRIMARY KEY (post_uuid=?)")
		require.NotContains(t, plan, "SCAN ")
		return nil
	}))
}
