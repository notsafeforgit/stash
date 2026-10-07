package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func postSelectionCapture(t *testing.T, repo models.Repository, post string, positions ...int) models.SourceCaptureInput {
	t.Helper()
	retained, err := archive.RetainSourcePayload([]byte(`{"category":"reddit","id":"one-post","title":"Original album"}`))
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", retained)
	require.NoError(t, err)
	title := "Original album"
	input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: "gallery-dl", Platform: "reddit",
		CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Metadata: models.SourcePostMetadata{Title: &title}, Payload: *payload}
	count := 3
	manifest := models.SourceAttachmentManifestInput{CaptureUUID: input.UUID, ExpectedCount: &count, DeclaredAlbum: true}
	for _, position := range positions {
		manifest.Entries = append(manifest.Entries, models.SourceAttachmentEntry{Position: position, MediaKind: "image",
			Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: []string{"first", "second", "third"}[position]}})
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, err := repo.SourceEvidence.RecordCapture(ctx, input); err != nil {
			return err
		}
		_, err := repo.SourceAttachment.RecordManifest(ctx, manifest)
		return err
	}))
	return input
}

func postSelectionApply(t *testing.T, repo models.Repository, post, capture, mode, origin string) *models.AttachmentSelection {
	t.Helper()
	var selected *models.AttachmentSelection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		selected, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: p.Revision, CaptureUUID: capture, Mode: mode, Origin: origin})
		return err
	}))
	return selected
}

func TestPostSelectionProvenanceCombinesOriginalListsAcrossChainedIdentities(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "legacy:catalog:fixture", "b")
	c := identityPost(t, repo, "native:reddit", "one-post")
	first := postSelectionCapture(t, repo, a, 0, 2)
	second := postSelectionCapture(t, repo, b, 1)
	old := postSelectionApply(t, repo, a, first.UUID, "pinned", "review")
	before := identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries")
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	input, expected, manifests := consolidationSelectionRequest(t, repo, c, first.UUID, "automatic")
	var selected *models.AttachmentSelection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		selected, err = publishConsolidatedPostSelection(ctx, input, merge.UUID, expected, manifests)
		return err
	}))
	require.Len(t, selected.Entries, 2)
	selected = postSelectionApply(t, repo, c, second.UUID, "automatic", "ingest")
	require.Len(t, selected.Decision.ManifestUUIDs, 2)
	require.Len(t, selected.Entries, 3)
	require.Equal(t, a, selected.Entries[0].Attachment.PostUUID)
	require.Equal(t, b, selected.Entries[1].Attachment.PostUUID)
	require.Equal(t, a, selected.Entries[2].Attachment.PostUUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceGallery.Preview(ctx, c)
		require.NoError(t, err)
		require.Equal(t, "create", preview.Action)
		require.Equal(t, "Original album", preview.Title)
		result, err := repo.SourceGallery.Sync(ctx, c, preview.Signature)
		require.NoError(t, err)
		require.True(t, result.Created)
		var capture string
		require.NoError(t, dbWrapper.Get(ctx, &capture, "SELECT capture_uuid FROM metadata_field_decisions WHERE entity_uuid=? AND field='title'", result.GalleryUUID))
		require.Equal(t, second.UUID, capture, "gallery metadata retains its original source capture")
		return nil
	}))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, before, identityRows(t, repo, "source_captures", "source_post_revisions", "source_attachment_manifests", "source_attachment_entries"))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceAttachment.Selection(ctx, c)
		require.NoError(t, err)
		require.Equal(t, selected, current)
		for _, alias := range []string{a, b} {
			current, err := repo.SourceAttachment.Selection(ctx, alias)
			require.NoError(t, err)
			require.Equal(t, selected, current)
			preview, err := repo.SourceAttachment.PreviewSelection(ctx, alias, first.UUID)
			require.NoError(t, err)
			require.Equal(t, c, preview.PostUUID)
			album, err := repo.SourceGallery.ReadAlbum(ctx, alias, -1, 25)
			require.NoError(t, err)
			require.Equal(t, alias, album.RequestedUUID)
			require.Equal(t, c, album.PostUUID)
			require.Equal(t, c, album.Album.PostUUID)
			require.Len(t, album.Slots, 3)
			_, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: alias,
				ExpectedPostRevision: preview.PostRevision, CaptureUUID: first.UUID, Mode: "pinned", Origin: "review"})
			require.ErrorIs(t, err, models.ErrAttachmentSelectionConflict)
		}
		posts, err := repo.SourceAttachment.SelectedPosts(ctx, "", 100)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		require.Equal(t, c, posts[0].PostUUID)
		lists, err := repo.SourceAttachment.ReviewSelectionManifests(ctx, a, "", 100)
		require.NoError(t, err)
		require.Len(t, lists, 2)
		history, err := repo.SourceAttachment.SelectionHistory(ctx, a, 0, 10)
		require.NoError(t, err)
		require.Equal(t, []models.AttachmentSelectionDecision{old.Decision}, history)
		replay, err := repo.SourceEvidence.RecordCapture(ctx, first)
		require.NoError(t, err)
		require.Equal(t, a, replay.PostUUID)
		preview, err := repo.SourceAttachment.PreviewSelection(ctx, c, first.UUID)
		require.NoError(t, err)
		require.False(t, preview.Changed)
		return nil
	}))
}

