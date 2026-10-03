package archive_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func enrichmentPlanFixture(t *testing.T, count int, longURL bool) models.EnrichmentActivationPlan {
	t.Helper()
	plan := models.EnrichmentActivationPlan{Version: 1, Input: models.EnrichmentActivationInput{UUID: uuid.NewString()}}
	for i := range count {
		target := models.EnrichmentTargetInput{PostUUID: uuid.NewString(), URLUUID: uuid.NewString(), CollectionUUID: uuid.NewString(), CollectionRevision: 1, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"}
		id, err := archive.EnrichmentTargetIdentity(target)
		require.NoError(t, err)
		ref := models.EnrichmentTargetRef{TargetUUID: id, Revision: 1}
		destination := 1 + i%2
		target.CollectionRevision = destination
		released, err := archive.EnrichmentTargetIdentity(target)
		require.NoError(t, err)
		revision := 1
		if released == id {
			revision = 2
		}
		url := "https://x.com/source/status/123"
		if longURL {
			url += "?" + strings.Repeat("&", 8100)
		}
		plan.Input.Targets = append(plan.Input.Targets, models.EnrichmentActivationSelection{EnrichmentTargetRef: ref, CollectionRevision: destination})
		plan.Entries = append(plan.Entries, models.EnrichmentActivationEntry{EnrichmentTargetRef: ref, ReleasedTargetUUID: released, ReleasedRevision: revision,
			ActivationCollectionRevision: destination, PostUUID: target.PostUUID, PostRevision: 3, URLUUID: target.URLUUID, URL: url, CollectionUUID: target.CollectionUUID,
			CollectionRevision: 1, Policy: target.Policy, Priority: 20, NotBefore: time.Now().UTC()})
	}
	var err error
	plan.Input, _, err = archive.PrepareEnrichmentActivation(plan.Input)
	require.NoError(t, err)
	slices.SortFunc(plan.Entries, func(a, b models.EnrichmentActivationEntry) int { return strings.Compare(a.TargetUUID, b.TargetUUID) })
	plan.PlanSHA256, err = archive.EnrichmentActivationDigest(plan)
	require.NoError(t, err)
	return plan
}

func TestEnrichmentActivationPlanBoundsAndCanonicalIdentity(t *testing.T) {
	plan := enrichmentPlanFixture(t, 2, false)
	input := plan.Input
	original := slices.Clone(input.Targets)
	prepared, digest, err := archive.PrepareEnrichmentActivation(input)
	require.NoError(t, err)
	require.Equal(t, original, input.Targets)
	slices.Reverse(input.Targets)
	reversed, other, err := archive.PrepareEnrichmentActivation(input)
	require.NoError(t, err)
	require.Equal(t, prepared, reversed)
	require.Equal(t, digest, other)
	for _, invalid := range []models.EnrichmentActivationInput{
		{}, {UUID: input.UUID}, {UUID: "invalid", Targets: input.Targets},
		{UUID: input.UUID, Targets: []models.EnrichmentActivationSelection{{EnrichmentTargetRef: models.EnrichmentTargetRef{TargetUUID: uuid.NewString(), Revision: 1}}}},
		{UUID: input.UUID, Targets: []models.EnrichmentActivationSelection{input.Targets[0], input.Targets[0]}},
		{UUID: input.UUID, SnapshotUUID: uuid.NewString(), Targets: input.Targets}, {UUID: input.UUID, ManifestSHA256: strings.Repeat("a", 64), Targets: input.Targets},
		{UUID: input.UUID, Targets: make([]models.EnrichmentActivationSelection, 101)},
	} {
		_, _, err := archive.PrepareEnrichmentActivation(invalid)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	plan.Input = prepared
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	decoded, err := archive.DecodeEnrichmentActivationPlan(body)
	require.NoError(t, err)
	require.Equal(t, plan, *decoded)
	for _, invalid := range []string{`null`, `[]`, string(body) + `{}`, strings.Replace(string(body), `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(string(body), `"version":1`, `"version":1,"unknown":true`, 1), strings.Replace(string(body), `"priority":20`, `"priority":21`, 1)} {
		_, err := archive.DecodeEnrichmentActivationPlan([]byte(invalid))
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	for _, field := range []string{"post_revision", "released_revision", "released_target_uuid", "activation_collection_revision"} {
		var value map[string]any
		require.NoError(t, json.Unmarshal(body, &value))
		entry := value["entries"].([]any)[0].(map[string]any)
		if field == "released_target_uuid" {
			entry[field] = uuid.NewString()
		} else {
			entry[field] = 0
		}
		modified, err := json.Marshal(value)
		require.NoError(t, err)
		var invalid models.EnrichmentActivationPlan
		require.NoError(t, json.Unmarshal(modified, &invalid))
		invalid.PlanSHA256, err = archive.EnrichmentActivationDigest(invalid)
		require.NoError(t, err)
		modified, err = json.Marshal(invalid)
		require.NoError(t, err)
		_, err = archive.DecodeEnrichmentActivationPlan(modified)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid, "valid hash cannot excuse invalid source/destination scope")
	}
}

func TestEnrichmentActivationRetainsLongURLsWithinBound(t *testing.T) {
	plan := enrichmentPlanFixture(t, archive.MaxEnrichmentActivationTargets, true)
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	require.Greater(t, len(body), 4<<20, "native JSON escaping must fit a complete page")
	_, err = archive.DecodeEnrichmentActivationPlan(body)
	require.NoError(t, err)
}
