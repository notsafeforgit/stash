package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogFileHistoryImport struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	CollectionUUID     string `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	RootUUID           string `json:"root_uuid" db:"root_uuid"`
	RootRevision       int    `json:"root_revision" db:"root_revision"`
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

type CatalogFileHistoryRecord struct {
	Ordinal     int64   `json:"ordinal" db:"ordinal"`
	Table       string  `json:"table" db:"source_table"`
	Key         string  `json:"key" db:"source_key"`
	KeyOmitted  bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256      string  `json:"sha256" db:"data_sha256"`
	HistoryUUID *string `json:"history_uuid,omitempty" db:"history_uuid"`
	Outcome     string  `json:"outcome" db:"outcome"`
	Reason      string  `json:"reason" db:"reason"`
}

type CatalogFileHistoryRecordDetails struct {
	CatalogFileHistoryRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogFileHistoryImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogFileHistoryImport, error)
	Find(context.Context, string) (*CatalogFileHistoryImport, error)
	Records(context.Context, string, int64, int) ([]CatalogFileHistoryRecord, error)
	Record(context.Context, string, int64) (*CatalogFileHistoryRecordDetails, error)
}
