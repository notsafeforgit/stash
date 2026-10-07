package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type SourceProfileBody struct {
	Hash      string          `json:"hash"`
	Namespace string          `json:"namespace"`
	Body      json.RawMessage `json:"body"`
}

// Profile references are separate records, never sentinel objects embedded in
// arbitrary source JSON. Paths are RFC 6901 pointers to existing null members.
type SourceProfileReference struct {
	Part string `json:"part"`
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type SourceCapturePayload struct {
	Shared   json.RawMessage          `json:"shared"`
	Patch    json.RawMessage          `json:"patch"`
	Profiles []SourceProfileBody      `json:"profiles"`
	Refs     []SourceProfileReference `json:"profile_refs"`
}

type SourcePost struct {
	UUID      string
	State     string
	Revision  int
	CreatedAt time.Time
}

// A qualified source identifier may be a service ID or a legacy catalog key.
// URLs and titles are evidence, not automatically unique post identifiers.
type SourcePostIdentifier struct {
	Namespace string
	Value     string
}

type SourcePostMetadata struct {
	Title        *string `json:"title,omitempty"`
	OriginalText *string `json:"original_text,omitempty"`
	PublishedAt  *string `json:"published_at,omitempty"`
	DateBasis    *string `json:"date_basis,omitempty"`
	Language     *string `json:"language,omitempty"`
}

type SourceCaptureInput struct {
	UUID       string
	PostUUID   string
	Origin     string
	Platform   string
	CapturedAt time.Time
	// A zero CapturedAt means the original observation time was not recorded.
	// RecordedAt then records when the archive received this retained evidence.
	RecordedAt       *time.Time
	ExtractorVersion *string
	RetentionPolicy  string
	Metadata         SourcePostMetadata
	Payload          SourceCapturePayload
	Contexts         []SourceCaptureContext `json:",omitempty"`
}

type SourceCapture struct {
	UUID             string
	PostUUID         string
	RevisionUUID     string
	Origin           string
	Platform         string
	CapturedAt       time.Time
	RecordedAt       *time.Time
	ExtractorVersion *string
	RetentionPolicy  string
	StructureVersion string
	Metadata         SourcePostMetadata
	// Summary queries omit payloads; FindCapture loads and verifies them.
	Payload *SourceCapturePayload
	// Contexts identifies older captures embedded in this payload. Their source
	// observation times remain independent of this capture's fetch time.
	Contexts []SourceCaptureContext `json:",omitempty"`
}

type SourceCaptureContext struct {
	CaptureUUID string `json:"capture_uuid" db:"capture_uuid"`
	Path        string `json:"path" db:"path"`
	ParentUUID  string `json:"parent_capture_uuid" db:"parent_capture_uuid"`
}

type SourceCaptureCursor struct {
	CapturedAt time.Time
	RecordedAt *time.Time
	UUID       string
}

// MarshalJSON preserves an unknown observation time as null, never year one or
// the time the archive happened to import an older saved result.
func (c SourceCapture) MarshalJSON() ([]byte, error) {
	type alias SourceCapture
	var observed *time.Time
	if !c.CapturedAt.IsZero() {
		observed = &c.CapturedAt
	}
	return json.Marshal(struct {
		alias
		CapturedAt *time.Time
	}{alias(c), observed})
}

var (
	ErrSourcePostConflict   = errors.New("source post identifier or revision conflicts")
	ErrSourcePostForgotten  = errors.New("source post has a retained deletion tombstone")
	ErrSourceCaptureReplay  = errors.New("capture UUID has different contents")
	ErrSourcePayloadCorrupt = errors.New("source payload failed integrity verification")
)

type SourceEvidenceReaderWriter interface {
	PostIdentity(context.Context, string) (*SourcePostIdentity, error)
	PostIdentityMembers(context.Context, string, string, int) ([]SourcePostIdentity, error)
	PostConsolidationHistory(context.Context, string, int, int) ([]SourcePostConsolidation, error)
	ComparePosts(context.Context, string, string) (*SourcePostComparison, error)
	// EnsurePost preserves a supplied portable UUID when creating a post. An
	// existing identifier with a different supplied UUID needs explicit review.
	EnsurePost(context.Context, SourcePostIdentifier, string) (*SourcePost, error)
	FindPost(context.Context, string) (*SourcePost, error)
	FindPostByIdentifier(context.Context, SourcePostIdentifier) (*SourcePost, error)
	BrowsePosts(context.Context, SourcePostFilter) ([]SourcePostSummary, error)
	PostSummary(context.Context, string) (*SourcePostSummary, error)
	AddPostIdentifier(context.Context, string, SourcePostIdentifier, int) error
	PostIdentifiers(context.Context, string, *SourcePostIdentifier, int) ([]SourcePostIdentifier, error)
	CurrentPostIdentifiers(context.Context, string, *SourcePostIdentifier, int) ([]SourcePostIdentifier, error)
	RecordCapture(context.Context, SourceCaptureInput) (*SourceCapture, error)
	CaptureContexts(context.Context, string) ([]SourceCaptureContext, error)
	RetainProfile(context.Context, string, json.RawMessage) (*SourceProfileBody, error)
	FindCapture(context.Context, string) (*SourceCapture, error)
	Captures(context.Context, string, *SourceCaptureCursor, int) ([]*SourceCapture, error)
	CurrentCaptures(context.Context, string, *SourceCaptureCursor, int) ([]*SourceCapture, error)
}
