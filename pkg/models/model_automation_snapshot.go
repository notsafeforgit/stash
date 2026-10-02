package models

import (
	"context"
	"errors"
	"time"
)

// Receiving historical operating state does not activate or import its jobs.
type AutomationSnapshot struct {
	UUID               string   `json:"snapshot_uuid" db:"uuid"`
	SourceUUID         string   `json:"registry_source_uuid" db:"source_uuid"`
	RegistryImportUUID string   `json:"registry_import_uuid" db:"registry_import_uuid"`
	ManifestSHA256     string   `json:"manifest_sha256" db:"manifest_sha256"`
	SourceSHA256       string   `json:"source_sha256" db:"source_sha256"`
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
	ErrAutomationSnapshotInvalid  = errors.New("invalid automation snapshot")
	ErrAutomationSnapshotConflict = errors.New("automation snapshot identity or checkpoint conflicts")
)

type AutomationSnapshotReaderWriter interface {
	Begin(context.Context, []byte, string, time.Time) (*AutomationSnapshot, error)
	Receive(context.Context, string, string, int, []byte, time.Time) (*AutomationSnapshot, error)
	Find(context.Context, string) (*AutomationSnapshot, error)
}
