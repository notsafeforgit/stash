package models

import (
	"context"
	"encoding/json"
	"time"
)

type AutomationDiscoveryImport = AutomationEnrichmentImport

// Compact, typed continuations point to original snapshot values rather than
// copying candidate payloads, cursors or maintenance documents a second time.
// These historical records never own a running job or certify a new scrape.
type AutomationDiscoveryProjection struct {
	Phase              string     `json:"phase,omitempty" db:"phase"`
	ServiceScope       string     `json:"service_scope,omitempty" db:"service_scope"`
	ProfileURL         string     `json:"profile_url,omitempty" db:"profile_url"`
	NotBefore          *time.Time `json:"not_before,omitempty" db:"not_before"`
	HistoricalAttempts *int64     `json:"historical_attempts,omitempty" db:"historical_attempts"`
	HistoricalPages    *int64     `json:"historical_pages,omitempty" db:"historical_pages"`
	CursorSHA256       string     `json:"cursor_sha256,omitempty" db:"cursor_sha256"`
	StagedSHA256       string     `json:"staged_sha256,omitempty" db:"staged_sha256"`
	StagedKind         string     `json:"staged_kind,omitempty" db:"staged_kind"`
	EvidenceSHA256     string     `json:"evidence_sha256,omitempty" db:"evidence_sha256"`
	CandidateURL       string     `json:"candidate_url,omitempty" db:"candidate_url"`
	CandidateBasis     string     `json:"candidate_basis,omitempty" db:"candidate_basis"`
	PayloadSHA256      string     `json:"payload_sha256,omitempty" db:"payload_sha256"`
	MaintenanceKind    string     `json:"maintenance_kind,omitempty" db:"maintenance_kind"`
	MaintenanceTime    *time.Time `json:"maintenance_time,omitempty" db:"maintenance_time"`
	WatermarkNS        string     `json:"watermark_ns,omitempty" db:"watermark_ns"`
}

type AutomationDiscoveryRecord struct {
	AutomationDiscoveryProjection
	Ordinal             int64   `json:"ordinal" db:"ordinal"`
	Table               string  `json:"table" db:"source_table"`
	Key                 string  `json:"key" db:"source_key"`
	KeyOmitted          bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256              string  `json:"sha256" db:"data_sha256"`
	AccountUUID         *string `json:"account_uuid,omitempty" db:"account_uuid"`
	AccountOrdinal      *int64  `json:"account_ordinal,omitempty" db:"account_ordinal"`
	TargetOrdinal       *int64  `json:"target_ordinal,omitempty" db:"target_ordinal"`
	EnrichmentOrdinal   *int64  `json:"enrichment_ordinal,omitempty" db:"enrichment_ordinal"`
	PostUUID            *string `json:"post_uuid,omitempty" db:"post_uuid"`
	PostReference       string  `json:"post_reference,omitempty" db:"post_reference"`
	CatalogSnapshotUUID *string `json:"catalog_snapshot_uuid,omitempty" db:"catalog_snapshot_uuid"`
	CollectionUUID      *string `json:"collection_uuid,omitempty" db:"collection_uuid"`
	CollectionRevision  *int    `json:"collection_revision,omitempty" db:"collection_revision"`
	Disposition         string  `json:"disposition" db:"disposition"`
	Outcome             string  `json:"outcome" db:"outcome"`
	Reason              string  `json:"reason" db:"reason"`
}

type AutomationDiscoveryRecordDetails struct {
	AutomationDiscoveryRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type AutomationDiscoveryImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*AutomationDiscoveryImport, error)
	Find(context.Context, string) (*AutomationDiscoveryImport, error)
	Records(context.Context, string, int64, int) ([]AutomationDiscoveryRecord, error)
	Record(context.Context, string, int64) (*AutomationDiscoveryRecordDetails, error)
}
