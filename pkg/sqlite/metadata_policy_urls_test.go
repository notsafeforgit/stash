package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyUsesSharedPostURLsAndRechecksLaterEvidence(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	capture, attachment := attachmentFixture(t, f.repo)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"urls": {JQ: `.source | select(.urls_complete) | .urls | select(length > 0)`},
	}})
	input := f.input(policy)
	input.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
	for _, url := range []string{"https://original.test/post", "https://mirror.test/post"} {
		_, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(capture.PostUUID), URL: url})
		require.NoError(t, err)
	}
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "another-post"}, "")
	_, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(other.UUID), URL: "https://unrelated.test/post"})
	require.NoError(t, err)
	before := metadataHistory(t, f.repo, f.entity.UUID, "urls")
	preview := policyPreview(t, f.repo, input)
	change := policyChange(t, preview, "urls")
	require.Equal(t, "ready", change.Status)
	require.JSONEq(t, `["https://mirror.test/post","https://original.test/post"]`, string(change.Value))
	require.Equal(t, before, metadataHistory(t, f.repo, f.entity.UUID, "urls"), "reading shared URLs does not edit the entity")
	_, err = observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(capture.PostUUID), URL: "https://original.test/post"})
	require.NoError(t, err)
	require.Equal(t, preview.Digest, policyPreview(t, f.repo, input).Digest, "another observation of one URL must not duplicate the mapping value")
	_, err = observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(capture.PostUUID), URL: "https://later.test/post"})
	require.NoError(t, err)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "new shared evidence requires a fresh metadata preview")
	preview = policyPreview(t, f.repo, input)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	state := metadataState(t, f.repo, f.entity.UUID, "urls")
	require.JSONEq(t, `["https://later.test/post","https://mirror.test/post","https://original.test/post"]`, string(state.Value))
	require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.JSONEq(t, string(state.Value), string(metadataState(t, f.repo, f.entity.UUID, "urls").Value))
}
