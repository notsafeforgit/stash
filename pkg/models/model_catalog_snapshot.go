package models

import (
	"context"
	"errors"
	"time"
)

// Receipt of migration input, not proof that its records have native mappings.
// Pending families stay explicit until their domain importers reconcile them.
type CatalogSnapshot struct {
	UUID               string   `json:"snapshot_uuid" db:"uuid"`
	SourceUUID         string   `json:"registry_source_uuid" db:"source_uuid"`
	RegistryImportUUID string   `json:"registry_import_uuid" db:"registry_import_uuid"`
	CatalogID          string   `json:"catalog_id" db:"catalog_id"`
	CollectionUUID     string   `json:"collection_uuid" db:"collection_uuid"`
	ManifestSHA256     string   `json:"manifest_sha256" db:"manifest_sha256"`
	CapturedAt         string   `json:"captured_at" db:"captured_at"`
	State              string   `json:"state" db:"state"`
	Chunks             int      `json:"chunks" db:"chunk_count"`
	Records            int64    `json:"records" db:"record_count"`
	Bytes              int64    `json:"bytes" db:"byte_count"`
	NextChunk          int      `json:"next_chunk" db:"next_chunk"`
	ReceivedRecords    int64    `json:"received_records" db:"received_records"`
	ReceivedBytes      int64    `json:"received_bytes" db:"received_bytes"`
	CreatedAt          string   `json:"created_at" db:"created_at"`
	UpdatedAt          string   `json:"updated_at" db:"updated_at"`
	PendingFamilies    []string `json:"pending_families"`
	Imported           bool     `json:"imported"`
}

var (
	ErrCatalogSnapshotInvalid  = errors.New("invalid catalog snapshot")
	ErrCatalogSnapshotConflict = errors.New("catalog snapshot identity or checkpoint conflicts")
)

type CatalogSnapshotReaderWriter interface {
	Begin(context.Context, []byte, string, time.Time) (*CatalogSnapshot, error)
	Receive(context.Context, string, string, int, []byte, time.Time) (*CatalogSnapshot, error)
	Find(context.Context, string) (*CatalogSnapshot, error)
}
