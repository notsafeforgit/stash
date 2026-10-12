package models

import (
	"errors"
	"time"
)

// PerformerSourceAccounts reports current account ownership for a canonical
// performer. It does not infer ownership from names or depicted media.
type PerformerSourceAccounts struct {
	RequestedUUID string                 `json:"requested_uuid"`
	Performer     AccountReviewPerformer `json:"performer"`
	Accounts      []AccountReviewState   `json:"accounts"`
}

// PerformerSourceIdentity retains stable identities and their merge redirects.
type PerformerSourceIdentity struct {
	UUID       string             `json:"uuid"`
	Revision   int                `json:"revision"`
	State      ArchiveEntityState `json:"state"`
	RedirectTo *string            `json:"redirect_to"`
	CreatedAt  time.Time          `json:"created_at"`
	RetiredAt  *time.Time         `json:"retired_at"`
}

type PerformerSourceIdentities struct {
	RequestedUUID string                    `json:"requested_uuid"`
	Performer     AccountReviewPerformer    `json:"performer"`
	Identities    []PerformerSourceIdentity `json:"identities"`
}

var ErrPerformerSourceLimit = errors.New("performer identity group exceeds the review limit")
