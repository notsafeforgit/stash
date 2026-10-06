package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyProjectsReadableTextWithoutChangingEvidence(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "readable-policy"}, "")
	original, title := "<p>One &amp; two</p><div>Three<br>four</div>", "Literal &amp; title"
	candidate := sourceTestCapture(t, post.UUID, 1, "Biography")
	candidate.Metadata.OriginalText, candidate.Metadata.Title = &original, &title
	capture := recordSourceTestCapture(t, f.repo, candidate)
	manifest := recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "readable-attachment")}})
	attachment := manifestEntries(t, f.repo, manifest.UUID)[0].Attachment
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"details": {JQ: `.source.metadata.original_text | readable_text | select(type == "string" and length > 0)`},
		"title":   {JQ: `.source.metadata.title | readable_text | select(type == "string" and length > 0)`},
	}})
	input := f.input(policy)
	require.Equal(t, "omitted", policyChange(t, policyPreview(t, f.repo, input), "details").Status, "a missing source does not clear a direct scan's metadata")
	input.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
	history := metadataHistory(t, f.repo, f.entity.UUID, "details")
	preview := policyPreview(t, f.repo, input)
	change := policyChange(t, preview, "details")
	require.Equal(t, "ready", change.Status)
	require.JSONEq(t, `"One & two\n\nThree\nfour"`, string(change.Value))
	require.JSONEq(t, `"Literal &amp; title"`, string(policyChange(t, preview, "title").Value))
	require.Equal(t, history, metadataHistory(t, f.repo, f.entity.UUID, "details"), "preview is read-only")
	_, err := applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	state := metadataState(t, f.repo, f.entity.UUID, "details")
	require.JSONEq(t, `"One & two\n\nThree\nfour"`, string(state.Value))
	require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		stored, err := f.repo.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, capture.Metadata, stored.Metadata, "the readable projection does not rewrite retained source text")
		return nil
	}))
	_, err = applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "details"), "clear", "review", ""), false)
	require.NoError(t, err)
	preview = policyPreview(t, f.repo, input)
	require.Equal(t, "protected", policyChange(t, preview, "details").Status)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	require.JSONEq(t, `""`, string(metadataState(t, f.repo, f.entity.UUID, "details").Value))
}
