package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Documents retain source bytes and the parser's interpretation separately.
// They are evidence, not playable library files or selected entity metadata.
type SourceDocumentInput struct {
	Content     []byte
	Encoding    string
	Parser      string
	ParseStatus string
	Warnings    json.RawMessage
	Parsed      json.RawMessage
}

type SourceDocument struct {
	UUID          string          `json:"uuid" db:"uuid"`
	ContentSHA256 string          `json:"content_sha256" db:"content_sha256"`
	ByteSize      int64           `json:"byte_size" db:"byte_size"`
	Encoding      string          `json:"encoding" db:"encoding"`
	Parser        string          `json:"parser" db:"parser"`
	ParseStatus   string          `json:"parse_status" db:"parse_status"`
	Warnings      json.RawMessage `json:"warnings" db:"warnings"`
	Parsed        json.RawMessage `json:"parsed" db:"parsed"`
}

// RelativePath is the source's literal historical pathname. Reading this
// evidence never opens that path, resolves a mount, or registers a media file.
// CapturedAt retains the original timestamp spelling; empty means unknown.
type SourceDocumentSource struct {
	UUID               string          `json:"uuid" db:"uuid"`
	DocumentUUID       string          `json:"document_uuid" db:"document_uuid"`
	CollectionUUID     string          `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int             `json:"collection_revision" db:"collection_revision"`
	RelativePath       string          `json:"relative_path" db:"relative_path"`
	PostUUID           *string         `json:"post_uuid,omitempty" db:"post_uuid"`
	CapturedAt         string          `json:"captured_at" db:"captured_at"`
	Origin             string          `json:"origin" db:"origin"`
	Details            json.RawMessage `json:"details" db:"details"`
	RecordedAt         time.Time       `json:"recorded_at" db:"recorded_at"`
}

// A claim preserves which source document a writer selected, even if the
// current native choice differs. Import cannot replace an explicit unlink.
type SourceDocumentHeadClaim struct {
	UUID           string          `json:"uuid" db:"uuid"`
	SourceUUID     string          `json:"source_uuid" db:"source_uuid"`
	CollectionUUID string          `json:"collection_uuid" db:"collection_uuid"`
	RelativePath   string          `json:"relative_path" db:"relative_path"`
	ObservedAt     string          `json:"observed_at" db:"observed_at"`
	Origin         string          `json:"origin" db:"origin"`
	Details        json.RawMessage `json:"details" db:"details"`
	RecordedAt     time.Time       `json:"recorded_at" db:"recorded_at"`
}

type SourceDocumentHead struct {
	UUID           string    `json:"uuid" db:"uuid"`
	CollectionUUID string    `json:"collection_uuid" db:"collection_uuid"`
	RelativePath   string    `json:"relative_path" db:"relative_path"`
	Revision       int       `json:"revision" db:"revision"`
	State          string    `json:"state" db:"state"`
	SourceUUID     *string   `json:"source_uuid,omitempty" db:"source_uuid"`
	ClaimUUID      *string   `json:"claim_uuid,omitempty" db:"claim_uuid"`
	Origin         string    `json:"origin" db:"origin"`
	Reason         string    `json:"reason" db:"reason"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type SourceDocumentHeadInput struct {
	CollectionUUID   string
	RelativePath     string
	ExpectedRevision int
	State            string
	SourceUUID       string
	ClaimUUID        string
	Origin           string
	Reason           string
}

var (
	ErrSourceDocumentInvalid  = errors.New("invalid retained source document")
	ErrSourceDocumentReplay   = errors.New("source document evidence UUID has different contents")
	ErrSourceDocumentConflict = errors.New("source document choice changed")
)

type SourceDocumentReaderWriter interface {
	Retain(context.Context, SourceDocumentInput) (*SourceDocument, error)
	Find(context.Context, string) (*SourceDocument, error)
	Content(context.Context, string) ([]byte, error)
	RecordSource(context.Context, SourceDocumentSource) (*SourceDocumentSource, error)
	Source(context.Context, string) (*SourceDocumentSource, error)
	PostSources(context.Context, string, string, int) ([]SourceDocumentSource, error)
	LocationSources(context.Context, string, string, string, int) ([]SourceDocumentSource, error)
	RecordHeadClaim(context.Context, SourceDocumentHeadClaim) (*SourceDocumentHeadClaim, error)
	HeadClaim(context.Context, string) (*SourceDocumentHeadClaim, error)
	HeadClaims(context.Context, string, string, string, int) ([]SourceDocumentHeadClaim, error)
	Head(context.Context, string, string) (*SourceDocumentHead, error)
	DecideHead(context.Context, SourceDocumentHeadInput) (*SourceDocumentHead, error)
	HeadHistory(context.Context, string, string, int, int) ([]SourceDocumentHead, error)
}
