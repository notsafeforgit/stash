package models

import (
	"context"
	"errors"
	"time"
)

// Activity reads do not claim, recover, cancel or retry work. The worker's
// service remains responsible for interpreting its arguments and results.
type ArchiveActivityPage struct {
	Before int64
	Limit  int
}

type ArchiveJobActivityFilter struct {
	ArchiveActivityPage
	Kind  string
	State string
}

type SourceRunActivityFilter struct {
	ArchiveActivityPage
	CollectionUUID string
	State          string
}

type ArchiveJobActivity struct {
	Sequence     int64      `json:"sequence"`
	UUID         string     `json:"uuid"`
	Kind         string     `json:"kind"`
	State        string     `json:"state"`
	Revision     int64      `json:"revision"`
	AttemptCount int64      `json:"attempt_count"`
	MaxAttempts  int        `json:"max_attempts"`
	AvailableAt  time.Time  `json:"available_at"`
	LeaseUntil   *time.Time `json:"lease_until"`
	ErrorCode    string     `json:"error_code"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type ArchiveActivityAttempt struct {
	Number    int64      `json:"number"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Outcome   string     `json:"outcome"`
	ErrorCode string     `json:"error_code"`
}

// References retain the subject chosen at admission. Collection revisions are
// original definitions; post/media readers may additionally resolve redirects.
type ArchiveActivityReference struct {
	Kind     string `db:"kind"`
	UUID     string `db:"uuid"`
	Revision int    `db:"revision"`
}

type SourceRunActivity struct {
	CanonicalCollectionUUID string     `json:"canonical_collection_uuid,omitempty"`
	Sequence                int64      `json:"sequence"`
	UUID                    string     `json:"uuid"`
	CollectionUUID          string     `json:"collection_uuid"`
	CollectionRevision      int        `json:"collection_revision"`
	CollectionLabel         string     `json:"collection_label"`
	TargetURL               string     `json:"target_url"`
	Operation               string     `json:"operation"`
	State                   string     `json:"state"`
	Revision                int64      `json:"revision"`
	AttemptCount            int64      `json:"attempt_count"`
	Failures                int        `json:"failures"`
	PendingWindows          int        `json:"pending_windows"`
	CompletedWindows        int        `json:"completed_windows"`
	AvailableAt             time.Time  `json:"available_at"`
	LeaseUntil              *time.Time `json:"lease_until"`
	ErrorCode               string     `json:"error_code"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

var ErrArchiveActivityInvalid = errors.New("invalid archive activity request")

// Newest-first keyset pages use sequence numbers (attempt numbers for history),
// not mutable update timestamps. Before=0 starts at the newest record. Filters
// inspect current state; separately requested pages are not one database snapshot.
type ArchiveActivityReader interface {
	Jobs(context.Context, ArchiveJobActivityFilter) ([]ArchiveJobActivity, error)
	Job(context.Context, string) (*ArchiveJobActivity, error)
	JobReferences(context.Context, string) ([]ArchiveActivityReference, error)
	JobAttempts(context.Context, string, ArchiveActivityPage) ([]ArchiveActivityAttempt, error)
	Runs(context.Context, SourceRunActivityFilter) ([]SourceRunActivity, error)
	Run(context.Context, string) (*SourceRunActivity, error)
	RunAttempts(context.Context, string, ArchiveActivityPage) ([]ArchiveActivityAttempt, error)
}
