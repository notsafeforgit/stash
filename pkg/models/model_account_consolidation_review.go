package models

import (
	"errors"
	"time"
)

// These application DTOs are separate from AccountConsolidationInput: its
// original JSON encoding is retained in immutable request digests.
type AccountConsolidationChoice struct {
	State             AccountOwnershipState `json:"state"`
	PerformerUUID     string                `json:"performer_uuid,omitempty"`
	PerformerRevision int                   `json:"performer_revision,omitempty"`
}

type AccountConsolidationReviewInput struct {
	SourceUUID                string                      `json:"source_uuid"`
	DestinationUUID           string                      `json:"destination_uuid"`
	OwnershipMode             string                      `json:"ownership_mode"`
	Ownership                 *AccountConsolidationChoice `json:"ownership,omitempty"`
	AcceptIdentifierConflicts bool                        `json:"accept_identifier_conflicts"`
	Reason                    string                      `json:"reason,omitempty"`
}

type AccountConsolidationConflict struct {
	Namespace string   `json:"namespace"`
	Kind      string   `json:"kind"`
	Values    []string `json:"values"`
}

type AccountConsolidationReviewPreview struct {
	Input               AccountConsolidationReviewInput `json:"input"`
	Source              AccountReviewState              `json:"source"`
	Destination         AccountReviewState              `json:"destination"`
	MemberCount         int                             `json:"member_count"`
	IdentifierCount     int                             `json:"identifier_count"`
	IdentifierConflicts []AccountConsolidationConflict  `json:"identifier_conflicts"`
	Ownership           *AccountConsolidationChoice     `json:"ownership,omitempty"`
	Performer           *AccountReviewPerformer         `json:"performer,omitempty"`
	Blockers            []string                        `json:"blockers"`
	Ready               bool                            `json:"ready"`
	Digest              string                          `json:"digest"`
}

type AccountConsolidationReviewApplyInput struct {
	AccountConsolidationReviewInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
}

type AccountConsolidationRecord struct {
	UUID                        string    `json:"uuid"`
	Sequence                    int       `json:"sequence"`
	SourceUUID                  string    `json:"source_uuid"`
	DestinationUUID             string    `json:"destination_uuid"`
	SourceRevision              int       `json:"source_revision"`
	DestinationRevision         int       `json:"destination_revision"`
	OwnershipDecisionUUID       string    `json:"ownership_decision_uuid"`
	Signature                   string    `json:"signature"`
	Origin                      string    `json:"origin"`
	Reason                      string    `json:"reason"`
	AcceptedIdentifierConflicts bool      `json:"accepted_identifier_conflicts"`
	CreatedAt                   time.Time `json:"created_at"`
}

// The original request is echoed only after its full digest matches the saved
// event. Receipt checking is read-only, including after subsequent account or
// performer merges; it does not infer success from current ownership.
type AccountConsolidationReview struct {
	Request       AccountConsolidationReviewApplyInput `json:"request"`
	Consolidation AccountConsolidationRecord           `json:"consolidation"`
}

var ErrAccountConsolidationReviewInvalid = errors.New("invalid account consolidation review")

func (c AccountConsolidation) ReviewRecord() AccountConsolidationRecord {
	return AccountConsolidationRecord(c)
}
