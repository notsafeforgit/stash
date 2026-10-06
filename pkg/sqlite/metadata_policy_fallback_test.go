package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyTypedFallbackForManualScan(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	var mapping models.MetadataMapping
	require.NoError(t, json.Unmarshal([]byte(`{"jq":".source.payload.actors | select(type == \"array\" and length > 0)","reference_names":true,"fallback":["`+performer.UUID+`"]}`), &mapping))
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, MarkOrganized: true, Mappings: map[string]models.MetadataMapping{"performers": mapping}})
	preview := policyPreview(t, f.repo, f.input(policy))
	change := policyChange(t, preview, "performers")
	require.Equal(t, "ready", change.Status)
	require.JSONEq(t, `["`+performer.UUID+`"]`, string(change.Value))
	require.Equal(t, "policy", change.Origin)
	require.True(t, change.UsedFallback)
	require.Empty(t, change.CaptureUUID)
	require.Empty(t, change.Names, "an explicit fallback UUID is not a name to match")
	require.Equal(t, performer.Revision, change.ReferenceRevisions[performer.UUID])
	_, err := applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	require.JSONEq(t, string(change.Value), string(metadataState(t, f.repo, f.entity.UUID, "performers").Value))
	require.Equal(t, "policy", metadataState(t, f.repo, f.entity.UUID, "performers").Origin)
	require.Equal(t, "true", string(metadataState(t, f.repo, f.entity.UUID, "organized").Value))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, "unchanged", policyChange(t, policyPreview(t, f.repo, f.input(policy)), "performers").Status)
}

func TestMetadataPolicyFallbackDoesNotReplaceResultsOrErrors(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	attachmentSQL(t, f.db, `INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Unique default test',1)`)
	var revision int
	for _, tc := range []struct {
		name, field, expression, fallback, want, status string
		names                                           bool
	}{
		{"null", "date", "null", `"2026-09-29"`, "null", "unchanged", false},
		{"false", "organized", "false", "true", "false", "unchanged", false},
		{"zero", "rating100", "0", "75", "0", "ready", false},
		{"blank", "title", `""`, `"Default title"`, `""`, "unchanged", false},
		{"empty list", "performers", "[]", `["` + performer.UUID + `"]`, "[]", "unchanged", true},
		{"name", "performers", `["Unique default test"]`, "[]", `["` + performer.UUID + `"]`, "ready", true},
		{"missing name", "performers", `["Unknown default test"]`, `["` + performer.UUID + `"]`, "", "review", true},
		{"ambiguous name", "performers", `["Shared name"]`, `["` + performer.UUID + `"]`, "", "review", true},
		{"error", "title", `error("bad source")`, `"Default title"`, "", "review", false},
		{"multiple results", "title", `"one", "two"`, `"Default title"`, "", "review", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := f.put(t, revision, models.MetadataPolicyRule{OnCreate: true, Mappings: map[string]models.MetadataMapping{
				tc.field: {JQ: tc.expression, ReferenceNames: tc.names, Fallback: json.RawMessage(tc.fallback)},
			}})
			revision = policy.Revision
			change := policyChange(t, policyPreview(t, f.repo, f.input(policy)), tc.field)
			require.Equal(t, tc.status, change.Status)
			require.False(t, change.UsedFallback)
			if tc.want != "" {
				require.JSONEq(t, tc.want, string(change.Value))
			}
			if tc.name == "ambiguous name" {
				require.Len(t, change.Names[0].Candidates, 2)
			}
		})
	}
}

func TestMetadataPolicyEmptyTypedFallbacksRemainConfigured(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	values := map[string]string{"date": "null", "organized": "false", "rating100": "0", "title": `""`, "performers": "[]", "custom_fields": "{}"}
	mappings := make(map[string]models.MetadataMapping)
	for field, value := range values {
		mappings[field] = models.MetadataMapping{JQ: "empty", Fallback: json.RawMessage(value)}
	}
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, Mappings: mappings})
	preview := policyPreview(t, f.repo, f.input(policy))
	for field, value := range values {
		change := policyChange(t, preview, field)
		require.True(t, change.UsedFallback, field)
		require.JSONEq(t, value, string(change.Value), field)
		require.NotEqual(t, "omitted", change.Status, field)
	}
	_, err := applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	for field, value := range values {
		require.JSONEq(t, value, string(metadataState(t, f.repo, f.entity.UUID, field).Value), field)
	}
}

