package models

import (
	"context"
	"errors"
	"time"
)

var ErrLibraryBulkInterrupted = errors.New("bulk job was cancelled, completed or superseded by another server")

const (
	MaxLibraryBulkJobs    = 32
	MaxLibraryBulkTargets = 250000
	MaxLibraryBulkBytes   = 64 << 10
	MaxLibraryBulkErrors  = 20
	MaxLibraryHookTries   = 3
)

// LibraryBulkAdmission is saved with the ordinary library job row in one
// transaction. Selectors and live services never enter this durable format.
type LibraryBulkAdmission struct {
	ID             string
	Description    string
	AddedAt        time.Time
	Generation     int
	CommandVersion int
	CommandKind    string
	CommandJSON    []byte
	NoticeJSON     []byte
	ReferencesJSON []byte
	Targets        []LibraryBulkTarget
}

type LibraryBulkJob struct {
	ID                 string `db:"job_id"`
	Generation         int    `db:"generation"`
	CommandVersion     int    `db:"command_version"`
	CommandKind        string `db:"command_kind"`
	CommandJSON        []byte `db:"command_json"`
	NoticeJSON         []byte `db:"notice_json"`
	ReferencesJSON     []byte `db:"references_json"`
	Total              int    `db:"total"`
	Processed          int    `db:"processed"`
	Updated            int    `db:"updated"`
	Failed             int    `db:"failed"`
	NotificationFailed int    `db:"notification_failed"`
	ErrorsJSON         []byte `db:"errors_json"`
}

type LibraryBulkTarget struct {
	Ordinal      int    `db:"ordinal"`
	EntityID     int    `db:"entity_id"`
	EntityUUID   string `db:"entity_uuid"`
	State        string `db:"state"`
	HookAttempts int    `db:"hook_attempts"`
}

type LibraryBulkError struct {
	EntityUUID string `json:"id"`
	Stage      string `json:"stage"`
	Message    string `json:"message"`
}

type LibraryBulkJobReaderWriter interface {
	Admit(context.Context, LibraryBulkAdmission) error
	Pending(context.Context) ([]LibraryBulkJob, error)
	Find(context.Context, string) (*LibraryBulkJob, error)
	Next(context.Context, string, int) (*LibraryBulkTarget, error)
	Applied(context.Context, string, int, LibraryBulkTarget) (int, error)
	Failed(context.Context, string, int, LibraryBulkTarget, string) (int, error)
	StartNotification(context.Context, string, int, LibraryBulkTarget) error
	FinishNotification(context.Context, string, int, LibraryBulkTarget, *string) error
}
