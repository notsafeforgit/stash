package models

import (
	"errors"
	"time"
)

// Account review identifies the owner of a service account. It never assigns
// depicted performers to media, and accounts on different services stay separate.
type AccountReviewFilter struct {
	After     string
	Query     string
	Namespace string
	Ownership AccountOwnershipState
	Limit     int
}

type AccountReviewPerformer struct {
	UUID           string             `json:"uuid"`
	Revision       int                `json:"revision"`
	State          ArchiveEntityState `json:"state"`
	LocalID        *int               `json:"local_id,omitempty"`
	Name           string             `json:"name"`
	Disambiguation string             `json:"disambiguation,omitempty"`
}

type AccountReviewOwnership struct {
	DecisionUUID  string                  `json:"decision_uuid"`
	Revision      int                     `json:"revision"`
	State         AccountOwnershipState   `json:"state"`
	PerformerUUID *string                 `json:"performer_uuid,omitempty"`
	Performer     *AccountReviewPerformer `json:"performer,omitempty"`
	Origin        string                  `json:"origin"`
	Reason        string                  `json:"reason"`
	CreatedAt     time.Time               `json:"created_at"`
}

type AccountReviewIdentifier struct {
	UUID        string           `json:"uuid"`
	AccountUUID string           `json:"account_uuid"`
	Reference   AccountReference `json:"reference"`
}

type AccountReviewState struct {
	UUID            string                    `json:"uuid"`
	Namespace       string                    `json:"namespace"`
	Label           string                    `json:"label"`
	Revision        int                       `json:"revision"`
	CanonicalUUID   string                    `json:"canonical_uuid"`
	RedirectTo      *string                   `json:"redirect_to,omitempty"`
	Ownership       *AccountReviewOwnership   `json:"ownership,omitempty"`
	Identifiers     []AccountReviewIdentifier `json:"identifiers"`
	MoreIdentifiers bool                      `json:"more_identifiers"`
}

type AccountOwnershipReviewInput struct {
	AccountUUID       string                `json:"account_uuid"`
	AccountRevision   int                   `json:"account_revision"`
	State             AccountOwnershipState `json:"state"`
	PerformerUUID     string                `json:"performer_uuid,omitempty"`
	PerformerRevision int                   `json:"performer_revision,omitempty"`
	Reason            string                `json:"reason,omitempty"`
}

type AccountOwnershipPreview struct {
	Input     AccountOwnershipReviewInput `json:"input"`
	Account   AccountReviewState          `json:"account"`
	Performer *AccountReviewPerformer     `json:"performer,omitempty"`
	Digest    string                      `json:"digest"`
}

type AccountOwnershipReviewApplyInput struct {
	AccountOwnershipReviewInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
}

// The receipt retains the original request after later links, account
// consolidation, performer UUID adoption, merges and deletion.
type AccountOwnershipReview struct {
	RequestUUID  string                           `json:"request_uuid"`
	DecisionUUID string                           `json:"decision_uuid"`
	Request      AccountOwnershipReviewApplyInput `json:"request"`
	CreatedAt    time.Time                        `json:"created_at"`
}

var (
	ErrAccountReviewInvalid = errors.New("invalid account ownership review")
	ErrAccountReviewReplay  = errors.New("account ownership review UUID has different contents")
)
