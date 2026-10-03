package models

import (
	"context"
	"errors"
	"time"
)

const EnrichmentGalleryMetadataV1 = "gallery-dl-metadata-v1"

// A target refers to a URL already retained for this post. It does not infer
// an author, select entity metadata, or authorize a media download.
type EnrichmentTargetInput struct {
	PostUUID           string `json:"post_uuid" db:"post_uuid"`
	URLUUID            string `json:"url_uuid" db:"url_uuid"`
	CollectionUUID     string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	Policy             string `json:"policy" db:"policy"`
	Origin             string `json:"origin" db:"origin"`
}

type EnrichmentSchedule struct {
	State     string    `json:"state" db:"state"` // held, pending, review, excluded; completed is output-only
	Priority  int       `json:"priority" db:"priority"`
	NotBefore time.Time `json:"not_before" db:"not_before"`
	Reason    string    `json:"reason" db:"reason"` // bounded machine code, not extractor logs
}

type EnrichmentTarget struct {
	EnrichmentTargetInput
	EnrichmentSchedule
	UUID           string    `json:"uuid" db:"uuid"`
	URL            string    `json:"url" db:"url"`
	Revision       int       `json:"revision" db:"revision"`
	CompletionUUID *string   `json:"completion_uuid,omitempty" db:"completion_uuid"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

type EnrichmentTargetHistory struct {
	EnrichmentSchedule
	TargetUUID     string    `json:"target_uuid" db:"target_uuid"`
	Revision       int       `json:"revision" db:"revision"`
	CompletionUUID *string   `json:"completion_uuid,omitempty" db:"completion_uuid"`
	RecordedAt     time.Time `json:"recorded_at" db:"recorded_at"`
}

type EnrichmentTargetQuery struct {
	PostUUID       string
	CollectionUUID string
	State          string
	After          string
	Limit          int
}

type EnrichmentCompletionInput struct {
	UUID             string   `json:"uuid"`
	TargetUUID       string   `json:"target_uuid"`
	ExpectedRevision int      `json:"expected_revision"`
	CaptureUUIDs     []string `json:"capture_uuids"`
}

// Completion proves retained source evidence for the exact target revision.
// Execution leases and ingestion validation are owned by the calling worker.
type EnrichmentCompletion struct {
	EnrichmentCompletionInput
	CreatedAt time.Time `json:"created_at"`
}

var (
	ErrEnrichmentInvalid  = errors.New("invalid native enrichment work")
	ErrEnrichmentConflict = errors.New("native enrichment work or source changed")
	ErrEnrichmentAtomic   = errors.New("native enrichment work did not finish atomically")
)

type EnrichmentTargetCursor struct {
	Priority  int       `json:"priority"`
	NotBefore time.Time `json:"not_before"`
	UUID      string    `json:"uuid"`
}

type EnrichmentWorkReaderWriter interface {
	RetainTarget(context.Context, EnrichmentTargetInput, EnrichmentSchedule, time.Time) (*EnrichmentTarget, error)
	Target(context.Context, string) (*EnrichmentTarget, error)
	Targets(context.Context, EnrichmentTargetQuery) ([]EnrichmentTarget, error)
	History(context.Context, string, int, int) ([]EnrichmentTargetHistory, error)
	Schedule(context.Context, string, int, EnrichmentSchedule, time.Time) (*EnrichmentTarget, error)
	Retry(context.Context, string, int, time.Time) (*EnrichmentTarget, error)
	Ready(context.Context, string, time.Time, int) ([]EnrichmentTarget, error)
	ReadyPage(context.Context, string, time.Time, *EnrichmentTargetCursor, int) ([]EnrichmentTarget, error)
	Complete(context.Context, EnrichmentCompletionInput, time.Time) (*EnrichmentCompletion, error)
	Completion(context.Context, string) (*EnrichmentCompletion, error)
}
