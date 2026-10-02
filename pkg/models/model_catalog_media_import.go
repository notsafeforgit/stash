package models

import (
	"context"
	"encoding/json"
	"time"
)

// This reviewed historical mount mapping does not activate a root or grant a
// worker filesystem access. Revision checks apply when the binding is created.
type CatalogMediaBinding struct {
	SnapshotUUID       string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256     string `json:"manifest_sha256" db:"manifest_sha256"`
	RootUUID           string `json:"root_uuid" db:"root_uuid"`
	RootRevision       int    `json:"root_revision" db:"root_revision"`
	CollectionRevision int    `json:"collection_revision" db:"collection_revision"`
	LibraryRootPath    string `json:"library_root_path" db:"library_root_path"`
}

type CatalogMediaImport struct {
	CatalogMediaBinding
	Policy             string `json:"policy" db:"policy"`
	State              string `json:"state" db:"state"`
	Phase              string `json:"phase" db:"phase"`
	LastOrdinal        int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords       int64  `json:"source_records" db:"source_records"`
	ProcessedRecords   int64  `json:"processed_records" db:"processed_records"`
	MappedRecords      int64  `json:"mapped_records" db:"mapped_records"`
	ReviewRecords      int64  `json:"review_records" db:"review_records"`
	UnavailableRecords int64  `json:"unavailable_records" db:"unavailable_records"`
	MatchedFiles       int64  `json:"matched_files" db:"matched_files"`
	MediaAssociations  int64  `json:"media_associations" db:"media_associations"`
	CreatedAt          string `json:"created_at" db:"created_at"`
	UpdatedAt          string `json:"updated_at" db:"updated_at"`
	Imported           bool   `json:"imported"`
}

// Every original asset, file and appearance has one receipt. References point
// to shared native evidence; source payloads are not copied into each summary.
type CatalogMediaRecord struct {
	Ordinal           int64   `json:"ordinal" db:"ordinal"`
	Table             string  `json:"table" db:"source_table"`
	Key               string  `json:"key" db:"source_key"`
	KeyOmitted        bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256            string  `json:"sha256" db:"data_sha256"`
	ClaimUUID         *string `json:"claim_uuid,omitempty" db:"claim_uuid"`
	ObservationUUID   *string `json:"observation_uuid,omitempty" db:"observation_uuid"`
	MatchUUID         *string `json:"match_uuid,omitempty" db:"match_uuid"`
	PostUUID          *string `json:"post_uuid,omitempty" db:"post_uuid"`
	PostFileUUID      *string `json:"post_file_uuid,omitempty" db:"post_file_uuid"`
	MediaEvidenceUUID *string `json:"media_evidence_uuid,omitempty" db:"media_evidence_uuid"`
	Outcome           string  `json:"outcome" db:"outcome"`
	Reason            string  `json:"reason" db:"reason"`
}

type CatalogMediaRecordDetails struct {
	CatalogMediaRecord
	Context json.RawMessage `json:"context"`
}

type CatalogMediaImportReaderWriter interface {
	Begin(context.Context, CatalogMediaBinding, time.Time) (*CatalogMediaImport, error)
	// The checkpoint is the processed-record count, across all three phases.
	Advance(context.Context, string, string, int64, time.Time) (*CatalogMediaImport, error)
	Find(context.Context, string) (*CatalogMediaImport, error)
	Records(context.Context, string, int64, int) ([]CatalogMediaRecord, error)
	Record(context.Context, string, int64) (*CatalogMediaRecordDetails, error)
}
