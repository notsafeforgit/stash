package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyOrganizedRequirementsSurviveRestartAndUseAppliedValues(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true,
		OrganizedRequires: []string{"title", "performers", "details", "date"},
		Mappings: map[string]models.MetadataMapping{
			"title":      {Value: json.RawMessage(`"Purchased video"`)},
			"performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)},
			"date":       {Value: json.RawMessage(`"2026-01-01"`)},
		}}
	policy := f.put(t, 0, rule)
	preview := policyPreview(t, f.repo, f.input(policy))
	organized := policyChange(t, preview, "organized")
	require.Equal(t, "omitted", organized.Status)
	require.Contains(t, organized.Message, "details")
	applied, err := applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"title", "performers", "date"}, applied.AppliedFields())
	require.Equal(t, "false", string(metadataState(t, f.repo, f.entity.UUID, "organized").Value))

	// A deliberate existing choice counts, even if the policy cannot overwrite it.
	_, err = applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "details"), "set", "review", `"Purchase notes"`), false)
	require.NoError(t, err)
	dbPath := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(dbPath))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		stored, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		if err == nil {
			require.Equal(t, rule.OrganizedRequires, stored.Definition.Rules[models.ArchiveScene].OrganizedRequires)
		}
		return err
	}))
	preview = policyPreview(t, f.repo, f.input(policy))
	require.Equal(t, "ready", policyChange(t, preview, "organized").Status)
	_, err = applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	require.Equal(t, "true", string(metadataState(t, f.repo, f.entity.UUID, "organized").Value))

	// Requirements only control marking. They never unmark an organized item.
	_, err = applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "details"), "clear", "review", ""), false)
	require.NoError(t, err)
	applied, err = applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	require.NotContains(t, applied.AppliedFields(), "organized")
	require.Equal(t, "true", string(metadataState(t, f.repo, f.entity.UUID, "organized").Value))
}

func TestMetadataPolicyOrganizedRequirementDoesNotCountClearedOrProtectedCandidate(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true,
		OrganizedRequires: []string{"title", "performers"}, Mappings: map[string]models.MetadataMapping{
			"title":      {Value: json.RawMessage(`"Existing title"`)},
			"performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)},
		}}
	policy := f.put(t, 0, rule)
	// Preserve an explicit empty performer list; the proposed default cannot pass
	// completeness merely because it contains a valid performer UUID.
	_, err := applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "performers"), "clear", "review", ""), false)
	require.NoError(t, err)
	preview := policyPreview(t, f.repo, f.input(policy))
	require.Equal(t, "protected", policyChange(t, preview, "performers").Status)
	require.Equal(t, "omitted", policyChange(t, preview, "organized").Status)
	_, err = applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	rule.Mappings["title"] = models.MetadataMapping{Value: json.RawMessage(`""`)}
	policy = f.put(t, policy.Revision, rule)
	preview = policyPreview(t, f.repo, f.input(policy))
	require.Equal(t, "ready", policyChange(t, preview, "title").Status)
	require.Contains(t, policyChange(t, preview, "organized").Message, "title, performers")
	_, err = applyPolicy(f.repo, f.input(policy), preview.Digest)
	require.NoError(t, err)
	require.Equal(t, "false", string(metadataState(t, f.repo, f.entity.UUID, "organized").Value))
}

func TestMetadataPolicyEmptyRequirementsRemainNoOp(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	rule := models.MetadataPolicyRule{OnCreate: true, MarkOrganized: true}
	policy := f.put(t, 0, rule)
	rule.OrganizedRequires = []string{}
	require.Equal(t, policy, f.put(t, policy.Revision, rule))
	require.NotNil(t, rule.OrganizedRequires, "normalization must not mutate caller input")
}
