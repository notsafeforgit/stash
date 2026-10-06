package models

import "errors"

// Comparison inspects current identity and association choices. It neither
// establishes that two posts are identical nor authorizes a merge. Original
// captures, receipts and pending work remain scoped to their original posts.
type SourcePostComparison struct {
	Left       SourcePostComparisonState      `json:"left"`
	Right      SourcePostComparisonState      `json:"right"`
	SharedURLs []string                       `json:"shared_urls"`
	Conflicts  []SourcePostComparisonConflict `json:"conflicts"`
}

type SourcePostComparisonState struct {
	UUID          string                           `json:"uuid"`
	State         string                           `json:"state"`
	Revision      int                              `json:"revision"`
	Identifiers   []SourcePostIdentifierSummary    `json:"identifiers"`
	URLs          []string                         `json:"urls"`
	LatestCapture *SourcePostReviewCapture         `json:"latest_capture"`
	Selection     *SourcePostComparisonSelection   `json:"selection"`
	Album         *SourcePostAlbum                 `json:"album"`
	Attachments   []SourcePostComparisonAttachment `json:"attachments"`
	MediaChoices  []SourcePostComparisonMedia      `json:"media_choices"`
}

type SourcePostComparisonSelection struct {
	DecisionUUID  string                     `json:"decision_uuid"`
	Mode          string                     `json:"mode"`
	Origin        string                     `json:"origin"`
	CaptureUUID   *string                    `json:"capture_uuid"`
	Complete      bool                       `json:"complete"`
	DeclaredAlbum bool                       `json:"declared_album"`
	ExpectedCount *int                       `json:"expected_count"`
	Entries       []SourcePostComparisonSlot `json:"entries"`
}

type SourcePostComparisonSlot struct {
	Position       int    `json:"position"`
	AttachmentUUID string `json:"attachment_uuid"`
	MediaKind      string `json:"media_kind"`
}

// Current decisions retain their original media UUID. Resolution is a separate
// current identity, so merged and deleted media are visible without rewriting
// historical choices or returning a stale local library ID.
type SourcePostComparisonAttachment struct {
	UUID      string                         `json:"uuid"`
	Namespace string                         `json:"namespace"`
	Value     string                         `json:"value"`
	Revision  int                            `json:"revision"`
	Choice    *AttachmentMediaReviewDecision `json:"choice"`
	Media     *SourcePostComparisonEntity    `json:"media"`
}

type SourcePostComparisonMedia struct {
	Decision SourcePostMediaDecision    `json:"decision"`
	Media    SourcePostComparisonEntity `json:"media"`
}

type SourcePostComparisonEntity struct {
	UUID     string             `json:"uuid"`
	Kind     ArchiveEntityKind  `json:"kind"`
	State    ArchiveEntityState `json:"state"`
	Revision int                `json:"revision"`
	LocalID  *int               `json:"local_id"`
}

type SourcePostComparisonConflict struct {
	Kind      string   `json:"kind"`
	PostUUID  string   `json:"post_uuid,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
	Values    []string `json:"values,omitempty"`
	MediaUUID string   `json:"media_uuid,omitempty"`
	Position  *int     `json:"position,omitempty"`
}

var (
	ErrSourcePostComparisonInvalid = errors.New("invalid source post comparison")
	ErrSourcePostComparisonMissing = errors.New("source post comparison requires two existing posts")
	ErrSourcePostComparisonLimit   = errors.New("source post comparison exceeds its review limit")
)
