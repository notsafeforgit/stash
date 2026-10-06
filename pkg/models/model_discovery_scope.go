package models

import "time"

// A scope review changes only the collection binding of an unstarted search.
// Original definitions, legacy cursors, targets and recovery references remain.
type DiscoveryScopeInput struct {
	UUID                     string `json:"uuid"`
	ListingUUID              string `json:"listing_uuid"`
	ExpectedDefinitionSHA256 string `json:"expected_definition_sha256"`
	CollectionRevision       int    `json:"collection_revision"`
	Reason                   string `json:"reason"`
}

type DiscoveryScopePlan struct {
	Version            int                 `json:"version"`
	Input              DiscoveryScopeInput `json:"input"`
	PreviousReviewUUID *string             `json:"previous_review_uuid"`
	Previous           DiscoveryListing    `json:"previous"`
	Collection         SourceCollection    `json:"collection"`
	DefinitionSHA256   string              `json:"definition_sha256"`
	PlanSHA256         string              `json:"plan_sha256,omitempty"`
}

type DiscoveryScopeReview struct {
	DiscoveryScopePlan
	CreatedAt time.Time `json:"created_at"`
}

type DiscoveryScopeCandidate struct {
	Listing                   DiscoveryListing `json:"listing"`
	CurrentCollectionRevision int              `json:"current_collection_revision"`
	CollectionState           string           `json:"collection_state"`
	Disposition               string           `json:"disposition"`
}
