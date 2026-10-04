package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrDiscoveryInvalid  = errors.New("invalid native source discovery work")
	ErrDiscoveryConflict = errors.New("native discovery definition or cursor changed")
	ErrDiscoveryAtomic   = errors.New("native discovery checkpoint did not finish atomically")
)

// Legacy is original resume evidence, not a transferred process lease or proof
// that the native worker observed the historical pages.
type DiscoveryListingLegacy struct {
	SnapshotUUID   string `json:"snapshot_uuid"`
	AccountOrdinal int64  `json:"account_ordinal"`
}

// Recovery retains the original incomplete search while a new search begins at
// the source's first page. It does not claim to have recovered historical bodies.
type DiscoveryListingRecovery struct {
	ListingUUID string `json:"listing_uuid"`
	SHA256      string `json:"sha256"`
}

type DiscoveryListingInput struct {
	UUID               string                    `json:"uuid"`
	AccountUUID        string                    `json:"account_uuid"`
	CollectionUUID     string                    `json:"collection_uuid"`
	CollectionRevision int                       `json:"collection_revision"`
	RootUUID           *string                   `json:"root_uuid"`
	ProfileURL         string                    `json:"profile_url"`
	PolicySHA256       string                    `json:"policy_sha256"`
	ExtractorVersion   string                    `json:"extractor_version"`
	InitialCursor      map[string]string         `json:"initial_cursor"`
	HistoricalPages    int64                     `json:"historical_pages"`
	Legacy             *DiscoveryListingLegacy   `json:"legacy"`
	NotBefore          time.Time                 `json:"not_before"`
	RecoveryOf         *DiscoveryListingRecovery `json:"recovery_of,omitempty"`
}

// A listing is a bounded source-history search. Its completion does not accept
// any post match, assign depicted performers, or complete catalog migration.
type DiscoveryListing struct {
	DiscoveryListingInput
	Digest    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

type DiscoveryJobArguments struct {
	Version          int    `json:"version"`
	ListingUUID      string `json:"listing_uuid"`
	Generation       int    `json:"generation"`
	PageOrdinal      int    `json:"page_ordinal"`
	DefinitionSHA256 string `json:"definition_sha256"`
	CollectionUUID   string `json:"collection_uuid"`
}

type DiscoveryJobLease struct {
	ArchiveJobLease
	ProducerUUID string `json:"producer_uuid"`
}

type DiscoveryJobAttempt struct {
	JobUUID      string `json:"job_uuid" db:"job_uuid"`
	Fence        int64  `json:"fence" db:"fence"`
	ProducerUUID string `json:"producer_uuid" db:"producer_uuid"`
}

type DiscoveryPageReceipt struct {
	ListingUUID  string    `json:"listing_uuid" db:"listing_uuid"`
	Ordinal      int       `json:"ordinal" db:"ordinal"`
	JobUUID      string    `json:"job_uuid" db:"job_uuid"`
	Fence        int64     `json:"fence" db:"fence"`
	ProducerUUID string    `json:"producer_uuid" db:"producer_uuid"`
	Digest       string    `json:"sha256" db:"digest"`
	RecordCount  int       `json:"record_count" db:"record_count"`
	Complete     bool      `json:"complete" db:"complete"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

type DiscoveryPage struct {
	DiscoveryPageReceipt
	Body json.RawMessage `json:"body"`
}

type DiscoveryJobCandidate struct {
	Sequence int64  `json:"sequence"`
	UUID     string `json:"uuid"`
}

type DiscoveryListingCandidate struct {
	UUID   string `json:"uuid"`
	Digest string `json:"definition_sha256"`
}

// After tracks inspected definitions, including ineligible ones. Empty results
// with HasMore still advance a bounded scan; they do not mean enumeration ended.
type DiscoveryListingCandidates struct {
	Listings []DiscoveryListingCandidate `json:"listings"`
	After    string                      `json:"after"`
	HasMore  bool                        `json:"has_more"`
}

type DiscoveryMaintenanceResult struct {
	Recovered int `json:"recovered"`
	Cancelled int `json:"cancelled"`
}

// Collection discovery returns current permitted containers with existing
// listing definitions. Per-profile readiness is checked inside each container.
type DiscoveryCollectionQuery struct {
	Scopes []IngestScope
	Roots  []string
	After  string
	Limit  int
}

type DiscoveryCollectionCandidate struct {
	UUID string `json:"uuid" db:"uuid"`
}

type DiscoveryJobReaderWriter interface {
	CreateListing(context.Context, DiscoveryListingInput, time.Time) (*DiscoveryListing, error)
	Listing(context.Context, string) (*DiscoveryListing, error)
	CheckListing(context.Context, string, time.Time) (*DiscoveryListing, error)
	Admit(context.Context, string, time.Time) (*ArchiveJob, error)
	Retry(context.Context, string, string, time.Time) (*ArchiveJob, error)
	Job(context.Context, string) (*ArchiveJob, error)
	BindAttempt(context.Context, DiscoveryJobLease, time.Time) error
	Attempt(context.Context, string, int64) (*DiscoveryJobAttempt, error)
	CheckLease(context.Context, DiscoveryJobLease, time.Time) (*ArchiveJob, error)
	ReserveSource(context.Context, DiscoveryJobLease, string, time.Time) (bool, error)
	AppendPage(context.Context, DiscoveryJobLease, int, json.RawMessage, time.Time) (*DiscoveryPageReceipt, error)
	Page(context.Context, string, int) (*DiscoveryPage, error)
	PageHead(context.Context, string) (*DiscoveryPage, error)
	Pages(context.Context, string, int, int) ([]DiscoveryPageReceipt, error)
	Ready(context.Context, string, string, string, int64, int, time.Time) ([]DiscoveryJobCandidate, error)
	ReadyListings(context.Context, string, string, string, string, int, time.Time) (*DiscoveryListingCandidates, error)
	Maintain(context.Context, time.Time) (*DiscoveryMaintenanceResult, error)
	Collections(context.Context, DiscoveryCollectionQuery) ([]DiscoveryCollectionCandidate, error)
}
