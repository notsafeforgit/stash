package models

import "time"

type DiscoveryActivationSelection struct {
	SourceOrdinal int64  `json:"source_ordinal"`
	SourceSHA256  string `json:"source_sha256"`
}

// Activation binds reviewed targets to a shared listing. Producers separately
// admit its page jobs; activation is neither a fetch nor accepted post identity.
type DiscoveryActivationInput struct {
	UUID           string                         `json:"uuid"`
	ManifestSHA256 string                         `json:"manifest_sha256"`
	Listing        DiscoveryListingInput          `json:"listing"`
	Targets        []DiscoveryActivationSelection `json:"targets"`
}

type DiscoveryActivationEntry struct {
	DiscoveryActivationSelection
	TargetUUID   string `json:"target_uuid"`
	PostUUID     string `json:"post_uuid"`
	PostRevision int    `json:"post_revision"`
}

type DiscoveryActivationPlan struct {
	Version       int                        `json:"version"`
	Input         DiscoveryActivationInput   `json:"input"`
	ListingSHA256 string                     `json:"listing_sha256"`
	AccountSHA256 string                     `json:"account_sha256"`
	Entries       []DiscoveryActivationEntry `json:"entries"`
	PlanSHA256    string                     `json:"plan_sha256,omitempty"`
}

type DiscoveryActivation struct {
	DiscoveryActivationPlan
	CreatedAt time.Time `json:"created_at"`
}