func TestMetadataPolicyFallbackPreservesSourceAndExplicitDecisions(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	capture, attachment := attachmentFixture(t, f.repo)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title":   {JQ: `.source.metadata.title // empty`, Fallback: json.RawMessage(`"Folder title"`)},
		"details": {JQ: `empty`, Fallback: json.RawMessage(`"Folder details"`)},
	}}
	policy := f.put(t, 0, rule)
	input := f.input(policy)
	input.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
	preview := policyPreview(t, f.repo, input)
	title, details := policyChange(t, preview, "title"), policyChange(t, preview, "details")
	require.False(t, title.UsedFallback)
	require.Equal(t, capture.UUID, title.CaptureUUID)
	require.Equal(t, "source", title.Origin)
	require.True(t, details.UsedFallback)
	require.Equal(t, "policy", details.Origin)
	require.Empty(t, details.CaptureUUID, "a policy default is not an assertion from the selected capture")
	_, err := applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	require.Nil(t, metadataState(t, f.repo, f.entity.UUID, "details").Decision.CaptureUUID)
	manual := input
	manual.Source = nil
	_, err = applyPolicy(f.repo, manual, "")
	require.NoError(t, err)
	require.JSONEq(t, string(title.Value), string(metadataState(t, f.repo, f.entity.UUID, "title").Value), "an unsourced rescan must preserve source-selected metadata")
	_, err = applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "details"), "clear", "review", ""), false)
	require.NoError(t, err)
	preview = policyPreview(t, f.repo, input)
	require.True(t, policyChange(t, preview, "details").UsedFallback)
	require.Equal(t, "protected", policyChange(t, preview, "details").Status)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	require.JSONEq(t, `""`, string(metadataState(t, f.repo, f.entity.UUID, "details").Value))
}

func TestMetadataPolicyUnusedFallbackValidation(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	for _, tc := range []struct {
		field, expression, fallback string
		names                       bool
	}{
		{"rating100", "50", "101", false},
		{"title", `"A title"`, "[]", false},
		{"performers", `[]`, `["Shared name"]`, true},
		{"performers", `[]`, `["` + uuid.NewString() + `"]`, true},
		{"performers", `[]`, `["` + f.entity.UUID + `"]`, false},
	} {
		t.Run(tc.field+tc.fallback, func(t *testing.T) {
			input := policyImportInput(t, f)
			// Validate the image rule even when previewing a scene and even when
			// the primary expression would supply a valid result.
			input.Policy.Definition.Rules[models.ArchiveImage] = models.MetadataPolicyRule{OnCreate: true, Mappings: map[string]models.MetadataMapping{
				tc.field: {JQ: tc.expression, Fallback: json.RawMessage(tc.fallback), ReferenceNames: tc.names},
			}}
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.MetadataPolicy.Put(ctx, input.Policy)
				return err
			})
			require.ErrorIs(t, err, models.ErrMetadataPolicyInvalid)
			err = f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := (metadata.Service{Repo: f.repo}).PreviewDraft(ctx, metadata.Input{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision,
					EntityUUID: f.entity.UUID, RelativePath: "Manual.mp4", Created: true}, input.Policy.Definition)
				return err
			})
			require.ErrorIs(t, err, models.ErrMetadataPolicyInvalid)
			err = f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.MetadataPolicyImport.Preview(ctx, input, time.Now())
				return err
			})
			require.ErrorIs(t, err, models.ErrMetadataPolicyImportInvalid)
		})
	}
}

func TestMetadataPolicyFallbackReferenceGuardsAndRedirects(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule := models.MetadataPolicyRule{OnCreate: true, Mappings: map[string]models.MetadataMapping{
		"performers": {JQ: "empty", ReferenceNames: true, Fallback: json.RawMessage(`["` + performer.UUID + `"]`)},
	}}
	policy := f.put(t, 0, rule)
	preview := policyPreview(t, f.repo, f.input(policy))
	attachmentSQL(t, f.db, `UPDATE performer_names SET name='Default renamed' WHERE performer_id=71 AND position=0`)
	_, err := applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict)
	performer = archiveFind(t, f.repo, models.ArchivePerformer, 71)
	redirect := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, performer.UUID, redirect, performer.Revision)
		return err
	}))
	preview = policyPreview(t, f.repo, f.input(policy))
	require.JSONEq(t, `["`+redirect+`"]`, string(policyChange(t, preview, "performers").Value))
	_, err = applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	require.JSONEq(t, `["`+performer.UUID+`"]`, string(policy.Definition.Rules[models.ArchiveScene].Mappings["performers"].Fallback), "retain the original reviewed UUID in policy history")
}

func TestMetadataPolicyImportBindsUnusedFallbackAndReplaysAfterRestart(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	input := policyImportInput(t, f)
	mappings := input.Policy.Definition.Rules[models.ArchiveScene].Mappings
	original := mappings["performers"].Value
	mappings["performers"] = models.MetadataMapping{JQ: `[]`, ReferenceNames: true, Fallback: original}
	plan := previewPolicyImport(t, f, input)
	require.Len(t, plan.ReferenceRevisions, 1, "unused defaults also bind the reviewed target")
	attachmentSQL(t, f.db, `UPDATE performer_names SET name='Renamed import default' WHERE performer_id=71 AND position=0`)
	_, err := applyPolicyImport(f, input, plan.PlanSHA256)
	require.ErrorIs(t, err, models.ErrMetadataPolicyImportConflict)
	plan = previewPolicyImport(t, f, input)
	first, err := applyPolicyImport(f, input, plan.PlanSHA256)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	again, err := applyPolicyImport(f, input, plan.PlanSHA256)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.JSONEq(t, string(original), string(again.Definition.Rules[models.ArchiveScene].Mappings["performers"].Fallback))
}
