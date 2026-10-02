package models

import (
	"context"
	"errors"
	"time"
)

type SourceGalleryDecision struct {
	UUID          string
	PostUUID      string
	Revision      int
	State         string // linked or disabled
	GalleryUUID   *string
	SelectionUUID *string
	Origin        string // source, review, migration
	Reason        string
	CreatedAt     time.Time
}

type SourceGalleryChoiceInput struct {
	PostUUID                string
	ExpectedPostRevision    int
	State                   string
	GalleryUUID             string
	ExpectedGalleryRevision int
	Origin                  string // review or migration; source creation is internal
	Reason                  string
}

type SourceAlbumEntry struct {
	Position       int
	AttachmentUUID string
	MediaUUID      *string
	MediaKind      ArchiveEntityKind
	MediaRevision  int
	Status         string // linked, unselected, unlinked, deleted, excluded; file availability is separate
}

type SourceGalleryPreview struct {
	PostUUID      string
	PostRevision  int
	SelectionUUID string
	Association   *SourceGalleryDecision
	Gallery       *ArchiveEntity
	Action        string // create, sync, disabled, ineligible, review
	Signature     string
	Title         string
	Details       string
	Date          *Date
	Entries       []SourceAlbumEntry
	Add           []ArchiveEntity
	Remove        []ArchiveEntity
}

type SourceGallerySyncResult struct {
	GalleryUUID string
	GalleryID   *int
	Created     bool
	Added       []string
	Removed     []string
	Action      string
}

type GalleryMembershipEvent struct {
	UUID          string
	Sequence      int
	GalleryUUID   string
	MediaUUID     string
	State         string
	Origin        string
	PostUUID      *string
	SelectionUUID *string
	CreatedAt     time.Time
}

var ErrSourceGalleryConflict = errors.New("source album or gallery changed; review a fresh preview")

type SourceGalleryReaderWriter interface {
	Association(context.Context, string) (*SourceGalleryDecision, error)
	AssociationHistory(context.Context, string, int, int) ([]SourceGalleryDecision, error)
	DecideAssociation(context.Context, SourceGalleryChoiceInput) (*SourceGalleryDecision, error)
	Preview(context.Context, string) (*SourceGalleryPreview, error)
	Sync(context.Context, string, string) (*SourceGallerySyncResult, error)
	PreviewBackfill(context.Context, string, string) (*SourceAlbumBackfillPreview, error)
	// Backfill requires a managed write transaction. Its caller must checkpoint
	// the result with durable after-success work before committing publication.
	Backfill(context.Context, string, string, string) (*SourceAlbumBackfillResult, error)
	MembershipHistory(context.Context, string, int, int) ([]GalleryMembershipEvent, error)
}
