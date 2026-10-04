package archive

import (
	"bytes"
	"cmp"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxDiscoveryActivationTargets = 1000
const MaxDiscoveryActivationBytes = 4 << 20

func DiscoveryTargetIdentity(listing string, ordinal int64) (string, error) {
	if !translationUUID(listing) || ordinal < 1 {
		return "", models.ErrDiscoveryInvalid
	}
	return uuid.NewSHA1(uuid.MustParse(listing), []byte("retained-discovery-target-v1\x00"+strconv.FormatInt(ordinal, 10))).String(), nil
}

func PrepareDiscoveryActivation(input models.DiscoveryActivationInput) (models.DiscoveryActivationInput, string, error) {
	if !translationUUID(input.UUID) || !ValidSHA256(input.ManifestSHA256) || input.Listing.Legacy == nil || len(input.Targets) == 0 || len(input.Targets) > MaxDiscoveryActivationTargets {
		return input, "", models.ErrDiscoveryInvalid
	}
	body, _, err := PrepareDiscoveryListing(input.Listing)
	if err != nil {
		return input, "", err
	}
	// Detach pointer/map fields and normalize the deadline before hashing.
	input.Listing = models.DiscoveryListingInput{}
	if err := json.Unmarshal(body, &input.Listing); err != nil {
		return input, "", err
	}
	input.Targets = slices.Clone(input.Targets)
	slices.SortFunc(input.Targets, func(a, b models.DiscoveryActivationSelection) int {
		return cmp.Compare(a.SourceOrdinal, b.SourceOrdinal)
	})
	previous := int64(0)
	for _, target := range input.Targets {
		if target.SourceOrdinal <= previous || !ValidSHA256(target.SourceSHA256) {
			return input, "", models.ErrDiscoveryInvalid
		}
		previous = target.SourceOrdinal
	}
	body, err = json.Marshal(input)
	return input, translationDigest(body), err
}

func DiscoveryActivationDigest(plan models.DiscoveryActivationPlan) (string, error) {
	plan.PlanSHA256 = ""
	body, err := json.Marshal(plan)
	if err != nil || len(body) > MaxDiscoveryActivationBytes {
		return "", models.ErrDiscoveryInvalid
	}
	return translationDigest(body), nil
}

func DecodeDiscoveryActivationPlan(body []byte) (*models.DiscoveryActivationPlan, error) {
	if _, err := DecodeJSONObject(body, MaxDiscoveryActivationBytes); err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	var plan models.DiscoveryActivationPlan
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	input, _, err := PrepareDiscoveryActivation(plan.Input)
	if err != nil || plan.Version != 1 || !reflect.DeepEqual(input, plan.Input) || len(plan.Entries) != len(input.Targets) || !ValidSHA256(plan.AccountSHA256) {
		return nil, models.ErrDiscoveryInvalid
	}
	_, listingSHA, err := PrepareDiscoveryListing(input.Listing)
	if err != nil || plan.ListingSHA256 != listingSHA {
		return nil, models.ErrDiscoveryInvalid
	}
	for i, entry := range plan.Entries {
		id, err := DiscoveryTargetIdentity(input.Listing.UUID, entry.SourceOrdinal)
		if err != nil || entry.DiscoveryActivationSelection != input.Targets[i] || entry.TargetUUID != id || !translationUUID(entry.PostUUID) || entry.PostRevision < 1 {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	expected, err := DiscoveryActivationDigest(plan)
	if err != nil || plan.PlanSHA256 != expected {
		return nil, models.ErrDiscoveryInvalid
	}
	return &plan, nil
}
