package models

import (
	"context"
	"time"
)

type DiscoveryTargetInput struct {
	ListingUUID          string `json:"listing_uuid"`
	SourceOrdinal        int64  `json:"source_ordinal"`
	ExpectedSourceSHA256 string `json:"expected_source_sha256"`
	ExpectedPostRevision int    `json:"expected_post_revision"`
}

// A target retains the original held input and reviewed native post revision.
// EnumerationComplete means its retained listing pages were compared through
// the final cursor, not that a post identity was accepted or imported.
type DiscoveryMatchTarget struct {
	UUID                string    `json:"uuid" db:"uuid"`
	ListingUUID         string    `json:"listing_uuid" db:"listing_uuid"`
	SnapshotUUID        string    `json:"snapshot_uuid" db:"snapshot_uuid"`
	SourceOrdinal       int64     `json:"source_ordinal" db:"source_ordinal"`
	SourceSHA256        string    `json:"source_sha256" db:"source_sha256"`
	PostUUID            string    `json:"post_uuid" db:"post_uuid"`
	PostRevision        int       `json:"post_revision" db:"post_revision"`
	Policy              string    `json:"policy" db:"policy"`
	Revision            int       `json:"revision" db:"revision"`
	LastPage            int       `json:"last_page" db:"last_page"`
	EnumerationComplete bool      `json:"enumeration_complete" db:"enumeration_complete"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time `json:"updated_at" db:"updated_at"`
}

type DiscoveryMatchReceipt struct {
	TargetUUID     string    `json:"target_uuid" db:"target_uuid"`
	ListingUUID    string    `json:"listing_uuid" db:"listing_uuid"`
	PageOrdinal    int       `json:"page_ordinal" db:"page_ordinal"`
	PageSHA256     string    `json:"page_sha256" db:"page_sha256"`
	MatchesSHA256  string    `json:"matches_sha256" db:"matches_sha256"`
	CandidateCount int       `json:"candidate_count" db:"candidate_count"`
	Complete       bool      `json:"complete" db:"complete"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type DiscoveryMatchCandidate struct {
	Sequence    int64  `json:"sequence" db:"id"`
	TargetUUID  string `json:"target_uuid" db:"target_uuid"`
	Namespace   string `json:"namespace" db:"namespace"`
	Value       string `json:"value" db:"value"`
	FirstPage   int    `json:"first_page" db:"first_page"`
	BestPage    int    `json:"best_page" db:"best_page"`
	LastPage    int    `json:"last_page" db:"last_page"`
	PageCount   int    `json:"page_count" db:"page_count"`
	URL         string `json:"url" db:"url"`
	Basis       string `json:"basis" db:"basis"`
	NeedsDetail bool   `json:"needs_detail" db:"needs_detail"`
}

type DiscoveryMatchEvidence struct {
	TargetUUID     string `json:"target_uuid"`
	PageOrdinal    int    `json:"page_ordinal"`
	Namespace      string `json:"namespace"`
	Value          string `json:"value"`
	URL            string `json:"url"`
	Basis          string `json:"basis"`
	NeedsDetail    bool   `json:"needs_detail"`
	RecordOrdinals []int  `json:"record_ordinals"`
}

type DiscoveryComparisonCandidate struct {
	UUID      string `json:"uuid"`
	AfterPage int    `json:"after_page"`
}

type DiscoveryComparisonCandidates struct {
	Targets []DiscoveryComparisonCandidate `json:"targets"`
	After   string                         `json:"after"`
	HasMore bool                           `json:"has_more"`
}

// After advances over inspected rows, including incomplete or blocked targets.
// Readiness is not a durable approval; publication rechecks the original binding.
type DiscoveryPublicationCandidates struct {
	Targets []DiscoveryPublicationInput `json:"targets"`
	After   string                      `json:"after"`
	HasMore bool                        `json:"has_more"`
}

// Coverage describes the saved search, not whether the source still exposes
// every post it has ever hosted. Complete requires comparing a retained search
// from its beginning through its final cursor. An imported page count cannot
// supply the missing bodies from before a saved cursor.
type DiscoveryMatchCoverage struct {
	HistoricalPages     int64 `json:"historical_pages"`
	StartsAtSavedCursor bool  `json:"starts_at_saved_cursor"`
	RetainedPages       int   `json:"retained_pages"`
	RetainedComplete    bool  `json:"retained_complete"`
	Complete            bool  `json:"complete"`
}

// Review is a read-only view of one target in the current transaction. Without
// Publication, empty Blockers identify a unique candidate for further validation,
// not acceptance or a durable review token. Published work keeps its receipt.
type DiscoveryMatchReview struct {
	Target               DiscoveryMatchTarget       `json:"target"`
	Coverage             DiscoveryMatchCoverage     `json:"coverage"`
	CurrentPost          *SourcePost                `json:"current_post"`
	CandidateCount       int                        `json:"candidate_count"`
	DetailCandidateCount int                        `json:"detail_candidate_count"`
	Candidate            *DiscoveryMatchCandidate   `json:"candidate,omitempty"`
	CandidatePost        *SourcePost                `json:"candidate_post,omitempty"`
	Publication          *DiscoveryMatchPublication `json:"publication,omitempty"`
	Blockers             []string                   `json:"blockers"`
}

type DiscoveryMatchReaderWriter interface {
	PreviewActivation(context.Context, DiscoveryActivationInput) (*DiscoveryActivationPlan, error)
	Activate(context.Context, DiscoveryActivationInput, string, time.Time) (*DiscoveryActivation, error)
	Activation(context.Context, string) (*DiscoveryActivation, error)
	BindTarget(context.Context, DiscoveryTargetInput, time.Time) (*DiscoveryMatchTarget, error)
	Target(context.Context, string) (*DiscoveryMatchTarget, error)
	Review(context.Context, string) (*DiscoveryMatchReview, error)
	PreparePublication(context.Context, DiscoveryPublicationInput) (PreparedDiscoveryPublication, error)
	Publication(context.Context, string) (*DiscoveryMatchPublication, error)
	PublishedRecords(context.Context, string, int, int) ([]DiscoveryPublishedRecord, error)
	PendingPublications(context.Context, string, int, time.Time) (*DiscoveryPublicationCandidates, error)
	Pending(context.Context, string, int, time.Time) (*DiscoveryComparisonCandidates, error)
	Prepare(context.Context, string, int, time.Time) (PreparedDiscoveryComparison, error)
	Advance(context.Context, string, int, time.Time) (*DiscoveryMatchReceipt, error)
	Receipt(context.Context, string, int) (*DiscoveryMatchReceipt, error)
	Candidates(context.Context, string, int64, int) ([]DiscoveryMatchCandidate, error)
	Evidence(context.Context, int64, int, int) ([]DiscoveryMatchEvidence, error)
}

// The repository retains the immutable comparison inputs privately. Preparing
// under a read transaction keeps source decoding out of the database writer;
// Commit checks the original target, page and current source bindings again.
type PreparedDiscoveryComparison interface {
	Commit(context.Context, time.Time) (*DiscoveryMatchReceipt, error)
}
