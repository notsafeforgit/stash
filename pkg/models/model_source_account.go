package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// AccountReference identifies a service account, not a person. Native service
// IDs, handles and mirror identifiers have separate qualified namespaces.
type AccountReference struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
}

type SourceAccount struct {
	UUID          string
	Namespace     string
	Label         string
	Revision      int
	CreatedAt     time.Time
	RedirectTo    *string
	CanonicalUUID string
}

type AccountIdentifier struct {
	UUID                 string
	AccountUUID          string
	CanonicalAccountUUID string
	Reference            AccountReference
}

// AccountIdentifierEvidence records why an identifier is associated with an
// account. Key is stable within an identifier; reusing it with other evidence
// is an error. Repeated observations may expand its time interval.
type AccountIdentifierEvidence struct {
	Key           string
	Basis         string
	Origin        string
	Details       json.RawMessage
	FirstObserved time.Time
	LastObserved  time.Time
}

type AccountOwnershipState string

const (
	AccountOwnershipLinked    AccountOwnershipState = "linked"
	AccountOwnershipUnlinked  AccountOwnershipState = "unlinked"
	AccountOwnershipUndecided AccountOwnershipState = "undecided"
)

// AccountOwnershipDecision is an append-only choice. A null performer on an
// explicit unlink is different from returning an account to undecided review.
// PerformerUUID keeps the chosen identity, including a later merge redirect.
type AccountOwnershipDecision struct {
	UUID          string
	AccountUUID   string
	Revision      int
	State         AccountOwnershipState
	PerformerUUID *string
	Origin        string
	Reason        string
	CreatedAt     time.Time
}

type AccountOwnershipInput struct {
	AccountUUID               string
	ExpectedAccountRevision   int
	State                     AccountOwnershipState
	PerformerUUID             string
	ExpectedPerformerRevision int
	Origin                    string
	Reason                    string
}

var (
	ErrSourceAccountConflict   = errors.New("source account or reviewed performer changed")
	ErrAccountEvidenceConflict = errors.New("account identifier evidence key has different contents")
)

type SourceAccountReaderWriter interface {
	Create(context.Context, string, string) (*SourceAccount, error)
	Find(context.Context, string) (*SourceAccount, error)
	Resolve(context.Context, string) (*SourceAccount, error)
	// Lookup is an indexed, bounded candidate query. Identifiers are not
	// globally unique: a reused handle or disputed ID remains reviewable.
	Lookup(context.Context, AccountReference, string, int) ([]*SourceAccount, error)
	ObserveIdentifier(context.Context, string, AccountReference, AccountIdentifierEvidence) (*AccountIdentifier, error)
	Identifiers(context.Context, string, string, int) ([]*AccountIdentifier, error)
	Evidence(context.Context, string, string, int) ([]AccountIdentifierEvidence, error)
	Ownership(context.Context, string) (*AccountOwnershipDecision, error)
	OwnershipHistory(context.Context, string, int, int) ([]*AccountOwnershipDecision, error)
	DecideOwnership(context.Context, AccountOwnershipInput) (*AccountOwnershipDecision, error)
	ReviewAccounts(context.Context, AccountReviewFilter) ([]AccountReviewState, error)
	ReviewAccount(context.Context, string) (*AccountReviewState, error)
	ReviewOwnershipHistory(context.Context, string, int, int) ([]AccountReviewOwnership, error)
	PreviewOwnership(context.Context, AccountOwnershipReviewInput) (*AccountOwnershipPreview, error)
	ApplyOwnershipReview(context.Context, AccountOwnershipReviewApplyInput) (*AccountOwnershipReview, bool, error)
	OwnershipReview(context.Context, string) (*AccountOwnershipReview, error)
	PreviewConsolidation(context.Context, string, string) (*AccountConsolidationPreview, error)
	Consolidate(context.Context, AccountConsolidationInput) (*AccountConsolidation, error)
	ConsolidationHistory(context.Context, string, int, int) ([]AccountConsolidation, error)
}
