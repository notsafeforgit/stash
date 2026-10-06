package models

import (
	"errors"
	"time"
)

// The original post UUID remains the owner of captures, identifiers, immutable
// choices and receipts. CanonicalUUID groups representations of one post; it
// never groups separate posts merely because they share media or a URL.
type SourcePostIdentity struct {
	UUID          string    `json:"uuid"`
	CanonicalUUID string    `json:"canonical_uuid"`
	RedirectTo    *string   `json:"redirect_to"`
	State         string    `json:"state"`
	Revision      int       `json:"revision"`
	CreatedAt     time.Time `json:"created_at"`
}

type SourcePostConsolidation struct {
	UUID                string    `json:"uuid"`
	Sequence            int       `json:"sequence"`
	SourceUUID          string    `json:"source_uuid"`
	DestinationUUID     string    `json:"destination_uuid"`
	SourceRevision      int       `json:"source_revision"`
	DestinationRevision int       `json:"destination_revision"`
	MemberCount         int       `json:"member_count"`
	IdentitySignature   string    `json:"identity_signature"`
	ReviewSignature     string    `json:"review_signature"`
	Origin              string    `json:"origin"`
	Reason              string    `json:"reason"`
	CreatedAt           time.Time `json:"created_at"`
}

var (
	ErrSourcePostIdentityInvalid     = errors.New("invalid source post identity request")
	ErrSourcePostIdentityConflict    = errors.New("source post identity or membership changed")
	ErrSourcePostIdentityLimit       = errors.New("source post identity exceeds its review limit")
	ErrSourcePostIdentifierConflict  = errors.New("distinct upstream post identifiers require separate posts")
	ErrSourcePostConsolidationReplay = errors.New("post consolidation request UUID has different contents")
)
