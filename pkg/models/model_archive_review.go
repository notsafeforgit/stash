package models

import (
	"context"
	"errors"
)

type ArchiveReviewFilter struct {
	Kind  string
	After string
	Limit int
}

// Queue entries describe current decisions. Historical importer warnings and
// browser delivery records have independent readers and do not populate this list.
type ArchiveReviewItem struct {
	UUID    string              `json:"uuid"`
	Reasons []string            `json:"reasons"`
	Account *AccountReviewState `json:"account,omitempty"`
	Post    *SourcePostSummary  `json:"post,omitempty"`
	// Collection is a source awaiting the owner's decision.
	Collection *SourceCollection `json:"collection,omitempty"`
}

// Next advances through unresolved identities. Settled candidates are filtered
// before pagination, so every non-final page contains Limit review items.
type ArchiveReviewPage struct {
	Kind    string              `json:"kind"`
	Items   []ArchiveReviewItem `json:"items"`
	Checked int                 `json:"checked"`
	Next    string              `json:"next,omitempty"`
}

// ArchiveReviewSummary counts new decisions cheaply enough for navigation.
// Sources stops counting at ArchiveReviewSummaryLimit.
type ArchiveReviewSummary struct {
	Sources int `json:"sources"`
}

const ArchiveReviewSummaryLimit = 100

var ErrArchiveReviewInvalid = errors.New("invalid archive review queue request")

type ArchiveReviewReader interface {
	Queue(context.Context, ArchiveReviewFilter) (*ArchiveReviewPage, error)
	Summary(context.Context) (*ArchiveReviewSummary, error)
}
