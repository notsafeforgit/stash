package models

import "time"

type EnrichmentTargetRef struct {
	TargetUUID string `json:"target_uuid" db:"target_uuid"`
	Revision   int    `json:"revision" db:"target_revision"`
}

type EnrichmentActivationSelection struct {
	EnrichmentTargetRef
	CollectionRevision int `json:"collection_revision"`
}

// Activation releases reviewed holds without changing deadlines, admitting jobs,
// or claiming that the metadata collector has run.
type EnrichmentActivationInput struct {
	UUID           string                          `json:"uuid"`
	SnapshotUUID   string                          `json:"snapshot_uuid,omitempty"`
	ManifestSHA256 string                          `json:"manifest_sha256,omitempty"`
	Targets        []EnrichmentActivationSelection `json:"targets"`
}

type EnrichmentActivationEntry struct {
	EnrichmentTargetRef
	ReleasedTargetUUID           string    `json:"released_target_uuid" db:"-"`
	ReleasedRevision             int       `json:"released_revision" db:"-"`
	ActivationCollectionRevision int       `json:"activation_collection_revision" db:"-"`
	PostUUID                     string    `json:"post_uuid" db:"post_uuid"`
	PostRevision                 int       `json:"post_revision" db:"post_revision"`
	URLUUID                      string    `json:"url_uuid" db:"url_uuid"`
	URL                          string    `json:"url" db:"url"`
	CollectionUUID               string    `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision           int       `json:"collection_revision" db:"collection_revision"`
	Policy                       string    `json:"policy" db:"policy"`
	Priority                     int       `json:"priority" db:"priority"`
	NotBefore                    time.Time `json:"not_before" db:"not_before"`
}

type EnrichmentActivationPlan struct {
	Version    int                         `json:"version"`
	Input      EnrichmentActivationInput   `json:"input"`
	Entries    []EnrichmentActivationEntry `json:"entries"`
	PlanSHA256 string                      `json:"plan_sha256,omitempty"`
}

type EnrichmentActivation struct {
	EnrichmentActivationPlan
	Activated []EnrichmentTargetRef `json:"activated"`
	CreatedAt time.Time             `json:"created_at"`
}

type AutomationEnrichmentCandidate struct {
	EnrichmentActivationEntry
	Ordinal                   int64  `json:"ordinal" db:"ordinal"`
	CurrentRevision           int    `json:"current_revision" db:"current_revision"`
	State                     string `json:"state" db:"state"`
	PostState                 string `json:"post_state" db:"post_state"`
	CurrentCollectionRevision int    `json:"current_collection_revision" db:"current_collection_revision"`
	CollectionState           string `json:"collection_state" db:"collection_state"`
	Disposition               string `json:"disposition" db:"-"`
}
