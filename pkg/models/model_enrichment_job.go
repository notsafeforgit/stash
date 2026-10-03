package models

import (
	"context"
	"encoding/json"
	"time"
)

// Arguments pin reviewed source/target revisions and the external runtime.
// Website credentials and deployment-specific paths are never job arguments.
type EnrichmentJobArguments struct {
	Version            int     `json:"version"`
	TargetUUID         string  `json:"target_uuid"`
	TargetRevision     int     `json:"target_revision"`
	PostUUID           string  `json:"post_uuid"`
	CollectionUUID     string  `json:"collection_uuid"`
	CollectionRevision int     `json:"collection_revision"`
	RootUUID           *string `json:"root_uuid"`
	PolicySHA256       string  `json:"policy_sha256"`
	ExtractorVersion   string  `json:"extractor_version"`
}

type EnrichmentJobBinding struct {
	JobUUID        string `json:"job_uuid" db:"job_uuid"`
	TargetUUID     string `json:"target_uuid" db:"target_uuid"`
	TargetRevision int    `json:"target_revision" db:"target_revision"`
}

type EnrichmentJobCandidate struct {
	Sequence int64  `json:"sequence"`
	UUID     string `json:"uuid"`
}

type EnrichmentMaintenanceResult struct {
	Recovered int `json:"recovered"`
	Cancelled int `json:"cancelled"`
}

type EnrichmentJobLease struct {
	ArchiveJobLease
	ProducerUUID string `json:"producer_uuid"`
}

type EnrichmentJobAttempt struct {
	JobUUID      string `json:"job_uuid" db:"job_uuid"`
	Fence        int64  `json:"fence" db:"fence"`
	ProducerUUID string `json:"producer_uuid" db:"producer_uuid"`
}

// Receipts are small, immutable acknowledgements. Only the current compact
// body is stored; previously accepted record digests/producers stay immutable.
type EnrichmentCheckpointReceipt struct {
	JobUUID         string    `json:"job_uuid" db:"job_uuid"`
	Revision        int       `json:"revision" db:"revision"`
	Digest          string    `json:"sha256" db:"digest"`
	Fence           int64     `json:"fence" db:"fence"`
	RecordCount     int       `json:"record_count" db:"record_count"`
	PendingCount    int       `json:"pending_count" db:"pending_count"`
	UnresolvedCount int       `json:"unresolved_count" db:"unresolved_count"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

type EnrichmentCheckpoint struct {
	EnrichmentCheckpointReceipt
	Body json.RawMessage `json:"body"`
}

type EnrichmentCheckpointRecord struct {
	JobUUID            string `json:"job_uuid" db:"job_uuid"`
	Ordinal            int    `json:"ordinal" db:"ordinal"`
	CheckpointRevision int    `json:"checkpoint_revision" db:"checkpoint_revision"`
	Digest             string `json:"sha256" db:"digest"`
	Fence              int64  `json:"fence" db:"fence"`
	ProducerUUID       string `json:"producer_uuid" db:"producer_uuid"`
}

type EnrichmentPublication struct {
	JobUUID            string    `json:"job_uuid" db:"job_uuid"`
	CheckpointRevision int       `json:"checkpoint_revision" db:"checkpoint_revision"`
	Digest             string    `json:"checkpoint_sha256" db:"digest"`
	Fence              int64     `json:"fence" db:"fence"`
	CompletionUUID     string    `json:"completion_uuid" db:"completion_uuid"`
	RecordCount        int       `json:"record_count" db:"record_count"`
	CaptureCount       int       `json:"capture_count" db:"capture_count"`
	UnresolvedCount    int       `json:"unresolved_count" db:"unresolved_count"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
}

type EnrichmentPublishedRecord struct {
	EnrichmentCheckpointRecord
	CaptureUUID string `json:"capture_uuid" db:"capture_uuid"`
}

type EnrichmentReference struct {
	URL    string `json:"url"`
	Parent int    `json:"parent"`
	Depth  int    `json:"depth"`
	Reason string `json:"reason"`
}

// Releasing staging preserves native captures, original acknowledgement hashes
// and unresolved references. The proof binds those retained records together;
// it is not a promise that the original compact transcript can be reconstructed.
type EnrichmentCheckpointRelease struct {
	JobUUID         string                `json:"job_uuid" db:"job_uuid"`
	Version         int                   `json:"version" db:"version"`
	ProofSHA256     string                `json:"proof_sha256" db:"proof_sha256"`
	CheckpointBytes int                   `json:"checkpoint_bytes" db:"checkpoint_bytes"`
	CreatedAt       time.Time             `json:"created_at" db:"created_at"`
	Unresolved      []EnrichmentReference `json:"unresolved" db:"-"`
}

type EnrichmentJobReaderWriter interface {
	Ready(context.Context, string, string, string, int64, int, time.Time) ([]EnrichmentJobCandidate, error)
	Maintain(context.Context, time.Time) (*EnrichmentMaintenanceResult, error)
	Bind(context.Context, string, time.Time) error
	Binding(context.Context, string) (*EnrichmentJobBinding, error)
	TargetBinding(context.Context, string, int) (*EnrichmentJobBinding, error)
	BindAttempt(context.Context, EnrichmentJobLease, time.Time) error
	Attempt(context.Context, string, int64) (*EnrichmentJobAttempt, error)
	CheckLease(context.Context, EnrichmentJobLease, time.Time) (*ArchiveJob, error)
	Checkpoint(context.Context, EnrichmentJobLease, int, json.RawMessage, time.Time) (*EnrichmentCheckpointReceipt, error)
	CheckpointHead(context.Context, string) (*EnrichmentCheckpoint, error)
	CheckpointReceipts(context.Context, string, int, int) ([]EnrichmentCheckpointReceipt, error)
	CheckpointRecords(context.Context, string, int, int) ([]EnrichmentCheckpointRecord, error)
	Publish(context.Context, EnrichmentJobLease, int, string, []string, time.Time) (*EnrichmentPublication, error)
	Publication(context.Context, string) (*EnrichmentPublication, error)
	PublishedRecords(context.Context, string, int, int) ([]EnrichmentPublishedRecord, error)
	ReleaseCheckpoint(context.Context, string, time.Time) (*EnrichmentCheckpointRelease, error)
	CheckpointRelease(context.Context, string) (*EnrichmentCheckpointRelease, error)
}
