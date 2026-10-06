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

func TestEnrichmentRebindInputBoundsAndStableIdentity(t *testing.T) {
	input := models.EnrichmentRebindInput{UUID: uuid.NewString(), CollectionUUID: uuid.NewString(), CollectionRevision: 3, Reason: "Reviewed current collection settings",
		Targets: []models.EnrichmentTargetRef{{TargetUUID: uuid.NewString(), Revision: 1}, {TargetUUID: uuid.NewString(), Revision: 7}}}
	original := slices.Clone(input.Targets)
	prepared, digest, err := archive.PrepareEnrichmentRebind(input)
	require.NoError(t, err)
	require.Equal(t, original, input.Targets)
	slices.Reverse(input.Targets)
	other, otherDigest, err := archive.PrepareEnrichmentRebind(input)
	require.NoError(t, err)
	require.Equal(t, prepared, other)
	require.Equal(t, digest, otherDigest)
	for _, change := range []func(*models.EnrichmentRebindInput){
		func(v *models.EnrichmentRebindInput) { v.UUID = "invalid" },
		func(v *models.EnrichmentRebindInput) { v.CollectionUUID = "" },
		func(v *models.EnrichmentRebindInput) { v.CollectionRevision = 0 },
		func(v *models.EnrichmentRebindInput) { v.Reason = strings.Repeat("a", 4097) },
		func(v *models.EnrichmentRebindInput) { v.Reason = string([]byte{255}) },
		func(v *models.EnrichmentRebindInput) { v.Targets = nil },
		func(v *models.EnrichmentRebindInput) { v.Targets = make([]models.EnrichmentTargetRef, 101) },
		func(v *models.EnrichmentRebindInput) { v.Targets[0].Revision = 0 },
		func(v *models.EnrichmentRebindInput) { v.Targets[0].Revision = int(^uint(0)>>1) - 1 },
		func(v *models.EnrichmentRebindInput) { v.Targets[1] = v.Targets[0] },
	} {
		changed := input
		changed.Targets = slices.Clone(input.Targets)
		change(&changed)
		_, _, err := archive.PrepareEnrichmentRebind(changed)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	require.Empty(t, archive.EnrichmentRebindActivationUUID("invalid"))
	require.Equal(t, archive.EnrichmentRebindActivationUUID(input.UUID), archive.EnrichmentRebindActivationUUID(input.UUID))
	require.NotEqual(t, input.UUID, archive.EnrichmentRebindActivationUUID(input.UUID))
}

func TestEnrichmentRebindPlanRejectsForgedNestedHistory(t *testing.T) {
	activation := enrichmentPlanFixture(t, 2, true)
	// The second fixture entry has a different destination collection revision.
	var entry models.EnrichmentActivationEntry
	for _, candidate := range activation.Entries {
		if candidate.ReleasedTargetUUID != candidate.TargetUUID {
			entry = candidate
		}
	}
	require.NotEmpty(t, entry.TargetUUID)
	entry.Revision = 2
	input := models.EnrichmentRebindInput{UUID: uuid.NewString(), CollectionUUID: entry.CollectionUUID, CollectionRevision: 2,
		Targets: []models.EnrichmentTargetRef{{TargetUUID: entry.TargetUUID, Revision: 1}}, Reason: "Reviewed collection"}
	activation.Input = models.EnrichmentActivationInput{UUID: archive.EnrichmentRebindActivationUUID(input.UUID),
		Targets: []models.EnrichmentActivationSelection{{EnrichmentTargetRef: entry.EnrichmentTargetRef, CollectionRevision: 2}}}
	activation.Entries = []models.EnrichmentActivationEntry{entry}
	var err error
	activation.PlanSHA256, err = archive.EnrichmentActivationDigest(activation)
	require.NoError(t, err)
	plan := models.EnrichmentRebindPlan{Version: 1, Input: input, Activation: activation,
		Collection: models.SourceCollection{UUID: input.CollectionUUID, Revision: 2, CreatedAt: time.Now().UTC(), SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Current collection", State: "active", Kind: "feed", Namespace: "native:twitter", TargetURL: "https://x.com/source"}}}
	plan.PlanSHA256, err = archive.EnrichmentRebindDigest(plan)
	require.NoError(t, err)
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	decoded, err := archive.DecodeEnrichmentRebindPlan(body)
	require.NoError(t, err)
	require.Equal(t, plan, *decoded)
	for _, invalid := range []string{"null", "[]", string(body) + "{}", strings.Replace(string(body), `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(string(body), `"version":1`, `"version":1,"unexpected":true`, 1), strings.Replace(string(body), `"priority":20`, `"priority":21`, 1)} {
		_, err = archive.DecodeEnrichmentRebindPlan([]byte(invalid))
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	for _, change := range []func(*models.EnrichmentRebindPlan){
		func(v *models.EnrichmentRebindPlan) { v.Activation.Input.UUID = uuid.NewString() },
		func(v *models.EnrichmentRebindPlan) {
			v.Activation.Input.SnapshotUUID = uuid.NewString()
			v.Activation.Input.ManifestSHA256 = strings.Repeat("a", 64)
		},
		func(v *models.EnrichmentRebindPlan) { v.Input.Targets[0].Revision++ },
		func(v *models.EnrichmentRebindPlan) { v.Collection.State = "disabled" },
		func(v *models.EnrichmentRebindPlan) { v.Collection.UUID = uuid.NewString() },
		func(v *models.EnrichmentRebindPlan) { v.Input.CollectionRevision++ },
	} {
		var changed models.EnrichmentRebindPlan
		require.NoError(t, json.Unmarshal(body, &changed))
		change(&changed)
		changed.Activation.PlanSHA256, err = archive.EnrichmentActivationDigest(changed.Activation)
		require.NoError(t, err)
		changed.PlanSHA256, err = archive.EnrichmentRebindDigest(changed)
		require.NoError(t, err)
		forged, err := json.Marshal(changed)
		require.NoError(t, err)
		_, err = archive.DecodeEnrichmentRebindPlan(forged)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid, "matching digests do not excuse changed source and destination history")
	}
}
