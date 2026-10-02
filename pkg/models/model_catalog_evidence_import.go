package models

import (
	"context"
	"time"
)

type CatalogEvidenceImport struct {
	SnapshotUUID     string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256   string `json:"manifest_sha256" db:"manifest_sha256"`
	State            string `json:"state" db:"state"`
	LastOrdinal      int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords     int64  `json:"source_records" db:"source_records"`
	ProcessedRecords int64  `json:"processed_records" db:"processed_records"`
	ReviewRecords    int64  `json:"review_records" db:"review_records"`
	CaptureMappings  int64  `json:"capture_mappings" db:"capture_mappings"`
	ProfileMappings  int64  `json:"profile_mappings" db:"profile_mappings"`
	CreatedAt        string `json:"created_at" db:"created_at"`
	UpdatedAt        string `json:"updated_at" db:"updated_at"`
	// Other catalog families, metadata decisions and media intake are separate.
	Imported bool `json:"imported"`
}

type CatalogEvidenceRecord struct {
	Ordinal      int64   `json:"ordinal" db:"ordinal"`
	Table        string  `json:"table" db:"source_table"`
	Key          string  `json:"key" db:"source_key"`
	SHA256       string  `json:"sha256" db:"data_sha256"`
	Outcome      string  `json:"outcome" db:"outcome"`
	Reason       string  `json:"reason" db:"reason"`
	PostUUID     *string `json:"post_uuid,omitempty" db:"post_uuid"`
	CaptureUUID  *string `json:"capture_uuid,omitempty" db:"capture_uuid"`
	ProfileHash  *string `json:"profile_hash,omitempty" db:"profile_hash"`
	HeaderSHA256 *string `json:"header_sha256,omitempty" db:"header_sha256"`
}

type CatalogEvidenceImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogEvidenceImport, error)
	Find(context.Context, string) (*CatalogEvidenceImport, error)
	Records(context.Context, string, int64, int) ([]CatalogEvidenceRecord, error)
}
