package models

import (
	"context"
	"encoding/json"
	"time"
)

type AutomationEnrichmentImport struct {
	SnapshotUUID     string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256   string `json:"manifest_sha256" db:"manifest_sha256"`
	Policy           string `json:"policy" db:"policy"`
	State            string `json:"state" db:"state"`
	LastOrdinal      int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords     int64  `json:"source_records" db:"source_records"`
	ProcessedRecords int64  `json:"processed_records" db:"processed_records"`
	MappedRecords    int64  `json:"mapped_records" db:"mapped_records"`
	ReviewRecords    int64  `json:"review_records" db:"review_records"`
	CreatedAt        string `json:"created_at" db:"created_at"`
	UpdatedAt        string `json:"updated_at" db:"updated_at"`
	Imported         bool   `json:"imported"`
}

type AutomationEnrichmentRecord struct {
	Ordinal             int64            `json:"ordinal" db:"ordinal"`
	Table               string           `json:"table" db:"source_table"`
	Key                 string           `json:"key" db:"source_key"`
	KeyOmitted          bool             `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256              string           `json:"sha256" db:"data_sha256"`
	PostUUID            *string          `json:"post_uuid,omitempty" db:"post_uuid"`
	PostReference       string           `json:"post_reference,omitempty" db:"post_reference"`
	CatalogSnapshotUUID *string          `json:"catalog_snapshot_uuid,omitempty" db:"catalog_snapshot_uuid"`
	CollectionUUID      *string          `json:"collection_uuid,omitempty" db:"collection_uuid"`
	CollectionRevision  *int             `json:"collection_revision,omitempty" db:"collection_revision"`
	URLUUID             *string          `json:"url_uuid,omitempty" db:"url_uuid"`
	URLEvidenceUUID     *string          `json:"url_evidence_uuid,omitempty" db:"url_evidence_uuid"`
	TargetUUID          *string          `json:"target_uuid,omitempty" db:"target_uuid"`
	TargetRevision      *int             `json:"target_revision,omitempty" db:"target_revision"`
	ReceiptUUID         *string          `json:"receipt_uuid,omitempty" db:"receipt_uuid"`
	CaptureUUID         *string          `json:"capture_uuid,omitempty" db:"capture_uuid"`
	CompletionUUID      *string          `json:"completion_uuid,omitempty" db:"completion_uuid"`
	AliasOrdinal        *int64           `json:"alias_ordinal,omitempty" db:"alias_ordinal"`
	HistoricalAttempts  *int64           `json:"historical_attempts,omitempty" db:"historical_attempts"`
	ServiceScope        string           `json:"service_scope,omitempty" db:"service_scope"`
	NotBefore           *time.Time       `json:"not_before,omitempty" db:"not_before"`
	StagedSHA256        string           `json:"staged_sha256,omitempty" db:"staged_sha256"`
	CooldownKind        string           `json:"cooldown_kind,omitempty" db:"cooldown_kind"`
	CooldownValue       string           `json:"cooldown_value,omitempty" db:"cooldown_value"`
	CooldownUntil       *time.Time       `json:"cooldown_until,omitempty" db:"cooldown_until"`
	CooldownReason      string           `json:"cooldown_reason,omitempty" db:"cooldown_reason"`
	SeedLastPostKey     *string          `json:"seed_last_post_key,omitempty" db:"seed_last_post_key"`
	SeedComplete        *bool            `json:"seed_complete,omitempty" db:"seed_complete"`
	SeedCounts          *json.RawMessage `json:"seed_counts,omitempty" db:"seed_counts"`
	SourcePlatform      string           `json:"source_platform,omitempty" db:"source_platform"`
	SourceLastAttempt   *time.Time       `json:"source_last_attempt,omitempty" db:"source_last_attempt"`
	Disposition         string           `json:"disposition" db:"disposition"`
	Outcome             string           `json:"outcome" db:"outcome"`
	Reason              string           `json:"reason" db:"reason"`
}

type AutomationEnrichmentRecordDetails struct {
	AutomationEnrichmentRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type AutomationEnrichmentImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*AutomationEnrichmentImport, error)
	Find(context.Context, string) (*AutomationEnrichmentImport, error)
	Records(context.Context, string, int64, int) ([]AutomationEnrichmentRecord, error)
	Record(context.Context, string, int64) (*AutomationEnrichmentRecordDetails, error)
	HeldTargets(context.Context, string, string, int64, int) ([]AutomationEnrichmentCandidate, error)
}
