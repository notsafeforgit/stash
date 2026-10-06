package models

import (
	"context"
	"errors"
	"time"
)

// A post association makes no claim about attachment identity or album order.
// Explicit unlinked decisions suppress attachment-derived associations too.
// Undecided returns that pair to attachment evidence. Conflicting decisions
// brought together by a media merge require a new, explicit review.
type SourcePostMediaAssociation struct {
	PostUUID      string                    `json:"post_uuid"`
	PostRevision  int                       `json:"post_revision"`
	PostState     string                    `json:"post_state"`
	MediaUUID     string                    `json:"media_uuid"`
	MediaRevision int                       `json:"media_revision"`
	MediaState    ArchiveEntityState        `json:"media_state"`
	State         string                    `json:"state"`
	Decisions     []SourcePostMediaDecision `json:"decisions"`
}

func (a SourcePostMediaAssociation) Suppressed() bool {
	return a.State == "unlinked" || a.State == "conflict"
}

type SourcePostMediaInput struct {
	UUID                  string   `json:"uuid"`
	PostUUID              string   `json:"post_uuid"`
	MediaUUID             string   `json:"media_uuid"`
	ExpectedPostRevision  int      `json:"expected_post_revision"`
	ExpectedMediaRevision int      `json:"expected_media_revision"`
	ExpectedDecisions     []string `json:"expected_decisions"`
	State                 string   `json:"state"`
	Origin                string   `json:"origin"`
	Reason                string   `json:"reason"`
}

// UUID is also the durable request receipt. A retry must send the exact input.
type SourcePostMediaDecision struct {
	UUID          string    `json:"uuid"`
	PostUUID      string    `json:"post_uuid"`
	MediaUUID     string    `json:"media_uuid"`
	PostRevision  int       `json:"post_revision"`
	MediaRevision int       `json:"media_revision"`
	State         string    `json:"state"`
	Origin        string    `json:"origin"`
	Reason        string    `json:"reason"`
	CreatedAt     time.Time `json:"created_at"`
}

var (
	ErrSourcePostMediaInvalid  = errors.New("invalid source post media association")
	ErrSourcePostMediaConflict = errors.New("source post or media association changed; review again")
	ErrSourcePostMediaReplay   = errors.New("post media request UUID has different contents")
)

type SourcePostMediaReaderWriter interface {
	Association(context.Context, string, string) (*SourcePostMediaAssociation, error)
	Decision(context.Context, string) (*SourcePostMediaDecision, error)
	History(context.Context, string, string, int, int) ([]SourcePostMediaDecision, error)
	// Decide runs in a managed transaction and updates an existing source
	// gallery through the same membership service, preserving manual choices.
	Decide(context.Context, SourcePostMediaInput) (*SourcePostMediaDecision, error)
	ValidateCapture(context.Context, string, string, string) error
	BackfillPosts(context.Context, string, int) ([]string, error)
	PreviewBackfill(context.Context, string) (*SourcePostMediaMatchPreview, error)
	Backfill(context.Context, SourcePostMediaBackfillInput) (*SourcePostMediaBackfillResult, error)
	BackfillResult(context.Context, string) (*SourcePostMediaBackfillResult, error)
	MatchedEvidence(context.Context, string) ([]SourcePostMediaMatchedEvidence, error)
}
