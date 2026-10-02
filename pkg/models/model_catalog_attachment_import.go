package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogAttachmentImport struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	Policy             string `json:"policy" db:"policy"`
	State              string `json:"state" db:"state"`
	LastOrdinal        int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords       int64  `json:"source_records" db:"source_records"`
	ProcessedRecords   int64  `json:"processed_records" db:"processed_records"`
	MappedRecords      int64  `json:"mapped_records" db:"mapped_records"`
	PreservedRecords   int64  `json:"preserved_records" db:"preserved_records"`
	ReviewRecords      int64  `json:"review_records" db:"review_records"`
	UnavailableRecords int64  `json:"unavailable_records" db:"unavailable_records"`
	ChangedSelections  int64  `json:"changed_selections" db:"changed_selections"`
	CreatedAt          string `json:"created_at" db:"created_at"`
	UpdatedAt          string `json:"updated_at" db:"updated_at"`
	Imported           bool   `json:"imported"`
}

// A receipt references the shared source list and the historical selection.
// It neither copies source payloads nor associates library media by itself.
type CatalogAttachmentRecord struct {
	Ordinal          int64   `json:"ordinal" db:"ordinal"`
	Table            string  `json:"table" db:"source_table"`
	Key              string  `json:"key" db:"source_key"`
	KeyOmitted       bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256           string  `json:"sha256" db:"data_sha256"`
	PostUUID         *string `json:"post_uuid,omitempty" db:"post_uuid"`
	CaptureUUID      *string `json:"capture_uuid,omitempty" db:"capture_uuid"`
	ManifestUUID     *string `json:"manifest_uuid,omitempty" db:"manifest_uuid"`
	SelectionUUID    *string `json:"selection_uuid,omitempty" db:"selection_uuid"`
	Outcome          string  `json:"outcome" db:"outcome"`
	Reason           string  `json:"reason" db:"reason"`
	SelectionChanged bool    `json:"selection_changed" db:"selection_changed"`
}

type CatalogAttachmentRecordDetails struct {
	CatalogAttachmentRecord
	Context json.RawMessage `json:"context"`
}

type CatalogAttachmentImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogAttachmentImport, error)
	Find(context.Context, string) (*CatalogAttachmentImport, error)
	Records(context.Context, string, int64, int) ([]CatalogAttachmentRecord, error)
	Record(context.Context, string, int64) (*CatalogAttachmentRecordDetails, error)
}
