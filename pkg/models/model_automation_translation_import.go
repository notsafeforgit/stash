package models

import (
	"context"
	"encoding/json"
	"time"
)

type AutomationTranslationImport struct {
	SnapshotUUID     string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256   string `json:"manifest_sha256" db:"manifest_sha256"`
	Policy           string `json:"policy" db:"policy"`
	State            string `json:"state" db:"state"`
	LastOrdinal      int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords     int64  `json:"source_records" db:"source_records"`
	ProcessedRecords int64  `json:"processed_records" db:"processed_records"`
	MappedRecords    int64  `json:"mapped_records" db:"mapped_records"`
	ReviewRecords    int64  `json:"review_records" db:"review_records"`
	CreatedAt        string `json:"created_at" db:"created_at"`
	UpdatedAt        string `json:"updated_at" db:"updated_at"`
	Imported         bool   `json:"imported"`
}

type AutomationTranslationRecord struct {
	Ordinal            int64   `json:"ordinal" db:"ordinal"`
	Table              string  `json:"table" db:"source_table"`
	Key                string  `json:"key" db:"source_key"`
	KeyOmitted         bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256             string  `json:"sha256" db:"data_sha256"`
	JobOrdinal         *int64  `json:"job_ordinal,omitempty" db:"job_ordinal"`
	RequestUUID        *string `json:"request_uuid,omitempty" db:"request_uuid"`
	CacheUUID          *string `json:"cache_uuid,omitempty" db:"cache_uuid"`
	PostUUID           *string `json:"post_uuid,omitempty" db:"post_uuid"`
	PostReference      string  `json:"post_reference,omitempty" db:"post_reference"`
	CollectionUUID     *string `json:"collection_uuid,omitempty" db:"collection_uuid"`
	CollectionRevision *int    `json:"collection_revision,omitempty" db:"collection_revision"`
	TargetUUID         *string `json:"target_uuid,omitempty" db:"target_uuid"`
	TargetRevision     *int    `json:"target_revision,omitempty" db:"target_revision"`
	EvidenceUUID       *string `json:"evidence_uuid,omitempty" db:"evidence_uuid"`
	Disposition        string  `json:"disposition" db:"disposition"`
	Outcome            string  `json:"outcome" db:"outcome"`
	Reason             string  `json:"reason" db:"reason"`
}

type AutomationTranslationRecordDetails struct {
	AutomationTranslationRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type AutomationTranslationImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*AutomationTranslationImport, error)
	Find(context.Context, string) (*AutomationTranslationImport, error)
	Records(context.Context, string, int64, int) ([]AutomationTranslationRecord, error)
	Record(context.Context, string, int64) (*AutomationTranslationRecordDetails, error)
}
