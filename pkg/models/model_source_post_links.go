package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// SourcePostEvidence retains why a URL, identifier or account was associated
// with a post. ObservedAt is the evidence time, not an invented publication date.
type SourcePostEvidence struct {
	UUID       string          `json:"uuid"`
	PostUUID   string          `json:"post_uuid"`
	Origin     string          `json:"origin"`
	Basis      string          `json:"basis"`
	ObservedAt time.Time       `json:"observed_at"`
	Details    json.RawMessage `json:"details"`
}

type SourcePostURL struct {
	UUID     string `json:"uuid"`
	PostUUID string `json:"post_uuid"`
	URL      string `json:"url"`
}

type SourcePostURLInput struct {
	SourcePostEvidence
	URL string `json:"url"`
}

type SourcePostURLObservation struct {
	SourcePostEvidence
	URLUUID string `json:"url_uuid"`
	URL     string `json:"url"`
}

type SourcePostIdentifierInput struct {
	SourcePostEvidence
	Identifier           SourcePostIdentifier `json:"identifier"`
	ExpectedPostRevision int                  `json:"expected_post_revision"`
}

type SourcePostIdentifierObservation struct {
	SourcePostEvidence
	Identifier SourcePostIdentifier `json:"identifier"`
}

// A retained publisher claim does not select a capture's publisher and never
// assigns depicted performers. Original account UUIDs survive consolidation.
type SourcePostAccountClaimInput struct {
	SourcePostEvidence
	AccountUUID string `json:"account_uuid"`
}

type SourcePostAccountClaim struct {
	SourcePostAccountClaimInput
	CanonicalAccountUUID string `json:"canonical_account_uuid"`
}

var (
	ErrSourcePostEvidenceReplay  = errors.New("source post evidence UUID has different contents")
	ErrSourcePostEvidenceInvalid = errors.New("invalid source post link evidence")
)

type SourcePostLinksReaderWriter interface {
	ObserveURL(context.Context, SourcePostURLInput) (*SourcePostURLObservation, error)
	URLs(context.Context, string, string, int) ([]SourcePostURL, error)
	URLEvidence(context.Context, string, string, int) ([]SourcePostURLObservation, error)
	ObserveIdentifier(context.Context, SourcePostIdentifierInput) (*SourcePostIdentifierObservation, error)
	IdentifierEvidence(context.Context, string, string, int) ([]SourcePostIdentifierObservation, error)
	ClaimAccount(context.Context, SourcePostAccountClaimInput) (*SourcePostAccountClaim, error)
	AccountClaims(context.Context, string, string, int) ([]SourcePostAccountClaim, error)
}
