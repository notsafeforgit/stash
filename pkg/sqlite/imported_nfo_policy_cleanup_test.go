package sqlite_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

const previousNFODetailsMapping = `def nonempty: type == "string" and length > 0;
.source as $source
| if $source == null then empty
  else ($source.payload.nfo_fields // {}) as $nfo
  | if (($nfo["original-plot"][0] | nonempty) and ($nfo["plot"][0] | nonempty)) then
      $nfo["plot"][0]
    elif $source.translations_complete != true then
      error("Translation choices are incomplete")
    else
      ([ $source.translations[]
         | select(.target_language == "en" and (.source_fields | index("original_text"))) ]
       | sort_by(.captured_at, .evidence_uuid)
       | last | .translated_text | select(nonempty))
      // ($source.metadata.original_text | readable_text | select(nonempty))
      // empty
    end
  end`

func TestImportedNFOCleanupPreservesCaptureDisplayChoicesAndPolicies(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "saved-display"}, "")
	var inputs []metadata.Input
	titles := []string{"Saved title", "Other saved title"}
	descriptions := []string{"Saved description", "Original description"}
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: false, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{
		"title":   {JQ: strings.NewReplacer("original-plot", "original-title", `"plot"`, `"title"`, "original_text", "title").Replace(previousNFODetailsMapping)},
		"details": {JQ: previousNFODetailsMapping}, "code": {Value: json.RawMessage(`"Keep this default"`)},
	}})
	for i := range titles {
		fields, err := json.Marshal(map[string][]string{"title": {titles[i]}, "original-title": {"Original title"}, "plot": {descriptions[i]}, "original-plot": {"Original description"}})
		require.NoError(t, err)
		data, err := archive.CollateLegacyNFOFields(fields)
		require.NoError(t, err)
		payload, err := archive.PrepareRetainedCapture("legacy-nfo", "twitter", json.RawMessage(`{"nfo_fields":`+string(fields)+`,"nfo_path":"old.nfo"}`))
		require.NoError(t, err)
		capture := recordSourceTestCapture(t, f.repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "legacy-nfo", Platform: "twitter", CapturedAt: time.Now().UTC(), RetentionPolicy: "legacy-retained-v1", Metadata: data.Metadata, Payload: *payload})
		manifest := recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, uuid.NewString())}})
		attachment := manifestEntries(t, f.repo, manifest.UUID)[0].Attachment
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
		}))
		require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
		input := f.input(policy)
		input.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
		inputs = append(inputs, input)
	}
	// A later English result must not displace either capture's saved choice,
	// including an explicit choice to retain the untranslated original.
	translation := retainTranslation(t, f.repo, models.SourceTranslationInput{OriginalText: translationPointer("Original description"), TranslatedText: "Later English", TargetLanguage: translationPointer("en")})
	_, err := translationEvidence(f.repo, models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: translation.UUID, PostUUID: post.UUID, Origin: "migration", Provenance: "saved-result", CapturedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	require.NoError(t, err)
	for i, input := range inputs {
		preview := policyPreview(t, f.repo, input)
		require.Equal(t, `"`+titles[i]+`"`, string(policyChange(t, preview, "title").Value))
		require.Equal(t, `"`+descriptions[i]+`"`, string(policyChange(t, preview, "details").Value))
	}
	result, err := f.db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Policies)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		require.Equal(t, policy.Revision+1, current.Revision)
		require.Equal(t, policy.Definition.Rules[models.ArchiveScene].Mappings["code"], current.Definition.Rules[models.ArchiveScene].Mappings["code"])
		require.False(t, current.Definition.Rules[models.ArchiveScene].MarkOrganized)
		history, err := f.repo.MetadataPolicy.History(ctx, f.collection.UUID, 0, 100)
		require.NoError(t, err)
		require.Equal(t, policy.Definition, history[0].Definition, "immutable prior definitions are not rewritten")
		return nil
	}))
	for i, input := range inputs {
		input.PolicyRevision++
		preview := policyPreview(t, f.repo, input)
		require.Equal(t, `"`+titles[i]+`"`, string(policyChange(t, preview, "title").Value))
		require.Equal(t, `"`+descriptions[i]+`"`, string(policyChange(t, preview, "details").Value))
		body, err := json.Marshal(preview.Data["source"])
		require.NoError(t, err)
		require.NotContains(t, string(body), "nfo_fields")
		require.NotContains(t, string(body), "old.nfo")
	}
	second, err := f.db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Zero(t, second.Policies)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestImportedNFOCleanupRejectsCustomNFOExpressions(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, Mappings: map[string]models.MetadataMapping{"title": {JQ: `.source.payload.nfo_fields.title[0]`}}})
	_, err := f.db.CompactImportedNFO(t.Context(), nil)
	require.ErrorContains(t, err, "custom title mapping")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		require.Equal(t, policy, current)
		return nil
	}))
}
