package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogTranslationImport struct {
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

type CatalogTranslationRecord struct {
	Ordinal         int64   `json:"ordinal" db:"ordinal"`
	Key             string  `json:"key" db:"source_key"`
	KeyOmitted      bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256          string  `json:"sha256" db:"data_sha256"`
	PostUUID        *string `json:"post_uuid,omitempty" db:"post_uuid"`
	TranslationUUID *string `json:"translation_uuid,omitempty" db:"translation_uuid"`
	EvidenceUUID    *string `json:"evidence_uuid,omitempty" db:"evidence_uuid"`
	InputHashState  string  `json:"input_hash_state" db:"input_hash_state"`
	Outcome         string  `json:"outcome" db:"outcome"`
	Reason          string  `json:"reason" db:"reason"`
}

type CatalogTranslationRecordDetails struct {
	CatalogTranslationRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogTranslationImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogTranslationImport, error)
	Find(context.Context, string) (*CatalogTranslationImport, error)
	Records(context.Context, string, int64, int) ([]CatalogTranslationRecord, error)
	Record(context.Context, string, int64) (*CatalogTranslationRecordDetails, error)
}
