package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// A journal snapshot is retained migration evidence, not a source-run lease or
// completion proof. UUID identifies one frozen input at the cutover boundary;
// SourceUUID identifies the original database across snapshots.
type ScanJournalInput struct {
	UUID       string          `json:"uuid"`
	RootUUID   string          `json:"root_uuid"`
	SourceUUID string          `json:"source_uuid"`
	Document   json.RawMessage `json:"document"`
}

type ScanJournal struct {
	UUID        string          `json:"uuid" db:"uuid"`
	RootUUID    string          `json:"root_uuid" db:"root_uuid"`
	SourceUUID  string          `json:"source_uuid" db:"source_uuid"`
	InputSHA256 string          `json:"input_sha256" db:"input_sha256"`
	CapturedAt  string          `json:"captured_at" db:"captured_at"`
	RecordCount int             `json:"record_count" db:"record_count"`
	Inventory   json.RawMessage `json:"inventory" db:"-"`
	CreatedAt   string          `json:"created_at" db:"created_at"`
}

type ScanJournalRecord struct {
	Sequence    int64           `json:"sequence" db:"id"`
	UUID        string          `json:"uuid" db:"uuid"`
	JournalUUID string          `json:"journal_uuid" db:"journal_uuid"`
	Table       string          `json:"table" db:"source_table"`
	SourceKey   string          `json:"source_key" db:"source_key"`
	Context     string          `json:"context" db:"context"`
	TargetURL   string          `json:"target_url" db:"target_url"`
	Disposition string          `json:"disposition" db:"disposition"` // pending_binding, historical, review
	Summary     json.RawMessage `json:"summary" db:"-"`
	Evidence    json.RawMessage `json:"evidence,omitempty" db:"-"`
}

var (
	ErrScanJournalInvalid  = errors.New("invalid legacy scan journal snapshot")
	ErrScanJournalConflict = errors.New("scan journal identity has different evidence")
)

type ScanJournalReaderWriter interface {
	Import(context.Context, ScanJournalInput, time.Time) (*ScanJournal, error)
	Find(context.Context, string) (*ScanJournal, error)
	Records(context.Context, string, string, int64, int) ([]ScanJournalRecord, error)
	Record(context.Context, string) (*ScanJournalRecord, error)
}
