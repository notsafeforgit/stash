package models

import (
	"encoding/json"
	"time"
)

// A handoff reviews the exact starting context for child-only extraction. It is
// not an execution receipt: accepting it does not claim a lease, release the
// target's hold, or attribute historical observations to a new producer.
type CheckpointHandoffInput struct {
	UUID               string `json:"uuid"`
	EvidenceUUID       string `json:"evidence_uuid"`
	EvidencePlanSHA256 string `json:"evidence_plan_sha256"`
	TargetRevision     int    `json:"target_revision"`
	CollectionRevision int    `json:"collection_revision"`
	PolicySHA256       string `json:"policy_sha256"`
	ExtractorVersion   string `json:"extractor_version"`
}

type CheckpointHandoffPlan struct {
	Version            int                      `json:"version"`
	Input              CheckpointHandoffInput   `json:"input"`
	Target             EnrichmentTarget         `json:"target"`
	PostRevision       int                      `json:"post_revision"`
	Collection         SourceCollectionRevision `json:"collection"`
	EvidenceCreatedAt  time.Time                `json:"evidence_created_at"`
	CapturePolicy      string                   `json:"capture_policy"`
	ReleasedTargetUUID string                   `json:"released_target_uuid"`
	ReleasedRevision   int                      `json:"released_revision"`
	SeedSHA256         string                   `json:"seed_sha256"`
	SeedBytes          int                      `json:"seed_bytes"`
	RetainedCount      int                      `json:"retained_capture_count"`
	PendingCount       int                      `json:"pending_count"`
	UnscopedCount      int                      `json:"unscoped_reference_count"`
	PlanSHA256         string                   `json:"plan_sha256,omitempty"`
}

type CheckpointHandoff struct {
	CheckpointHandoffPlan
	CreatedAt time.Time `json:"created_at"`
}

// The seed is reconstructed from frozen staging and verified native captures;
// it is never persisted as a fabricated worker checkpoint acknowledgement.
type CheckpointHandoffSeed struct {
	HandoffUUID string          `json:"handoff_uuid"`
	PlanSHA256  string          `json:"plan_sha256"`
	SHA256      string          `json:"sha256"`
	Body        json.RawMessage `json:"body"`
}
