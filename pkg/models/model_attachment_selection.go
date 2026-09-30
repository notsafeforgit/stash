package models

import (
	"errors"
	"time"
)

type AttachmentSelectionDecision struct {
	UUID          string
	PostUUID      string
	Revision      int
	Mode          string // automatic, pinned, disabled
	Origin        string // ingest, review, migration
	Reason        string
	CaptureUUID   *string
	ManifestUUIDs []string
	CreatedAt     time.Time
}

// A selection references immutable source lists. It does not copy post payloads
// or infer download state, depicted performers, or gallery membership.
type AttachmentSelection struct {
	Decision      AttachmentSelectionDecision
	Complete      bool
	DeclaredAlbum bool
	ExpectedCount *int
	Entries       []SourceAttachmentManifestEntry
}

func (s AttachmentSelection) IsAlbum() bool {
	return s.Decision.Mode != "disabled" && (s.DeclaredAlbum || len(s.Entries) > 1 || (s.ExpectedCount != nil && *s.ExpectedCount > 1))
}

type AttachmentSelectionInput struct {
	PostUUID             string
	ExpectedPostRevision int
	CaptureUUID          string // required except when disabling
	Mode                 string
	Origin               string
	Reason               string
}

type AttachmentManifestConflict struct {
	Kind     string // count, position, media-kind, or count-position
	Position *int
}

type AttachmentSelectionPreview struct {
	PostUUID     string
	PostRevision int
	Current      *AttachmentSelection
	Proposed     *AttachmentSelection
	CaptureUUID  string
	Protected    bool
	Changed      bool
	Conflicts    []AttachmentManifestConflict
}

var (
	ErrAttachmentSelectionConflict  = errors.New("attachment selection or source post changed")
	ErrAttachmentSelectionProtected = errors.New("attachment selection is pinned or disabled")
	ErrAttachmentManifestConflict   = errors.New("source attachment lists conflict and require review")
)
