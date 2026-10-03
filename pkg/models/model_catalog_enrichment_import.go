package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogEnrichmentImport struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	CollectionUUID     string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	Policy             string `json:"policy" db:"policy"`
	State              string `json:"state" db:"state"`
	LastOrdinal        int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords       int64  `json:"source_records" db:"source_records"`
	ProcessedRecords   int64  `json:"processed_records" db:"processed_records"`
	MappedRecords      int64  `json:"mapped_records" db:"mapped_records"`
	ReviewRecords      int64  `json:"review_records" db:"review_records"`
	CreatedAt          string `json:"created_at" db:"created_at"`
	UpdatedAt          string `json:"updated_at" db:"updated_at"`
	Imported           bool   `json:"imported"`
}

type CatalogEnrichmentRecord struct {
	Ordinal     int64   `json:"ordinal" db:"ordinal"`
	Key         string  `json:"key" db:"source_key"`
	KeyOmitted  bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256      string  `json:"sha256" db:"data_sha256"`
	PostUUID    *string `json:"post_uuid,omitempty" db:"post_uuid"`
	ReceiptUUID *string `json:"receipt_uuid,omitempty" db:"receipt_uuid"`
	Outcome     string  `json:"outcome" db:"outcome"`
	Reason      string  `json:"reason" db:"reason"`
}

type CatalogEnrichmentRecordDetails struct {
	CatalogEnrichmentRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogEnrichmentImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogEnrichmentImport, error)
	Find(context.Context, string) (*CatalogEnrichmentImport, error)
	Records(context.Context, string, int64, int) ([]CatalogEnrichmentRecord, error)
	Record(context.Context, string, int64) (*CatalogEnrichmentRecordDetails, error)
}

// SourceEnrichmentReceipt retains a legacy worker's completion assertion.
// It does not certify complete child coverage or a native job execution.
type SourceEnrichmentReceipt struct {
	UUID                    string `json:"uuid" db:"uuid"`
	PostUUID                string `json:"post_uuid" db:"post_uuid"`
	CollectionUUID          string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision      int    `json:"collection_revision" db:"collection_revision"`
	Origin                  string `json:"origin" db:"origin"`
	Policy                  string `json:"policy" db:"policy"`
	SourceVersion           int    `json:"source_version" db:"source_version"`
	CompletedAt             string `json:"completed_at" db:"completed_at"`
	AttachmentLinksEnriched int64  `json:"attachment_links_enriched" db:"attachment_links_enriched"`
	UnresolvedChildren      int64  `json:"unresolved_children" db:"unresolved_children"`
	RecordedAt              string `json:"recorded_at" db:"recorded_at"`
}

type SourceEnrichmentReceiptReader interface {
	Find(context.Context, string) (*SourceEnrichmentReceipt, error)
	PostReceipts(context.Context, string, string, int) ([]SourceEnrichmentReceipt, error)
}