func TestPostSelectionProvenanceReviewRetryAndExplicitExclusionsSurviveMerge(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "legacy:catalog:fixture", "b")
	c := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	var request models.AttachmentSelectionReviewApplyInput
	var receipt *models.AttachmentSelectionReview
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.FindPost(ctx, b)
		require.NoError(t, err)
		preview, err := repo.SourceAttachment.PreviewSelectionReview(ctx, models.AttachmentSelectionReviewInput{PostUUID: b,
			PostRevision: post.Revision, CaptureUUID: capture.UUID, Mode: "pinned", Reason: "Keep original album order"})
		require.NoError(t, err)
		request = models.AttachmentSelectionReviewApplyInput{AttachmentSelectionReviewInput: preview.Input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
		receipt, _, err = repo.SourceAttachment.ApplySelectionReview(ctx, request)
		return err
	}))
	for _, mode := range []string{"pinned", "disabled"} {
		if mode == "disabled" {
			postSelectionApply(t, repo, b, "", mode, "review")
		}
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			preview, err := repo.SourceAttachment.PreviewSelection(ctx, b, capture.UUID)
			require.NoError(t, err)
			require.True(t, preview.Protected)
			_, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: b, ExpectedPostRevision: preview.PostRevision,
				CaptureUUID: capture.UUID, Mode: "automatic", Origin: "ingest"})
			return err
		})
		require.ErrorIs(t, err, models.ErrAttachmentSelectionProtected)
	}
	publishPostIdentity(t, repo, identityRequest(t, repo, b, c))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replayed, replay, err := repo.SourceAttachment.ApplySelectionReview(ctx, request)
		require.NoError(t, err)
		require.True(t, replay)
		require.Equal(t, receipt, replayed)
		return nil
	}))
}

func TestPostSelectionProvenanceRejectsUnrelatedEvidenceInAPIAndSQL(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "other-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	other := postSelectionCapture(t, repo, b, 0, 1)
	selected := postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
	before := identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.PreviewSelection(ctx, a, other.UUID)
		require.ErrorIs(t, err, models.ErrAttachmentSelectionConflict)
		return nil
	}))
	for _, test := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO post_attachment_decisions(uuid,post_uuid,revision,mode,origin,capture_uuid,manifest_count,signature)
 VALUES(?,?,999,'pinned','review',?,1,?)`, []any{uuid.NewString(), a, other.UUID, strings.Repeat("a", 64)}},
		{`INSERT INTO post_attachment_decision_manifests(post_uuid,decision_uuid,manifest_uuid)
 SELECT ?,?,manifest_uuid FROM source_capture_attachment_manifests WHERE capture_uuid=?`, []any{a, selected.Decision.UUID, other.UUID}},
	} {
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := dbWrapper.Exec(ctx, test.query, test.args...)
			return err
		})
		require.ErrorContains(t, err, "another post identity")
	}
	require.Equal(t, before, identityRows(t, repo, "source_posts", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections"))
}

func TestPostSelectionProvenanceUsesOnlyContributingAttachmentIdentities(t *testing.T) {
	ref := models.SourcePostIdentifier{Namespace: "native:reddit", Value: "one-image"}
	sources := map[string]selectionManifest{}
	for _, id := range []string{"a", "b", "c"} {
		sources[id] = selectionManifest{entries: []models.SourceAttachmentManifestEntry{{Attachment: models.SourceAttachment{UUID: id, Reference: ref}}}}
	}
	manifest := models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{{Reference: ref}}}
	for range 100 {
		selected := selectionFromMerge(models.AttachmentSelectionDecision{ManifestUUIDs: []string{"c", "b"}}, manifest, sources)
		require.Equal(t, "b", selected.Entries[0].Attachment.UUID, "unused candidate a must not replace a current attachment")
	}
}
