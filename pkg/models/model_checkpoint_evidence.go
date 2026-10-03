package models

import "time"

// Evidence acceptance preserves saved metadata before execution handoff. It
// leaves the target in review and never claims that a native producer ran.
type CheckpointEvidenceInput struct {
	UUID           string `json:"uuid"`
	SnapshotUUID   string `json:"snapshot_uuid"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Ordinal        int64  `json:"ordinal"`
	TargetRevision int    `json:"target_revision"`
}

type CheckpointEvidenceCapture struct {
	BodyIndex   int    `json:"body_index" db:"body_index"`
	CaptureUUID string `json:"capture_uuid" db:"capture_uuid"`
	PayloadSHA  string `json:"payload_sha256" db:"payload_sha256"`
}

type CheckpointEvidencePlan struct {
	Version       int                         `json:"version"`
	Input         CheckpointEvidenceInput     `json:"input"`
	SourceSHA256  string                      `json:"source_sha256"`
	StagedSHA256  string                      `json:"staged_sha256"`
	BodySHA256    string                      `json:"body_sha256"`
	Target        EnrichmentTarget            `json:"target"`
	PostRevision  int                         `json:"post_revision"`
	PostNamespace string                      `json:"post_namespace"`
	PostValue     string                      `json:"post_value"`
	AssignPostID  bool                        `json:"assign_post_identifier"`
	Captures      []CheckpointEvidenceCapture `json:"captures"`
	RecordCount   int                         `json:"record_count"`
	PendingCount  int                         `json:"pending_count"`
	UnscopedCount int                         `json:"unscoped_reference_count"`
	PlanSHA256    string                      `json:"plan_sha256,omitempty"`
}

type CheckpointEvidenceAcceptance struct {
	CheckpointEvidencePlan
	CreatedAt time.Time `json:"created_at"`
}
