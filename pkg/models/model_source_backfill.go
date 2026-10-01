package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// A backfill subject is a qualified source account at one logical media root.
// It is not a performer identity: accepting an account's historical scrape
// cannot imply that its publisher depicts the same person in every file.
type BackfillSubject struct {
	RootUUID string `json:"root_uuid"`
	Platform string `json:"platform"`
	Account  string `json:"account"`
}

type BackfillDecisionSummary struct {
	UUID      string    `json:"uuid" db:"uuid"`
	Component string    `json:"component" db:"component"`
	Outcome   string    `json:"outcome" db:"outcome"` // completed or skipped
	Basis     string    `json:"basis" db:"basis"`     // source_runs, legacy_completion, legacy_skip
	DecidedAt time.Time `json:"decided_at" db:"-"`
}

type BackfillDecision struct {
	BackfillDecisionSummary
	BackfillSubject
	ProducerUUID *string         `json:"producer_uuid,omitempty"`
	SourceUUID   *string         `json:"source_uuid,omitempty"`
	SourceTable  *string         `json:"source_table,omitempty"`
	Evidence     json.RawMessage `json:"evidence"`
	CreatedAt    time.Time       `json:"created_at"`
}

type BackfillStatus struct {
	BackfillSubject
	Component       string                    `json:"component"`
	State           string                    `json:"state"` // needed, completed, skipped
	AccountComplete bool                      `json:"account_complete"`
	Decisions       []BackfillDecisionSummary `json:"decisions"`
}

// Only the application-authorized importer can accept legacy decisions. Record
// retains the source table's entire row, including result_json as its original
// string. SourceUUID identifies that input database across resumable imports.
type LegacyBackfillRecord struct {
	RootUUID   string          `json:"root_uuid"`
	SourceUUID string          `json:"source_uuid"`
	Table      string          `json:"table"`
	Record     json.RawMessage `json:"record"`
}

// Native completion must prove every URL in the requested component through
// this producer's immutable submissions and their actually completed windows.
// No producer-supplied success boolean or arbitrary log is sufficient.
type BackfillCompletion struct {
	UUID string `json:"uuid"`
	BackfillSubject
	Component    string             `json:"component"`
	Window       SourceWindow       `json:"window"`
	PolicySHA256 string             `json:"policy_sha256"`
	Requests     []SourceRunRequest `json:"requests"`
}

var (
	ErrBackfillInvalid    = errors.New("invalid source backfill decision")
	ErrBackfillConflict   = errors.New("source backfill identity has different evidence")
	ErrBackfillIncomplete = errors.New("source backfill coverage is incomplete")
)

type SourceBackfillReaderWriter interface {
	Find(context.Context, string) (*BackfillDecision, error)
	Status(context.Context, BackfillSubject, string) (*BackfillStatus, error)
	ImportLegacy(context.Context, LegacyBackfillRecord, time.Time) (*BackfillDecision, error)
	Complete(context.Context, string, BackfillCompletion, time.Time) (*BackfillDecision, error)
}
