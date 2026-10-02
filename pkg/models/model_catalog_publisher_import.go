package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogPublisherImport struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	Policy             string `json:"policy" db:"policy"`
	State              string `json:"state" db:"state"`
	LastOrdinal        int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords       int64  `json:"source_records" db:"source_records"`
	ProcessedRecords   int64  `json:"processed_records" db:"processed_records"`
	LinkedRecords      int64  `json:"linked_records" db:"linked_records"`
	PreservedRecords   int64  `json:"preserved_records" db:"preserved_records"`
	ReviewRecords      int64  `json:"review_records" db:"review_records"`
	UnavailableRecords int64  `json:"unavailable_records" db:"unavailable_records"`
	CreatedAccounts    int64  `json:"created_accounts" db:"created_accounts"`
	CreatedAt          string `json:"created_at" db:"created_at"`
	UpdatedAt          string `json:"updated_at" db:"updated_at"`
	Imported           bool   `json:"imported"`
}

// The source row and capture stay in shared evidence storage. A receipt records
// the publisher decision at import time, not a replacement for its current head.
type CatalogPublisherRecord struct {
	Ordinal              int64   `json:"ordinal" db:"ordinal"`
	Table                string  `json:"table" db:"source_table"`
	Key                  string  `json:"key" db:"source_key"`
	KeyOmitted           bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256               string  `json:"sha256" db:"data_sha256"`
	PostUUID             *string `json:"post_uuid,omitempty" db:"post_uuid"`
	CaptureUUID          *string `json:"capture_uuid,omitempty" db:"capture_uuid"`
	DecisionUUID         *string `json:"decision_uuid,omitempty" db:"decision_uuid"`
	AccountUUID          *string `json:"account_uuid,omitempty" db:"account_uuid"`
	CanonicalAccountUUID *string `json:"canonical_account_uuid,omitempty" db:"canonical_account_uuid"`
	Outcome              string  `json:"outcome" db:"outcome"`
	Reason               string  `json:"reason" db:"reason"`
	CreatedAccount       bool    `json:"created_account" db:"created_account"`
}

type CatalogPublisherRecordDetails struct {
	CatalogPublisherRecord
	Context json.RawMessage `json:"context"`
}

type CatalogPublisherImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogPublisherImport, error)
	Find(context.Context, string) (*CatalogPublisherImport, error)
	Records(context.Context, string, int64, int) ([]CatalogPublisherRecord, error)
	Record(context.Context, string, int64) (*CatalogPublisherRecordDetails, error)
}
