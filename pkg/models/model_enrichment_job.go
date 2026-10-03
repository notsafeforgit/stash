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

type EnrichmentJobReaderWriter interface {
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
}
