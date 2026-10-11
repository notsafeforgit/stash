package models

import (
	"context"
	"errors"
)

// ArchivePageFilter counts the same filtered identities as a cursor reader.
// Cursor is excluded from Total and used only to locate the displayed page.
type ArchivePageFilter struct {
	Resource, Subject, Cursor                       string
	Query, State, Kind, Namespace, Ownership, Scope string
	PostUUID, URL, Value, Collection                string
}

type ArchivePageInfo struct {
	Total  int64 `json:"total" db:"total"`
	Offset int64 `json:"offset" db:"offset"`
}

var ErrArchivePageInvalid = errors.New("invalid archive pagination request")

type ArchivePaginationReader interface {
	PageInfo(context.Context, ArchivePageFilter) (*ArchivePageInfo, error)
}
