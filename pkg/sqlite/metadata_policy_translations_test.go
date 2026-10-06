package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyUsesRetainedTranslationsWithoutChangingOriginalText(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "translated-policy"}, "")
	original := "元の文章"
	captureInput := sourceTestCapture(t, post.UUID, 1, "Biography")
	captureInput.Metadata.Title, captureInput.Metadata.OriginalText = &original, &original
	capture := recordSourceTestCapture(t, f.repo, captureInput)
	manifest := recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "translated-attachment")}})
	attachment := manifestEntries(t, f.repo, manifest.UUID)[0].Attachment
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	observe := func(result *models.SourceTranslation, owner, observed string) *models.SourceTranslationEvidence {
		evidence, err := translationEvidence(f.repo, models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: result.UUID,
			PostUUID: owner, CapturedAt: observed, Origin: "migration", Provenance: "retained-source-result"})
		require.NoError(t, err)
		return evidence
	}
	translated := func(input, text string, language *string) *models.SourceTranslation {
		return retainTranslation(t, f.repo, models.SourceTranslationInput{OriginalText: &input, TranslatedText: text,
			SourceLanguage: translationPointer("ja"), TargetLanguage: language})
	}
	first := translated(original, "Best English", translationPointer("en"))
	observe(first, post.UUID, "2026-10-01T00:00:00Z")
	latestEvidence := observe(first, post.UUID, "2026-10-01T23:30:00-07:00")
	older := translated(original, "Earlier English", translationPointer("en"))
	observe(older, post.UUID, "2026-10-02T05:00:00Z")
	unknown := translated(original, "Unknown language", nil)
	observe(unknown, post.UUID, "")
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "unrelated-policy"}, "")
	observe(translated(original, "Unrelated post", translationPointer("en")), other.UUID, "2026-10-02T09:00:00Z")
	observe(translated("A different original", "Unrelated original", translationPointer("en")), post.UUID, "2026-10-02T09:00:00Z")
	observe(retainTranslation(t, f.repo, models.SourceTranslationInput{TranslatedText: "Unknown original", TargetLanguage: translationPointer("en")}), post.UUID, "")
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `.source as $source | if $source == null then empty
 elif $source.translations_complete != true then error("Translation choices are incomplete") else
 ([ $source.translations[] | select(.target_language == "en" and (.source_fields | index("title"))) ]
  | sort_by(.captured_at, .evidence_uuid) | last | .translated_text | select(type == "string" and length > 0))
 // $source.metadata.title // empty end`},
	}})
	input := f.input(policy)
	manual := policyPreview(t, f.repo, input)
	require.Equal(t, "filename", policyChange(t, manual, "title").Origin, "a manual file still uses filename fallback without a source")
	input.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
	before := metadataHistory(t, f.repo, f.entity.UUID, "title")
	preview := policyPreview(t, f.repo, input)
	require.Equal(t, "ready", policyChange(t, preview, "title").Status)
	require.JSONEq(t, `"Best English"`, string(policyChange(t, preview, "title").Value))
	require.Equal(t, before, metadataHistory(t, f.repo, f.entity.UUID, "title"), "reading translation choices must not change metadata")
	var source struct {
		Metadata             models.SourcePostMetadata `json:"metadata"`
		TranslationsComplete bool                      `json:"translations_complete"`
		Translations         []struct {
			UUID         string   `json:"uuid"`
			SourceFields []string `json:"source_fields"`
			EvidenceUUID string   `json:"evidence_uuid"`
			CapturedAt   *string  `json:"captured_at"`
			Target       *string  `json:"target_language"`
			Provider     *string  `json:"provider"`
		} `json:"translations"`
	}
	body, err := json.Marshal(preview.Data["source"])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &source))
	require.Equal(t, capture.Metadata, source.Metadata)
	require.True(t, source.TranslationsComplete)
	require.Len(t, source.Translations, 3, "one result serves both source fields and repeated evidence")
	for _, choice := range source.Translations {
		require.Equal(t, []string{"title", "original_text"}, choice.SourceFields)
		require.Nil(t, choice.Provider, "missing provider is not invented")
		if choice.UUID == first.UUID {
			require.Equal(t, latestEvidence.UUID, choice.EvidenceUUID)
			require.Equal(t, "2026-10-02T06:30:00.000000000Z", *choice.CapturedAt)
		}
		if choice.UUID == unknown.UUID {
			require.Nil(t, choice.CapturedAt)
			require.Nil(t, choice.Target)
		}
	}
	observe(first, post.UUID, "2026-10-02T08:00:00Z")
	require.Equal(t, preview.Digest, policyPreview(t, f.repo, input).Digest, "repeat evidence of the selected value does not duplicate or change the selection")
	observe(translated(original, "Revised English", translationPointer("en")), post.UUID, "2026-10-02T09:00:00Z")
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "a newly preferred translation requires a fresh preview")
	preview = policyPreview(t, f.repo, input)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	state := metadataState(t, f.repo, f.entity.UUID, "title")
	require.JSONEq(t, `"Revised English"`, string(state.Value))
	require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
	require.Equal(t, policy.MetadataPolicyRef, *state.Decision.Policy)
	_, err = applyMetadata(f.repo, metadataInput(state, "clear", "review", ""), false)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	preview = policyPreview(t, f.repo, input)
	require.Equal(t, "protected", policyChange(t, preview, "title").Status)
	_, err = applyPolicy(f.repo, input, "")
	require.NoError(t, err)
	require.Equal(t, `""`, string(metadataState(t, f.repo, f.entity.UUID, "title").Value))
	observe(translated(original, "", translationPointer("en")), post.UUID, "2026-10-02T10:00:00Z")
	preview = policyPreview(t, f.repo, input)
	require.JSONEq(t, `"元の文章"`, string(policyChange(t, preview, "title").Value), "an empty latest translation falls back to the original, not an earlier result")
	require.Equal(t, "protected", policyChange(t, preview, "title").Status)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		retained, err := f.repo.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, capture.Metadata, retained.Metadata)
		return nil
	}))
}
