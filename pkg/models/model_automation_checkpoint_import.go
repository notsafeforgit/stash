package models

import (
	"context"
	"encoding/json"
	"time"
)

// Mapping a staged legacy result neither releases its review hold nor asserts
// that a native worker observed, published or completed it.
type AutomationCheckpointImport = AutomationEnrichmentImport

type AutomationCheckpointRecord struct {
	Ordinal         int64   `json:"ordinal" db:"ordinal"`
	SourceSHA256    string  `json:"source_sha256" db:"source_sha256"`
	StagedSHA256    *string `json:"staged_sha256" db:"staged_sha256"`
	BodySHA256      *string `json:"body_sha256" db:"body_sha256"`
	RecordCount     int     `json:"record_count" db:"record_count"`
	PendingCount    int     `json:"pending_count" db:"pending_count"`
	UnresolvedCount int     `json:"unresolved_count" db:"unresolved_count"`
	Outcome         string  `json:"outcome" db:"outcome"`
	Reason          string  `json:"reason" db:"reason"`
}

type AutomationCheckpointRecordDetails struct {
	AutomationCheckpointRecord
	SourceValues json.RawMessage `json:"source_values"`
	Body         json.RawMessage `json:"body"`
}

type AutomationCheckpointImportReaderWriter interface {
	Advance(context.Context, string, string, int64, time.Time) (*AutomationCheckpointImport, error)
	Find(context.Context, string) (*AutomationCheckpointImport, error)
	Records(context.Context, string, int64, int) ([]AutomationCheckpointRecord, error)
	Record(context.Context, string, int64) (*AutomationCheckpointRecordDetails, error)
	PreviewEvidence(context.Context, CheckpointEvidenceInput) (*CheckpointEvidencePlan, error)
	AcceptEvidence(context.Context, CheckpointEvidenceInput, string, time.Time) (*CheckpointEvidenceAcceptance, error)
	EvidenceAcceptance(context.Context, string) (*CheckpointEvidenceAcceptance, error)
}
