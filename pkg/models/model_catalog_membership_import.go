package models

import (
	"context"
	"encoding/json"
	"time"
)

type CatalogMembershipImport struct {
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

type CatalogMembershipRecord struct {
	Ordinal        int64   `json:"ordinal" db:"ordinal"`
	Key            string  `json:"key" db:"source_key"`
	KeyOmitted     bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256         string  `json:"sha256" db:"data_sha256"`
	PostUUID       *string `json:"post_uuid,omitempty" db:"post_uuid"`
	CollectionUUID *string `json:"collection_uuid,omitempty" db:"collection_uuid"`
	MembershipUUID *string `json:"membership_uuid,omitempty" db:"membership_uuid"`
	Outcome        string  `json:"outcome" db:"outcome"`
	Reason         string  `json:"reason" db:"reason"`
}

type CatalogMembershipRecordDetails struct {
	CatalogMembershipRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogMembershipImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogMembershipImport, error)
	Find(context.Context, string) (*CatalogMembershipImport, error)
	Records(context.Context, string, int64, int) ([]CatalogMembershipRecord, error)
	Record(context.Context, string, int64) (*CatalogMembershipRecordDetails, error)
}
