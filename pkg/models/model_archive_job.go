package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Archive jobs are persisted work, separate from the legacy in-memory progress
// queue. Only a service for the named kind may interpret Arguments.
const ArchiveJobVerifyMedia = "media.verify"

type ArchiveJob struct {
	Sequence    int64           `json:"sequence"`
	UUID        string          `json:"uuid"`
	Kind        string          `json:"kind"`
	WorkKey     string          `json:"work_key"`
	ResourceKey string          `json:"resource_key"`
	Arguments   json.RawMessage `json:"arguments"`
	State       string          `json:"state"`
	Revision    int64           `json:"revision"`
	Priority    int             `json:"priority"`
	Fence       int64           `json:"fence"`
	MaxAttempts int             `json:"max_attempts"`
	AvailableAt time.Time       `json:"available_at"`
	OwnerUUID   string          `json:"owner_uuid,omitempty"`
	LeaseUntil  *time.Time      `json:"lease_until,omitempty"`
	Progress    json.RawMessage `json:"progress"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   string          `json:"error_code"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type ArchiveJobSubmission struct {
	RequestUUID string
	Kind        string
	WorkKey     string // service-defined SHA-256 of equivalent work
	ResourceKey string // service-defined SHA-256 of a shared destination/target
	Arguments   json.RawMessage
	Priority    int
	MaxAttempts int
	AvailableAt time.Time // zero makes new work immediately available
}

type ArchiveJobLease struct {
	JobUUID   string `json:"job_uuid"`
	OwnerUUID string `json:"owner_uuid"`
	Fence     int64  `json:"fence"`
}

func (j ArchiveJob) Lease() ArchiveJobLease {
	return ArchiveJobLease{JobUUID: j.UUID, OwnerUUID: j.OwnerUUID, Fence: j.Fence}
}

type ArchiveJobAttempt struct {
	JobUUID   string          `json:"job_uuid"`
	Fence     int64           `json:"fence"`
	OwnerUUID string          `json:"owner_uuid"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   *time.Time      `json:"ended_at,omitempty"`
	Outcome   string          `json:"outcome"`
	Result    json.RawMessage `json:"result"`
	ErrorCode string          `json:"error_code"`
}

type ArchiveJobOutcome struct {
	State     string // succeeded, failed, or retry
	Result    json.RawMessage
	ErrorCode string // bounded machine-readable code; no raw worker stderr/secrets
	RetryAt   time.Time
}

var (
	ErrArchiveJobConflict = errors.New("archive job or submission changed")
	ErrArchiveJobLease    = errors.New("archive job lease expired or is no longer owned")
	ErrArchiveJobCapacity = errors.New("archive job queue is full")
)

// Mutations require the caller's managed write transaction. Times are supplied
// by the server clock, never accepted from a producer as scheduling authority.
type ArchiveJobReaderWriter interface {
	Submit(context.Context, ArchiveJobSubmission, time.Time, int) (*ArchiveJob, error)
	Find(context.Context, string) (*ArchiveJob, error)
	FindSubmission(context.Context, string) (*ArchiveJob, error)
	List(context.Context, string, string, int64, int) ([]ArchiveJob, error)
	Attempts(context.Context, string, int64, int) ([]ArchiveJobAttempt, error)
	Claim(context.Context, string, string, time.Time, time.Duration) (*ArchiveJob, error)
	Renew(context.Context, ArchiveJobLease, time.Time, time.Duration) (*ArchiveJob, error)
	CheckLease(context.Context, ArchiveJobLease, time.Time) (*ArchiveJob, error)
	Progress(context.Context, ArchiveJobLease, time.Time, json.RawMessage) (*ArchiveJob, error)
	Finish(context.Context, ArchiveJobLease, time.Time, ArchiveJobOutcome) (*ArchiveJob, error)
	Cancel(context.Context, string, int64, time.Time) (*ArchiveJob, error)
	Recover(context.Context, time.Time, int) (int, error)
}
