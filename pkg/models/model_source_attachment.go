package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type SourceAttachment struct {
	UUID      string
	PostUUID  string
	Reference SourcePostIdentifier
	Revision  int
}

// Positions are source positions, independent of filename or download order.
// A partial manifest may have gaps. Multiple positions may reuse an attachment.
type SourceAttachmentEntry struct {
	Position  int
	Reference SourcePostIdentifier
	MediaKind string // image, video, or unknown; a source hint, not a library type
}

type SourceAttachmentManifestInput struct {
	CaptureUUID   string
	Complete      bool
	DeclaredAlbum bool
	ExpectedCount *int
	Entries       []SourceAttachmentEntry
}

type SourceAttachmentManifest struct {
	UUID          string
	PostUUID      string
	Version       string
	Signature     string
	Complete      bool
	DeclaredAlbum bool
	ExpectedCount *int
	EntryCount    int
}

func (m SourceAttachmentManifest) IsAlbum() bool {
	return m.DeclaredAlbum || m.EntryCount > 1 || (m.ExpectedCount != nil && *m.ExpectedCount > 1)
}

type SourceAttachmentManifestEntry struct {
	Position   int
	MediaKind  string
	Attachment SourceAttachment
}

// Evidence identifies a candidate association. Recording it does not assign
// performers, choose a preferred media entity, or create library records.
type SourceMediaEvidence struct {
	UUID           string
	AttachmentUUID string
	CaptureUUID    string
	MediaUUID      string
	FileUUID       *string
	Basis          string // observed-file, verified-bytes, review, or legacy
	Details        json.RawMessage
	CreatedAt      time.Time
}

type AttachmentMediaDecisionInput struct {
	AttachmentUUID             string
	ExpectedAttachmentRevision int
	State                      string // linked, unlinked, or undecided
	MediaUUID                  string
	ExpectedMediaRevision      int
	Origin                     string // review, ingest, or migration
	Reason                     string
}

type AttachmentMediaDecision struct {
	UUID           string
	AttachmentUUID string
	Revision       int
	State          string
	MediaUUID      *string
	Origin         string
	Reason         string
	CreatedAt      time.Time
}

var (
	ErrSourceAttachmentConflict  = errors.New("source attachment or media review changed")
	ErrAttachmentManifestReplay  = errors.New("capture already has a different attachment manifest")
	ErrSourceMediaEvidenceReplay = errors.New("media evidence UUID has different contents")
	ErrAmbiguousSourceMedia      = errors.New("attachment media candidates require explicit review")
)

type SourceAttachmentReaderWriter interface {
	RecordManifest(context.Context, SourceAttachmentManifestInput) (*SourceAttachmentManifest, error)
	FindManifest(context.Context, string) (*SourceAttachmentManifest, error)
	ManifestForCapture(context.Context, string) (*SourceAttachmentManifest, error)
	ManifestEntries(context.Context, string, int, int) ([]SourceAttachmentManifestEntry, error)
	Find(context.Context, string) (*SourceAttachment, error)
	Lookup(context.Context, string, SourcePostIdentifier) (*SourceAttachment, error)
	RecordMediaEvidence(context.Context, SourceMediaEvidence) (*SourceMediaEvidence, error)
	MediaEvidence(context.Context, string, string, int) ([]SourceMediaEvidence, error)
	DecideMedia(context.Context, AttachmentMediaDecisionInput) (*AttachmentMediaDecision, error)
	MediaDecision(context.Context, string) (*AttachmentMediaDecision, error)
	MediaDecisionHistory(context.Context, string, int, int) ([]AttachmentMediaDecision, error)
}
