package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogDocumentImport struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	CollectionUUID     string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	Policy             string `json:"policy" db:"policy"`
	State              string `json:"state" db:"state"`
	Phase              string `json:"phase" db:"phase"`
	LastOrdinal        int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords       int64  `json:"source_records" db:"source_records"`
	ProcessedRecords   int64  `json:"processed_records" db:"processed_records"`
	MappedRecords      int64  `json:"mapped_records" db:"mapped_records"`
	ReviewRecords      int64  `json:"review_records" db:"review_records"`
	CreatedAt          string `json:"created_at" db:"created_at"`
	UpdatedAt          string `json:"updated_at" db:"updated_at"`
	Imported           bool   `json:"imported"`
}

type CatalogDocumentRecord struct {
	Ordinal        int64   `json:"ordinal" db:"ordinal"`
	Table          string  `json:"table" db:"source_table"`
	Key            string  `json:"key" db:"source_key"`
	KeyOmitted     bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256         string  `json:"sha256" db:"data_sha256"`
	DocumentUUID   *string `json:"document_uuid,omitempty" db:"document_uuid"`
	SourceUUID     *string `json:"source_uuid,omitempty" db:"source_uuid"`
	PostUUID       *string `json:"post_uuid,omitempty" db:"post_uuid"`
	ClaimUUID      *string `json:"claim_uuid,omitempty" db:"claim_uuid"`
	HeadUUID       *string `json:"head_uuid,omitempty" db:"head_uuid"`
	SelectionBasis string  `json:"selection_basis" db:"selection_basis"`
	Outcome        string  `json:"outcome" db:"outcome"`
	Reason         string  `json:"reason" db:"reason"`
}

type CatalogDocumentRecordDetails struct {
	CatalogDocumentRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogDocumentImportReaderWriter interface {
	// The checkpoint counts processed source records across dependency phases.
	Advance(context.Context, string, string, int64, time.Time) (*CatalogDocumentImport, error)
	Find(context.Context, string) (*CatalogDocumentImport, error)
	Records(context.Context, string, int64, int) ([]CatalogDocumentRecord, error)
	Record(context.Context, string, int64) (*CatalogDocumentRecordDetails, error)
}
