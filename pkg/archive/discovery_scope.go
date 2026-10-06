package archive

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

const MaxDiscoveryScopeBytes = 128 << 10

func PrepareDiscoveryScope(input models.DiscoveryScopeInput) (string, error) {
	if !translationUUID(input.UUID) || !translationUUID(input.ListingUUID) ||
		!ValidSHA256(input.ExpectedDefinitionSHA256) || input.CollectionRevision < 1 ||
		len(input.Reason) > 4096 || !utf8.ValidString(input.Reason) {
		return "", models.ErrDiscoveryInvalid
	}
	body, err := json.Marshal(input)
	return translationDigest(body), err
}

func DiscoveryScopeDefinition(plan models.DiscoveryScopePlan) (json.RawMessage, string, error) {
	input := plan.Previous.DiscoveryListingInput
	input.CollectionRevision, input.RootUUID = plan.Collection.Revision, plan.Collection.RootUUID
	return PrepareDiscoveryListing(input)
}

func DiscoveryScopeDigest(plan models.DiscoveryScopePlan) (string, error) {
	plan.PlanSHA256 = ""
	body, err := json.Marshal(plan)
	if err != nil || len(body) > MaxDiscoveryScopeBytes {
		return "", models.ErrDiscoveryInvalid
	}
	return translationDigest(body), nil
}

func DecodeDiscoveryScopePlan(body []byte) (*models.DiscoveryScopePlan, error) {
	if _, err := DecodeJSONObject(body, MaxDiscoveryScopeBytes); err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	var plan models.DiscoveryScopePlan
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	_, err := PrepareDiscoveryScope(plan.Input)
	if err != nil || plan.Version != 1 || plan.Previous.UUID != plan.Input.ListingUUID ||
		plan.Previous.Digest != plan.Input.ExpectedDefinitionSHA256 ||
		plan.Collection.UUID != plan.Previous.CollectionUUID || plan.Collection.State != "active" ||
		plan.Collection.Revision != plan.Input.CollectionRevision || plan.Collection.Revision <= plan.Previous.CollectionRevision ||
		(plan.PreviousReviewUUID != nil && (!translationUUID(*plan.PreviousReviewUUID) || *plan.PreviousReviewUUID == plan.Input.UUID)) ||
		plan.Previous.CreatedAt.UnixMilli() <= 0 || plan.Collection.CreatedAt.UnixMilli() <= 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	_, previous, err := PrepareDiscoveryListing(plan.Previous.DiscoveryListingInput)
	if err != nil || previous != plan.Previous.Digest {
		return nil, models.ErrDiscoveryInvalid
	}
	_, next, err := DiscoveryScopeDefinition(plan)
	if err != nil || next != plan.DefinitionSHA256 {
		return nil, models.ErrDiscoveryInvalid
	}
	digest, err := DiscoveryScopeDigest(plan)
	if err != nil || digest != plan.PlanSHA256 {
		return nil, models.ErrDiscoveryInvalid
	}
	return &plan, nil
}
