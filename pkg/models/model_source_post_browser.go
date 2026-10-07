package models

import (
	"errors"
	"time"
)

// SourcePostFilter pages the archive or performs an exact lookup. A URL is
// retained evidence and can legitimately identify more than one native post.
// Identifier namespaces are explicit; an unqualified number is not an identity.
type SourcePostFilter struct {
	After      string
	Limit      int
	PostUUID   string
	URL        string
	Identifier *SourcePostIdentifier
}

type SourcePostIdentifierSummary struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

// SourcePostSummary projects compact metadata without reconstructing payloads,
// profile bodies, attachment lists or account ownership for every list item.
type SourcePostSummary struct {
	RequestedUUID   string                        `json:"requested_uuid"`
	UUID            string                        `json:"uuid"`
	State           string                        `json:"state"`
	Revision        int                           `json:"revision"`
	CreatedAt       time.Time                     `json:"created_at"`
	Identifiers     []SourcePostIdentifierSummary `json:"identifiers"`
	MoreIdentifiers bool                          `json:"more_identifiers"`
	URLs            []SourcePostURL               `json:"urls"`
	MoreURLs        bool                          `json:"more_urls"`
	LatestCapture   *SourcePostReviewCapture      `json:"latest_capture"`
}

// SourcePostAlbum preserves a disabled choice and the selected gallery's
// current identity, including deletion. Inspection does not create an album.
type SourcePostAlbum struct {
	PostUUID      string                 `json:"post_uuid"`
	DecisionUUID  string                 `json:"decision_uuid"`
	Revision      int                    `json:"revision"`
	State         string                 `json:"state"`
	GalleryUUID   *string                `json:"gallery_uuid"`
	Gallery       *SourcePostLibraryItem `json:"gallery"`
	SelectionUUID *string                `json:"selection_uuid"`
	Origin        string                 `json:"origin"`
	Reason        string                 `json:"reason"`
	CreatedAt     time.Time              `json:"created_at"`
}

type SourcePostLibraryItem struct {
	UUID           string             `json:"uuid"`
	Kind           ArchiveEntityKind  `json:"kind"`
	State          ArchiveEntityState `json:"state"`
	Revision       int                `json:"revision"`
	LocalID        *int               `json:"local_id"`
	Title          string             `json:"title"`
	TitleTruncated bool               `json:"title_truncated"`
}

// Each canonical media identity appears once, including rejected choices and
// historical evidence. Attachment links alone do not override an explicit
// unlinked/conflicting post association. Post metadata is fetched separately.
type SourcePostMediaItem struct {
	RequestedPostUUID   string                      `json:"requested_post_uuid"`
	Media               *SourcePostLibraryItem      `json:"media"`
	Association         *SourcePostMediaAssociation `json:"association"`
	HasRetainedEvidence bool                        `json:"has_retained_evidence"`
	LinkedAttachments   int                         `json:"linked_attachments"`
}

var ErrSourcePostBrowseInvalid = errors.New("invalid source post lookup")
