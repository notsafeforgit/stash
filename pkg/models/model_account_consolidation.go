package models

import (
	"errors"
	"time"
)

type AccountOwnershipSelection struct {
	State             AccountOwnershipState
	PerformerUUID     string
	PerformerRevision int
}

type AccountIdentifierConflict struct {
	Namespace string
	Kind      string
	Values    []string
}

// Consolidation joins duplicate records for one service account. Accounts on
// different services remain separate even when they share a performer owner.
type AccountConsolidationPreview struct {
	Source               *SourceAccount
	Destination          *SourceAccount
	Members              []*SourceAccount
	Identifiers          []*AccountIdentifier
	SourceOwnership      *AccountOwnershipDecision
	DestinationOwnership *AccountOwnershipDecision
	Performers           []*ArchiveEntity
	DefaultOwnership     *AccountOwnershipSelection
	IdentifierConflicts  []AccountIdentifierConflict
	Signature            string
}

type AccountConsolidationInput struct {
	// UUID is an optional idempotency key. Reusing it with changed input fails.
	UUID                      string
	SourceUUID                string
	DestinationUUID           string
	Signature                 string
	OwnershipMode             string // preserve compatible choices, or choose explicitly
	Ownership                 AccountOwnershipSelection
	AcceptIdentifierConflicts bool
	Origin                    string // review or migration
	Reason                    string
}

type AccountConsolidation struct {
	UUID                        string
	Sequence                    int
	SourceUUID                  string
	DestinationUUID             string
	SourceRevision              int
	DestinationRevision         int
	OwnershipDecisionUUID       string
	Signature                   string
	Origin                      string
	Reason                      string
	AcceptedIdentifierConflicts bool
	CreatedAt                   time.Time
}

var (
	ErrAccountOwnershipResolution  = errors.New("account ownership choices conflict; choose the resulting ownership explicitly")
	ErrAccountIdentifierResolution = errors.New("account identifiers conflict; review and acknowledge the retained evidence")
	ErrAccountConsolidationReplay  = errors.New("account consolidation UUID has different input")
)
