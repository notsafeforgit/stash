package models

import (
	"context"
	"errors"
	"time"
)

type ArchiveEntityKind string

const (
	ArchivePerformer ArchiveEntityKind = "performer"
	ArchiveScene     ArchiveEntityKind = "scene"
	ArchiveImage     ArchiveEntityKind = "image"
	ArchiveFile      ArchiveEntityKind = "file"
	ArchiveGallery   ArchiveEntityKind = "gallery"
	ArchiveTag       ArchiveEntityKind = "tag"
	ArchiveStudio    ArchiveEntityKind = "studio"
	ArchiveGroup     ArchiveEntityKind = "group"
)

type ArchiveEntityState string

const (
	ArchiveEntityActive     ArchiveEntityState = "active"
	ArchiveEntityRedirected ArchiveEntityState = "redirected"
	ArchiveEntityDeleted    ArchiveEntityState = "deleted"
)

var ErrArchiveIdentityConflict = errors.New("archive identity changed or conflicts with another identity")

// ArchiveEntity names a library record independently of its local ID and bytes.
// LocalID is set only while the underlying record exists. OriginalID is retained
// as historical evidence; it must not be used to resolve a deleted identity.
type ArchiveEntity struct {
	UUID       string
	Kind       ArchiveEntityKind
	State      ArchiveEntityState
	Revision   int
	LocalID    *int
	OriginalID *int
	RedirectTo *string
	CreatedAt  time.Time
	RetiredAt  *time.Time
}

type ArchiveEntityReaderWriter interface {
	Find(context.Context, string) (*ArchiveEntity, error)
	FindByLocalID(context.Context, ArchiveEntityKind, int) (*ArchiveEntity, error)
	Resolve(context.Context, string) (*ArchiveEntity, error)
	// AdoptUUID preserves the old UUID as a redirect. Used by audited import
	// when a pre-existing catalog UUID must become the canonical identity.
	AdoptUUID(context.Context, string, string, int) (*ArchiveEntity, error)
	// Redirect records a merge in the caller's transaction. The same transaction
	// must transfer relationships and delete the source record.
	Redirect(context.Context, string, string, int) error
}
