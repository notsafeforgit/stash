package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type IngestProducer struct {
	UUID      string    `json:"uuid"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// A credential grants one collection at its permitted logical root. Moving a
// collection to another root requires a new grant. A nil root permits metadata
// for an unbound collection only; it is never a filesystem wildcard.
type IngestScope struct {
	CollectionUUID string  `json:"collection_uuid"`
	RootUUID       *string `json:"root_uuid"`
}

type IngestCredential struct {
	UUID         string        `json:"uuid"`
	ProducerUUID string        `json:"producer_uuid"`
	SecretHash   string        `json:"-"`
	Scopes       []IngestScope `json:"scopes"`
	ExpiresAt    *time.Time    `json:"expires_at"`
	Revoked      bool          `json:"revoked"`
	CreatedAt    time.Time     `json:"created_at"`
}

// Receipts retain the exact submitted event digest and the original result.
// Result is a small server-generated projection, never the raw source payload.
type IngestReceipt struct {
	ProducerUUID       string          `json:"producer_uuid"`
	EventUUID          string          `json:"event_uuid"`
	Digest             string          `json:"sha256"`
	CredentialUUID     string          `json:"credential_uuid"`
	CollectionUUID     string          `json:"collection_uuid"`
	CollectionRevision int             `json:"collection_revision"`
	RootUUID           *string         `json:"root_uuid"`
	RunUUID            string          `json:"run_uuid"`
	Kind               string          `json:"kind"`
	PostUUID           string          `json:"post_uuid,omitempty"`
	CaptureUUID        string          `json:"capture_uuid,omitempty"`
	JobUUID            string          `json:"job_uuid,omitempty"`
	Result             json.RawMessage `json:"result"`
	CommittedAt        time.Time       `json:"committed_at"`
}

var ErrIngestReplay = errors.New("producer event identity has different contents")

type IngestReaderWriter interface {
	CreateProducer(context.Context, string) (*IngestProducer, error)
	FindProducer(context.Context, string) (*IngestProducer, error)
	Producers(context.Context, string, int) ([]IngestProducer, error)
	IssueCredential(context.Context, IngestCredential) (*IngestCredential, error)
	FindCredential(context.Context, string) (*IngestCredential, error)
	Credentials(context.Context, string, string, int) ([]IngestCredential, error)
	RevokeCredential(context.Context, string) error
	FindReceipt(context.Context, string, string) (*IngestReceipt, error)
	RecordReceipt(context.Context, IngestReceipt) (*IngestReceipt, error)
}
