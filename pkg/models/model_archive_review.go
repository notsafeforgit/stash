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
	UUID    string                 `json:"uuid"`
	Reasons []string               `json:"reasons"`
	Account *AccountReviewState    `json:"account,omitempty"`
	Post    *SourcePostSummary     `json:"post,omitempty"`
	Media   *SourcePostLibraryItem `json:"media,omitempty"`
}

// Next advances through inspected candidate identities, including settled
// candidates. An empty Items with Next set is not an empty queue.
type ArchiveReviewPage struct {
	Kind    string              `json:"kind"`
	Items   []ArchiveReviewItem `json:"items"`
	Checked int                 `json:"checked"`
	Next    string              `json:"next,omitempty"`
}

var ErrArchiveReviewInvalid = errors.New("invalid archive review queue request")

type ArchiveReviewReader interface {
	Queue(context.Context, ArchiveReviewFilter) (*ArchiveReviewPage, error)
}
