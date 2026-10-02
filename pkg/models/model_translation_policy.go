package models

import (
	"context"
	"errors"
	"time"
)

// Source translation is independent of selecting scene/image metadata.
type TranslationPolicyDefinition struct {
	Enabled        bool   `json:"enabled"`
	ProviderPolicy string `json:"provider_policy"`
	TargetLanguage string `json:"target_language"`
	Title          bool   `json:"title"`
	Caption        bool   `json:"caption"`
	Priority       int    `json:"priority"`
}

type TranslationPolicy struct {
	CollectionUUID     string                      `json:"collection_uuid"`
	Revision           int                         `json:"revision"`
	CollectionRevision int                         `json:"collection_revision"`
	Definition         TranslationPolicyDefinition `json:"definition"`
	Origin             string                      `json:"origin"`
	Reason             string                      `json:"reason"`
	CreatedAt          time.Time                   `json:"created_at"`
}

type TranslationPolicyInput struct {
	CollectionUUID             string                      `json:"collection_uuid"`
	ExpectedRevision           int                         `json:"expected_revision"`
	ExpectedCollectionRevision int                         `json:"expected_collection_revision"`
	Definition                 TranslationPolicyDefinition `json:"definition"`
	Origin                     string                      `json:"origin"`
	Reason                     string                      `json:"reason"`
}

type CaptureTranslationEntry struct {
	Field          string  `json:"field" db:"field"`
	Status         string  `json:"status" db:"status"` // created, retained, no_text
	TargetUUID     *string `json:"target_uuid,omitempty" db:"target_uuid"`
	TargetRevision *int    `json:"target_revision,omitempty" db:"target_revision"`
}

// An immutable decision records the policy at first acceptance. A duplicate
// capture cannot activate later policy choices or release an existing hold.
type CaptureTranslationDecision struct {
	CollectionCapture
	PolicyRevision *int                      `json:"policy_revision,omitempty"`
	Status         string                    `json:"status"` // no_policy, disabled, collection_changed, recorded
	Entries        []CaptureTranslationEntry `json:"entries"`
}

var (
	ErrTranslationPolicyInvalid  = errors.New("invalid source translation policy")
	ErrTranslationPolicyConflict = errors.New("source translation policy or collection changed")
)

type TranslationPolicyReaderWriter interface {
	Find(context.Context, string) (*TranslationPolicy, error)
	History(context.Context, string, int, int) ([]*TranslationPolicy, error)
	Put(context.Context, TranslationPolicyInput) (*TranslationPolicy, error)
	CaptureDecision(context.Context, CollectionCapture) (*CaptureTranslationDecision, error)
	ScheduleCapture(context.Context, CollectionCapture, time.Time) (*CaptureTranslationDecision, error)
}
