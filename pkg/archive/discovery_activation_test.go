package archive_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func discoveryActivationExample() models.DiscoveryActivationInput {
	return models.DiscoveryActivationInput{UUID: uuid.NewString(), ManifestSHA256: strings.Repeat("a", 64),
		Listing: models.DiscoveryListingInput{UUID: uuid.NewString(), AccountUUID: uuid.NewString(), CollectionUUID: uuid.NewString(), CollectionRevision: 2,
			ProfileURL: "https://www.reddit.com/user/example/submitted/", PolicySHA256: strings.Repeat("b", 64), ExtractorVersion: "1.32.15-dev",
			InitialCursor: map[string]string{"after": "t3_saved"}, HistoricalPages: 67,
			Legacy: &models.DiscoveryListingLegacy{SnapshotUUID: uuid.NewString(), AccountOrdinal: 5}, NotBefore: time.Date(2026, 10, 4, 1, 0, 0, 0, time.FixedZone("source", -7*3600))},
		Targets: []models.DiscoveryActivationSelection{{SourceOrdinal: 11, SourceSHA256: strings.Repeat("c", 64)}, {SourceOrdinal: 7, SourceSHA256: strings.Repeat("d", 64)}}}
}

func TestDiscoveryActivationCanonicalInputDoesNotAliasCallerValues(t *testing.T) {
	input := discoveryActivationExample()
	prepared, digest, err := archive.PrepareDiscoveryActivation(input)
	require.NoError(t, err)
	require.EqualValues(t, 7, prepared.Targets[0].SourceOrdinal)
	require.EqualValues(t, 11, input.Targets[0].SourceOrdinal)
	_, replay, err := archive.PrepareDiscoveryActivation(prepared)
	require.NoError(t, err)
	require.Equal(t, digest, replay)
	input.Listing.InitialCursor["after"] = "t3_changed"
	input.Listing.Legacy.AccountOrdinal++
	input.Targets[0].SourceOrdinal++
	require.Equal(t, "t3_saved", prepared.Listing.InitialCursor["after"])
	require.EqualValues(t, 5, prepared.Listing.Legacy.AccountOrdinal)
	require.EqualValues(t, 11, prepared.Targets[1].SourceOrdinal)
	require.Equal(t, time.UTC, prepared.Listing.NotBefore.Location())
}

func TestDiscoveryActivationPlanRejectsChangedIdentityAndUnboundedSelections(t *testing.T) {
	input, _, err := archive.PrepareDiscoveryActivation(discoveryActivationExample())
	require.NoError(t, err)
	_, listingSHA, err := archive.PrepareDiscoveryListing(input.Listing)
	require.NoError(t, err)
	plan := models.DiscoveryActivationPlan{Version: 1, Input: input, ListingSHA256: listingSHA, AccountSHA256: strings.Repeat("e", 64)}
	for _, selected := range input.Targets {
		id, err := archive.DiscoveryTargetIdentity(input.Listing.UUID, selected.SourceOrdinal)
		require.NoError(t, err)
		plan.Entries = append(plan.Entries, models.DiscoveryActivationEntry{DiscoveryActivationSelection: selected, TargetUUID: id, PostUUID: uuid.NewString(), PostRevision: 1})
	}
	plan.PlanSHA256, err = archive.DiscoveryActivationDigest(plan)
	require.NoError(t, err)
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	decoded, err := archive.DecodeDiscoveryActivationPlan(body)
	require.NoError(t, err)
	require.Equal(t, plan, *decoded)
	plan.Entries[0].TargetUUID = uuid.NewString()
	plan.PlanSHA256, err = archive.DiscoveryActivationDigest(plan)
	require.NoError(t, err)
	body, err = json.Marshal(plan)
	require.NoError(t, err)
	_, err = archive.DecodeDiscoveryActivationPlan(body)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid, "re-hashing cannot legitimize a different target identity")
	input.Targets = []models.DiscoveryActivationSelection{input.Targets[0], input.Targets[0]}
	_, _, err = archive.PrepareDiscoveryActivation(input)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	input.Targets = make([]models.DiscoveryActivationSelection, archive.MaxDiscoveryActivationTargets+1)
	_, _, err = archive.PrepareDiscoveryActivation(input)
	require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
}

func TestDiscoveryRecoveryDefinitionPinsOriginalAndRequiresFreshCursor(t *testing.T) {
	input := discoveryActivationExample()
	input.Listing.InitialCursor, input.Listing.HistoricalPages = nil, 0
	input.Listing.RecoveryOf = &models.DiscoveryListingRecovery{ListingUUID: uuid.NewString(), SHA256: strings.Repeat("e", 64)}
	prepared, originalSHA, err := archive.PrepareDiscoveryActivation(input)
	require.NoError(t, err)
	input.Listing.RecoveryOf.SHA256 = strings.Repeat("f", 64)
	_, changedSHA, err := archive.PrepareDiscoveryActivation(input)
	require.NoError(t, err)
	require.NotEqual(t, originalSHA, changedSHA)
	require.Equal(t, strings.Repeat("e", 64), prepared.Listing.RecoveryOf.SHA256, "caller changes cannot alter a reviewed reference")
	for _, change := range []func(*models.DiscoveryListingInput){
		func(i *models.DiscoveryListingInput) { i.Legacy = nil },
		func(i *models.DiscoveryListingInput) { i.InitialCursor = map[string]string{"after": "t3_saved"} },
		func(i *models.DiscoveryListingInput) { i.HistoricalPages = 67 },
		func(i *models.DiscoveryListingInput) { i.RecoveryOf.ListingUUID = i.UUID },
		func(i *models.DiscoveryListingInput) { i.RecoveryOf.SHA256 = "bad" },
	} {
		listing := prepared.Listing
		reference := *listing.RecoveryOf
		listing.RecoveryOf = &reference
		change(&listing)
		_, _, err := archive.PrepareDiscoveryListing(listing)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
	}
}
