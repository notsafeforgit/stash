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
	// ThreadPlans retain each post's own selection and guards. They are computed
	// by a read-only preview and committed together by Sync.
	ThreadPlans []*SourceGalleryPreview
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
	PreviewAssociationReview(context.Context, GalleryAssociationReviewInput) (*GalleryAssociationReviewPreview, error)
	ApplyAssociationReview(context.Context, GalleryAssociationReviewApplyInput) (*GalleryAssociationReview, bool, error)
	AssociationReview(context.Context, string) (*GalleryAssociationReview, error)
	AssociationView(context.Context, string) (*SourcePostAlbum, error)
	ReadAlbum(context.Context, string, int, int) (*SourceAlbumPage, error)
	PostsForGallery(context.Context, string, string, int) (*SourceGalleryPosts, error)
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
