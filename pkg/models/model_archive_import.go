package models

import (
	"context"
	"errors"
)

// ArchiveImportSnapshot separates receipt of migration input from the outcomes
// of its domain importers. Neither state certifies current review or scrape work.
type ArchiveImportSnapshot struct {
	Kind               string  `json:"kind" db:"kind"`
	UUID               string  `json:"uuid" db:"uuid"`
	SourceUUID         string  `json:"source_uuid" db:"source_uuid"`
	RegistryImportUUID string  `json:"registry_import_uuid" db:"registry_import_uuid"`
	CollectionUUID     *string `json:"collection_uuid" db:"collection_uuid"`
	CollectionLabel    *string `json:"collection_label" db:"collection_label"`
	CapturedAt         string  `json:"captured_at" db:"captured_at"`
	TransferState      string  `json:"transfer_state" db:"state"`
	Chunks             int     `json:"chunks" db:"chunk_count"`
	ReceivedChunks     int     `json:"received_chunks" db:"next_chunk"`
	Records            int64   `json:"records" db:"record_count"`
	ReceivedRecords    int64   `json:"received_records" db:"received_records"`
	Bytes              int64   `json:"bytes" db:"byte_count"`
	ReceivedBytes      int64   `json:"received_bytes" db:"received_bytes"`
	CreatedAt          string  `json:"created_at" db:"created_at"`
	UpdatedAt          string  `json:"updated_at" db:"updated_at"`
}

type ArchiveImportFilter struct {
	Kind  string
	After string
	Limit int
}

// These counters retain the result at import time. Resolving an association
// later does not rewrite the import receipt or keep that item in a review queue.
type ArchiveImportProgress struct {
	State                   string `json:"state" db:"state"`
	SourceRecords           int64  `json:"source_records" db:"source_records"`
	ProcessedRecords        int64  `json:"processed_records" db:"processed_records"`
	HistoricalReviewRecords int64  `json:"historical_review_records" db:"review_records"`
	UpdatedAt               string `json:"updated_at" db:"updated_at"`
}

// A missing progress row means the importer has not run, including when its
// eventual input may be empty. Do not infer completion from received bytes.
type ArchiveImportFamily struct {
	Name     string                 `json:"name"`
	Progress *ArchiveImportProgress `json:"progress"`
}

type ArchiveImportDetails struct {
	Snapshot ArchiveImportSnapshot `json:"snapshot"`
	Families []ArchiveImportFamily `json:"families"`
}

var ErrArchiveImportInvalid = errors.New("invalid archive import history request")

type ArchiveImportReader interface {
	Snapshots(context.Context, ArchiveImportFilter) ([]ArchiveImportSnapshot, error)
	Snapshot(context.Context, string, string) (*ArchiveImportDetails, error)
}
