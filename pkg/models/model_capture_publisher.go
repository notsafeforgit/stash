package models

import (
	"context"
	"errors"
	"time"
)

// CapturedAccount contains source identity claims, not a performer attribution.
type CapturedAccount struct {
	Policy      string                      `json:"policy"`
	Namespace   string                      `json:"namespace"`
	Label       string                      `json:"label"`
	Identifiers []CapturedAccountIdentifier `json:"identifiers"`
}

type CapturedAccountIdentifier struct {
	Reference AccountReference `json:"reference"`
	Basis     string           `json:"basis"`
	Path      string           `json:"path"`
}

type CapturePublisherDecision struct {
	UUID                 string
	CaptureUUID          string
	Revision             int
	State                string  // linked, unlinked, undecided
	AccountUUID          *string // original association, retained through account consolidation
	CanonicalAccountUUID *string // current resolution; not stored over the original
	Origin               string
	Policy               string
	Reason               string
	CreatedAt            time.Time
}

type CapturePublisherCandidate struct {
	Account SourceAccount
	Matches []AccountReference
}

type CapturePublisherPreview struct {
	CaptureUUID         string
	PostUUID            string
	PostState           string
	Current             *CapturePublisherDecision
	Identity            *CapturedAccount
	IdentityError       string
	Candidates          []CapturePublisherCandidate
	StableIDMatches     bool
	CandidatesTruncated bool
	Target              *SourceAccount // optional explicitly selected account, resolved canonically
	Action              string         // preserve, link, create, review, unavailable
	AccountUUID         *string        // automatic suggestion, when Action is link
	Conflicts           []string
	Signature           string
}

type CapturePublisherInput struct {
	UUID              string // required stable request identity
	CaptureUUID       string
	ExpectedSignature string
	Action            string // automatic, link, create, unlink, inherit
	AccountUUID       string // required only for an explicit link
	Origin            string // review or migration for explicit choices; automatic uses capture
	Reason            string
}

var (
	ErrCapturePublisherConflict = errors.New("capture publisher evidence or review changed")
	ErrCapturePublisherReplay   = errors.New("publisher decision UUID has different input")
)

type CapturePublisherReaderWriter interface {
	Preview(context.Context, string, string) (*CapturePublisherPreview, error)
	Apply(context.Context, CapturePublisherInput) (*CapturePublisherDecision, error)
	Current(context.Context, string) (*CapturePublisherDecision, error)
	History(context.Context, string, int, int) ([]CapturePublisherDecision, error)
	PostAccounts(context.Context, string, string, int) ([]*SourceAccount, error)
}
