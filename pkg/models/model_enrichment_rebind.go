package models

import "time"

// Rebinding reviews unstarted pending work against a newer definition of the
// same collection. It neither changes historical provenance nor admits a job.
type EnrichmentRebindInput struct {
	UUID               string                `json:"uuid"`
	CollectionUUID     string                `json:"collection_uuid"`
	CollectionRevision int                   `json:"collection_revision"`
	Targets            []EnrichmentTargetRef `json:"targets"`
	Reason             string                `json:"reason"`
}

type EnrichmentRebindPlan struct {
	Version    int                      `json:"version"`
	Input      EnrichmentRebindInput    `json:"input"`
	Collection SourceCollection         `json:"collection"`
	Activation EnrichmentActivationPlan `json:"activation"`
	PlanSHA256 string                   `json:"plan_sha256,omitempty"`
}

type EnrichmentRebinding struct {
	EnrichmentRebindPlan
	CreatedAt time.Time `json:"created_at"`
}

type EnrichmentRebindCursor struct {
	CollectionRevision int    `json:"collection_revision"`
	TargetUUID         string `json:"target_uuid"`
}

type EnrichmentRebindCandidate struct {
	Target                    EnrichmentTarget `json:"target"`
	CurrentCollectionRevision int              `json:"current_collection_revision"`
	CollectionState           string           `json:"collection_state"`
	PostState                 string           `json:"post_state"`
	PostRevision              int              `json:"post_revision"`
	ReleasedTargetUUID        string           `json:"released_target_uuid"`
	Disposition               string           `json:"disposition"`
}
