package models

import (
	"context"
	"errors"
	"time"
)

// SourceWindow is a half-open published-time interval. A nil Since means all
// history before Until. Until is always explicit so a replay has stable meaning.
type SourceWindow struct {
	Since *time.Time `json:"since"`
	Until time.Time  `json:"until"`
}

// PolicySHA256 identifies the effective, secret-free worker configuration and
// adapter version. It never contains executable commands or website logins.
type SourceRunRequest struct {
	RequestUUID        string       `json:"request_uuid"`
	CollectionUUID     string       `json:"collection_uuid"`
	CollectionRevision int          `json:"collection_revision"`
	Operation          string       `json:"operation"` // download or enrich
	PolicySHA256       string       `json:"policy_sha256"`
	Window             SourceWindow `json:"window"`
	CooldownSeconds    int          `json:"cooldown_seconds"`
}

type SourceRun struct {
	Sequence           int64             `json:"sequence"`
	UUID               string            `json:"uuid"`
	CollectionUUID     string            `json:"collection_uuid"`
	CollectionRevision int               `json:"collection_revision"`
	TargetURL          string            `json:"target_url"`
	PathPrefix         string            `json:"path_prefix"`
	RootUUID           *string           `json:"root_uuid"`
	RootRevision       int               `json:"root_revision"`
	Operation          string            `json:"operation"`
	PolicySHA256       string            `json:"policy_sha256"`
	CooldownSeconds    int               `json:"cooldown_seconds"`
	State              string            `json:"state"`
	Revision           int64             `json:"revision"`
	Fence              int64             `json:"fence"`
	Failures           int               `json:"failures"`
	Pending            []SourceWindow    `json:"pending"`
	Completed          []SourceWindow    `json:"completed"`
	Window             *SourceWindow     `json:"window"`
	ProducerUUID       string            `json:"producer_uuid,omitempty"`
	OwnerUUID          string            `json:"owner_uuid,omitempty"`
	LeaseUntil         *time.Time        `json:"lease_until,omitempty"`
	AvailableAt        time.Time         `json:"available_at"`
	Progress           SourceRunProgress `json:"progress"`
	ErrorCode          string            `json:"error_code"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// SourceRunCandidate is a discovery hint, not a lease. The worker must still
// claim the run before reading a source or changing download state.
type SourceRunCandidate struct {
	Sequence int64  `json:"sequence" db:"id"`
	UUID     string `json:"uuid" db:"uuid"`
}

type SourceRunProgress struct {
	ItemsSeen      int64  `json:"items_seen"`
	FilesCompleted int64  `json:"files_completed"`
	Cursor         string `json:"cursor"` // source key, not a command or raw log
}

type SourceRunLease struct {
	RunUUID      string `json:"run_uuid"`
	ProducerUUID string `json:"producer_uuid"`
	OwnerUUID    string `json:"owner_uuid"`
	Fence        int64  `json:"fence"`
}

func (r SourceRun) Lease() SourceRunLease {
	return SourceRunLease{RunUUID: r.UUID, ProducerUUID: r.ProducerUUID, OwnerUUID: r.OwnerUUID, Fence: r.Fence}
}

type SourceRunAttempt struct {
	SourceRunLease
	Window    SourceWindow      `json:"window"`
	Progress  SourceRunProgress `json:"progress"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   *time.Time        `json:"ended_at"`
	Outcome   string            `json:"outcome"`
	ErrorCode string            `json:"error_code"`
}

type SourceRunOutcome struct {
	State             string `json:"state"` // succeeded, retry, deferred
	ErrorCode         string `json:"error_code"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

var (
	ErrSourceRunInvalid  = errors.New("invalid source run request")
	ErrSourceRunConflict = errors.New("source run or request changed")
	ErrSourceRunLease    = errors.New("source run lease expired or is no longer owned")
	ErrSourceRunCapacity = errors.New("source run queue or window capacity reached")
)

// The caller owns a managed transaction and the server clock. Claim validates
// the current collection/root, and serializes targets and overlapping paths.
type SourceRunReaderWriter interface {
	Submit(context.Context, string, SourceRunRequest, time.Time, int) (*SourceRun, error)
	Find(context.Context, string) (*SourceRun, error)
	List(context.Context, string, *string, int64, int) ([]SourceRun, error)
	Ready(context.Context, []string, string, string, int64, int, time.Time) ([]SourceRunCandidate, error)
	Attempts(context.Context, string, int64, int) ([]SourceRunAttempt, error)
	Claim(context.Context, string, string, string, string, time.Time, time.Duration) (*SourceRun, error)
	CheckLease(context.Context, SourceRunLease, time.Time) (*SourceRun, error)
	Renew(context.Context, SourceRunLease, time.Time, time.Duration) (*SourceRun, error)
	Progress(context.Context, SourceRunLease, SourceRunProgress, time.Time) (*SourceRun, error)
	Finish(context.Context, SourceRunLease, SourceRunOutcome, time.Time) (*SourceRun, error)
	Review(context.Context, string, int64, string, time.Time) (*SourceRun, error) // retry or cancel
	Recover(context.Context, time.Time, int) (int, error)
}
