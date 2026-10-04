package models

import (
	"context"
	"encoding/json"
	"time"
)

// The selected URL is a fetch destination, not an accepted post association.
// Each job pins the original frozen evidence and the particular listing record
// that made this post a candidate. The detail runtime has its own policy.
type DiscoveryDetailJobArguments struct {
	Version            int     `json:"version"`
	Generation         int     `json:"generation"`
	TargetUUID         string  `json:"target_uuid"`
	TargetRevision     int     `json:"target_revision"`
	SourceSHA256       string  `json:"source_sha256"`
	PostUUID           string  `json:"post_uuid"`
	PostRevision       int     `json:"post_revision"`
	CandidateSequence  int64   `json:"candidate_sequence"`
	Namespace          string  `json:"post_namespace"`
	Value              string  `json:"post_value"`
	URL                string  `json:"url"`
	ListingUUID        string  `json:"listing_uuid"`
	DefinitionSHA256   string  `json:"definition_sha256"`
	PageOrdinal        int     `json:"page_ordinal"`
	PageSHA256         string  `json:"page_sha256"`
	CollectionUUID     string  `json:"collection_uuid"`
	CollectionRevision int     `json:"collection_revision"`
	RootUUID           *string `json:"root_uuid"`
	PolicySHA256       string  `json:"policy_sha256"`
	ExtractorVersion   string  `json:"extractor_version"`
	CapturePolicy      string  `json:"capture_policy"`
}

func (a DiscoveryDetailJobArguments) PostIdentifier() SourcePostIdentifier {
	return SourcePostIdentifier{Namespace: a.Namespace, Value: a.Value}
}

type DiscoveryDetailAdmission struct {
	TargetUUID             string `json:"target_uuid"`
	ExpectedTargetRevision int    `json:"expected_target_revision"`
	CandidateSequence      int64  `json:"candidate_sequence"`
	PolicySHA256           string `json:"policy_sha256"`
	ExtractorVersion       string `json:"extractor_version"`
}

// Success here means the saved transcript was compared. It does not publish
// captures or identities, clear review blockers or complete a catalog import.
type DiscoveryDetailResult struct {
	JobUUID            string                  `json:"job_uuid" db:"job_uuid"`
	CheckpointRevision int                     `json:"checkpoint_revision" db:"checkpoint_revision"`
	Fence              int64                   `json:"fence" db:"fence"`
	Evidence           DiscoveryDetailEvidence `json:"evidence" db:"-"`
	CreatedAt          time.Time               `json:"created_at" db:"created_at"`
}

type DiscoveryDetailReaderWriter interface {
	Collections(context.Context, EnrichmentCollectionQuery, time.Time) ([]EnrichmentCollectionCandidate, error)
	Admit(context.Context, DiscoveryDetailAdmission, time.Time) (*ArchiveJob, error)
	Retry(context.Context, string, time.Time) (*ArchiveJob, error)
	CheckJob(context.Context, string, time.Time) (*ArchiveJob, error)
	BindAttempt(context.Context, EnrichmentJobLease, time.Time) error
	Attempt(context.Context, string, int64) (*EnrichmentJobAttempt, error)
	CheckLease(context.Context, EnrichmentJobLease, time.Time) (*ArchiveJob, error)
	ReserveSource(context.Context, EnrichmentJobLease, string, time.Time) (bool, error)
	Checkpoint(context.Context, EnrichmentJobLease, int, json.RawMessage, time.Time) (*EnrichmentCheckpointReceipt, error)
	CheckpointHead(context.Context, string) (*EnrichmentCheckpoint, error)
	CheckpointReceipts(context.Context, string, int, int) ([]EnrichmentCheckpointReceipt, error)
	CheckpointRecords(context.Context, string, int, int) ([]EnrichmentCheckpointRecord, error)
	Complete(context.Context, EnrichmentJobLease, int, string, time.Time) (*DiscoveryDetailResult, error)
	Result(context.Context, string) (*DiscoveryDetailResult, error)
	Ready(context.Context, string, string, string, int64, int, time.Time) ([]DiscoveryJobCandidate, error)
	Maintain(context.Context, time.Time) (*DiscoveryMaintenanceResult, error)
}
