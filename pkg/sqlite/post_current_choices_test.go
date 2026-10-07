package sqlite

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostCurrentChoicesSamplesPreserveOriginalCapturesAcrossSharedLinks(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	other := identityPost(t, repo, "native:reddit", "another-post")
	captures := map[string]models.SourceCaptureInput{}
	for _, post := range []string{a, b, other} {
		captures[post] = postSelectionCapture(t, repo, post, 0)
	}
	media := consolidationTestMedia(t, repo)
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Reviewed evidence", Kind: "collection", State: "active", Namespace: "native:reddit"}})
		require.NoError(t, err)
		for _, capture := range captures {
			require.NoError(t, repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{
				CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, CaptureUUID: capture.UUID}))
		}
		return nil
	}))
	scope := models.MetadataPolicySampleScope{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, EntityUUID: media.UUID}
	read := func(after *models.MetadataPolicySourceCursor, limit int) []models.MetadataPolicySampleSource {
		t.Helper()
		var ret []models.MetadataPolicySampleSource
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			ret, err = repo.MetadataPolicy.SampleSources(ctx, scope, after, limit)
			return err
		}))
		return ret
	}
	first := attachmentConsolidationInput(t, repo, a, "linked", media.UUID)
	originalAttachmentChoice(t, repo, first)
	second := attachmentConsolidationInput(t, repo, b, "unlinked", "")
	originalAttachmentChoice(t, repo, second)
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	require.Empty(t, read(nil, 25), "competing attachment heads cannot supply metadata, even when only one links this media")
	choice := attachmentConsolidationInput(t, repo, b, "linked", media.UUID)
	snapshot := attachmentConsolidationSnapshot(t, repo, b)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedAttachmentMedia(ctx, b, merge.UUID, snapshot.Signature, choice)
		return err
	}))
	rows := read(nil, 25)
	require.Len(t, rows, 2, "a shared choice exposes both original captures without including an unrelated post's same reference")
	originals := map[string]string{captures[a].UUID: first.AttachmentUUID, captures[b].UUID: second.AttachmentUUID}
	for _, row := range rows {
		require.Equal(t, originals[row.CaptureUUID], row.AttachmentUUID)
		require.Empty(t, row.PostMediaDecisionUUID)
	}
	page := read(nil, 1)
	require.Len(t, page, 1)
	next := read(&page[0].MetadataPolicySourceCursor, 1)
	require.Len(t, next, 1)
	require.Equal(t, rows, append(page, next...))
	require.Empty(t, read(&next[0].MetadataPolicySourceCursor, 1))

	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "unlinked", false), "")
	require.Empty(t, read(nil, 25), "canonical post rejection suppresses every original attachment")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceAttachment.MediaReviewContext(ctx, first.AttachmentUUID)
		require.NoError(t, err)
		require.Equal(t, "unlinked", current.PostLinkState)
		_, err = repo.SourceAttachment.PreviewMediaReview(ctx, models.AttachmentMediaReviewInput{
			PostUUID: current.PostUUID, PostRevision: current.PostRevision, AttachmentUUID: current.Attachment.UUID,
			AttachmentRevision: current.Attachment.Revision, State: "linked", MediaUUID: media.UUID, MediaRevision: media.Revision})
		require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
		return nil
	}))
	// Whole-post choices also expose historical captures without a manifest.
	withoutManifest := captures[a]
	withoutManifest.UUID = uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, withoutManifest)
		require.NoError(t, err)
		return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{
			CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, CaptureUUID: withoutManifest.UUID})
	}))
	decision := applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "linked", false), "")
	rows = read(nil, 25)
	require.Len(t, rows, 3)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, row := range rows {
			require.Contains(t, []string{a, b}, row.PostUUID)
			require.Empty(t, row.AttachmentUUID)
			require.Equal(t, decision.UUID, row.PostMediaDecisionUUID)
			require.NoError(t, repo.SourcePostMedia.ValidateCapture(ctx, decision.UUID, row.CaptureUUID, media.UUID))
		}
		return nil
	}))
}

func TestPostCurrentChoicesRequireCanonicalReviewedWritesAndPreserveRetry(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	media := consolidationTestMedia(t, repo)
	originalInput := consolidationMediaRequest(t, repo, a, media.UUID, "linked", false)
	original := applyConsolidationMedia(t, repo, originalInput, "")
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "unlinked", false), "")
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, alias := range []string{a, b} {
			current, err := repo.SourcePostMedia.Association(ctx, alias, media.UUID)
			require.NoError(t, err)
			require.Equal(t, b, current.PostUUID)
			require.Equal(t, "conflict", current.State)
			require.Len(t, current.Decisions, 2)
			states, err := sourcePostMediaStates(ctx, alias)
			require.NoError(t, err)
			require.Equal(t, "conflict", states[media.UUID])
		}
		return nil
	}))
	originalAlias := consolidationMediaRequest(t, repo, a, media.UUID, "linked", false)
	allHeads := consolidationMediaRequest(t, repo, b, media.UUID, "linked", true)
	before := identityRows(t, repo, "source_posts", "post_media_decisions", "post_media_links", "post_media_supersessions")
	for _, input := range []models.SourcePostMediaInput{originalAlias, allHeads} {
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourcePostMedia.Decide(ctx, input)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	}
	require.Equal(t, before, identityRows(t, repo, "source_posts", "post_media_decisions", "post_media_links", "post_media_supersessions"))
	require.Equal(t, original, applyConsolidationMedia(t, repo, originalInput, ""), "the original committed request remains recoverable")
	applyConsolidationMedia(t, repo, allHeads, merge.UUID)
	applyConsolidationMedia(t, repo, consolidationMediaRequest(t, repo, b, media.UUID, "unlinked", false), "")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourcePostMedia.Association(ctx, a, media.UUID)
		require.NoError(t, err)
		require.Equal(t, "unlinked", current.State)
		return nil
	}))
}
