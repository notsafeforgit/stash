package models

import (
	"context"
	"encoding/json"
	"time"
)

// This pass covers account rows, handle history, post URLs/aliases and original
// post/account claims. Other catalog families and publisher decisions are separate.
type CatalogRelationsImport struct {
	SnapshotUUID      string `json:"snapshot_uuid" db:"snapshot_uuid"`
	ManifestSHA256    string `json:"manifest_sha256" db:"manifest_sha256"`
	State             string `json:"state" db:"state"`
	LastOrdinal       int64  `json:"last_ordinal" db:"last_ordinal"`
	TotalRecords      int64  `json:"source_records" db:"source_records"`
	ProcessedRecords  int64  `json:"processed_records" db:"processed_records"`
	MappedRecords     int64  `json:"mapped_records" db:"mapped_records"`
	ReviewRecords     int64  `json:"review_records" db:"review_records"`
	UnassignedRecords int64  `json:"unassigned_records" db:"unassigned_records"`
	CreatedAt         string `json:"created_at" db:"created_at"`
	UpdatedAt         string `json:"updated_at" db:"updated_at"`
	Imported          bool   `json:"imported"`
}

type CatalogRelationRecord struct {
	Ordinal                    int64   `json:"ordinal" db:"ordinal"`
	Table                      string  `json:"table" db:"source_table"`
	Key                        string  `json:"key" db:"source_key"`
	KeyOmitted                 bool    `json:"key_omitted,omitempty" db:"key_omitted"`
	SHA256                     string  `json:"sha256" db:"data_sha256"`
	Outcome                    string  `json:"outcome" db:"outcome"`
	Reason                     string  `json:"reason" db:"reason"`
	AccountUUID                *string `json:"account_uuid,omitempty" db:"account_uuid"`
	IdentifierUUID             *string `json:"identifier_uuid,omitempty" db:"identifier_uuid"`
	IdentifierEvidenceKey      *string `json:"identifier_evidence_key,omitempty" db:"identifier_evidence_key"`
	PostUUID                   *string `json:"post_uuid,omitempty" db:"post_uuid"`
	URLEvidenceUUID            *string `json:"url_evidence_uuid,omitempty" db:"url_evidence_uuid"`
	PostIdentifierEvidenceUUID *string `json:"post_identifier_evidence_uuid,omitempty" db:"post_identifier_evidence_uuid"`
	AccountClaimUUID           *string `json:"account_claim_uuid,omitempty" db:"account_claim_uuid"`
}

type CatalogRelationRecordDetails struct {
	CatalogRelationRecord
	SourceValues json.RawMessage `json:"source_values"`
}

type CatalogRelationsImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*CatalogRelationsImport, error)
	Find(context.Context, string) (*CatalogRelationsImport, error)
	Records(context.Context, string, int64, int) ([]CatalogRelationRecord, error)
	Record(context.Context, string, int64) (*CatalogRelationRecordDetails, error)
}
