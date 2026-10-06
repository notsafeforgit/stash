package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogCleanupImport struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	CollectionUUID     string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	Policy             string `json:"policy" db:"policy"`
	State              string `json:"state" db:"state"`
	LastOrdinal        int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords       int64  `json:"source_records" db:"source_records"`
	ProcessedRecords   int64  `json:"processed_records" db:"processed_records"`
	HeldRecords        int64  `json:"held_records" db:"held_records"`
	ReviewRecords      int64  `json:"review_records" db:"review_records"`
	CreatedAt          string `json:"created_at" db:"created_at"`
	UpdatedAt          string `json:"updated_at" db:"updated_at"`
	Imported           bool   `json:"imported"`
}

type CatalogCleanupRecord struct {
	Ordinal    int64   `json:"ordinal" db:"ordinal"`
	Key        string  `json:"key" db:"source_key"`
	KeyOmitted bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256     string  `json:"sha256" db:"data_sha256"`
	IntentUUID *string `json:"intent_uuid,omitempty" db:"intent_uuid"`
	Outcome    string  `json:"outcome" db:"outcome"`
	Reason     string  `json:"reason" db:"reason"`
}

type CatalogCleanupRecordDetails struct {
	CatalogCleanupRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogCleanupImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogCleanupImport, error)
	Find(context.Context, string) (*CatalogCleanupImport, error)
	Records(context.Context, string, int64, int) ([]CatalogCleanupRecord, error)
	Record(context.Context, string, int64) (*CatalogCleanupRecordDetails, error)
}
