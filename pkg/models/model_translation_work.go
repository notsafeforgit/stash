package models

import (
	"context"
	"errors"
	"time"
)

// Policy versions identify provider/preprocessing behavior. Changing the
// behavior requires a new policy identity, not overwriting an existing cache.
const TranslationBingTextV1 = "translate-shell-bing-text-v1"

type TranslationRequestInput struct {
	OriginalText   string `json:"original_text" db:"original_text"`
	TargetLanguage string `json:"target_language" db:"target_language"`
	Policy         string `json:"policy" db:"policy"`
}

type TranslationRequest struct {
	TranslationRequestInput
	UUID           string    `json:"uuid" db:"uuid"`
	OriginalSHA256 string    `json:"original_sha256" db:"original_sha256"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type TranslationCacheInput struct {
	RequestUUID    string  `json:"request_uuid"`
	Status         string  `json:"status"` // translated, unchanged, no_text
	TranslatedText *string `json:"translated_text"`
	SourceLanguage *string `json:"source_language"`
	Provider       *string `json:"provider"`
	CapturedAt     string  `json:"captured_at"`
	Origin         string  `json:"origin"`
}

// Cached English detections and no-text outcomes are useful completed work.
// They remain distinct from a newly translated result.
type TranslationCache struct {
	UUID            string    `json:"uuid" db:"uuid"`
	RequestUUID     string    `json:"request_uuid" db:"request_uuid"`
	Status          string    `json:"status" db:"status"`
	TranslationUUID *string   `json:"translation_uuid" db:"translation_uuid"`
	CapturedAt      string    `json:"captured_at" db:"captured_at"`
	Origin          string    `json:"origin" db:"origin"`
	RecordedAt      time.Time `json:"recorded_at" db:"recorded_at"`
}

type TranslationTargetInput struct {
	RequestUUID        string  `json:"request_uuid" db:"request_uuid"`
	PostUUID           string  `json:"post_uuid" db:"post_uuid"`
	CollectionUUID     *string `json:"collection_uuid,omitempty" db:"collection_uuid"`
	CollectionRevision *int    `json:"collection_revision,omitempty" db:"collection_revision"`
	Field              string  `json:"field" db:"field"` // title or caption
	Origin             string  `json:"origin" db:"origin"`
}

// Targets are durable domain work even before an execution job is admitted.
// Held targets never become runnable merely because their request has a cache.
type TranslationTarget struct {
	TranslationTargetInput
	UUID         string    `json:"uuid" db:"uuid"`
	Revision     int       `json:"revision" db:"revision"`
	State        string    `json:"state" db:"state"` // held, pending, completed, review
	Priority     int       `json:"priority" db:"priority"`
	NotBefore    time.Time `json:"not_before" db:"not_before"`
	CacheUUID    *string   `json:"cache_uuid,omitempty" db:"cache_uuid"`
	EvidenceUUID *string   `json:"evidence_uuid,omitempty" db:"evidence_uuid"`
	Reason       string    `json:"reason" db:"reason"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

type TranslationTargetSchedule struct {
	State     string    `json:"state"` // held or pending
	Priority  int       `json:"priority"`
	NotBefore time.Time `json:"not_before"`
}

type TranslationTargetQuery struct {
	RequestUUID string
	PostUUID    string
	State       string
	After       string
	Limit       int
}

type TranslationTargetHistory struct {
	TargetUUID   string    `json:"target_uuid" db:"target_uuid"`
	Revision     int       `json:"revision" db:"revision"`
	State        string    `json:"state" db:"state"`
	Priority     int       `json:"priority" db:"priority"`
	NotBefore    time.Time `json:"not_before" db:"not_before"`
	CacheUUID    *string   `json:"cache_uuid,omitempty" db:"cache_uuid"`
	EvidenceUUID *string   `json:"evidence_uuid,omitempty" db:"evidence_uuid"`
	Reason       string    `json:"reason" db:"reason"`
	RecordedAt   time.Time `json:"recorded_at" db:"recorded_at"`
}

// A job owns a bounded, immutable set of target revisions. Changing a target
// cannot widen or redirect a previously admitted execution.
type TranslationTargetRef struct {
	TargetUUID string `json:"target_uuid" db:"target_uuid"`
	Revision   int    `json:"revision" db:"target_revision"`
}

type TranslationJobArguments struct {
	Version     int                    `json:"version"`
	RequestUUID string                 `json:"request_uuid"`
	Targets     []TranslationTargetRef `json:"targets"`
}

type TranslationJobTarget struct {
	TranslationTargetRef
	JobUUID string `json:"job_uuid" db:"job_uuid"`
}

var (
	ErrTranslationWorkInvalid  = errors.New("invalid native translation work")
	ErrTranslationWorkConflict = errors.New("native translation work changed")
	ErrTranslationWorkAtomic   = errors.New("native translation work did not finish atomically")
)

type TranslationWorkReaderWriter interface {
	RetainRequest(context.Context, TranslationRequestInput) (*TranslationRequest, error)
	Request(context.Context, string) (*TranslationRequest, error)
	RetainCache(context.Context, TranslationCacheInput) (*TranslationCache, error)
	Cache(context.Context, string) (*TranslationCache, error) // request UUID
	RetainTarget(context.Context, TranslationTargetInput, TranslationTargetSchedule, time.Time) (*TranslationTarget, error)
	Target(context.Context, string) (*TranslationTarget, error)
	Targets(context.Context, TranslationTargetQuery) ([]TranslationTarget, error)
	TargetHistory(context.Context, string, int, int) ([]TranslationTargetHistory, error)
	ScheduleTarget(context.Context, string, int, TranslationTargetSchedule, time.Time) (*TranslationTarget, error)
	PublishTarget(context.Context, string, int, time.Time) (*TranslationTarget, error)
	ReadyTargets(context.Context, string, time.Time, int) ([]TranslationTarget, error)
	BindJob(context.Context, string, time.Time) error
	JobTargets(context.Context, string) ([]TranslationJobTarget, error)
	TargetBinding(context.Context, string, int) (*TranslationJobTarget, error)
	RetryTarget(context.Context, string, int, time.Time) (*TranslationTarget, error)
	PreviewActivation(context.Context, TranslationActivationInput) (*TranslationActivationPlan, error)
	Activate(context.Context, TranslationActivationInput, string, time.Time) (*TranslationActivation, error)
	Activation(context.Context, string) (*TranslationActivation, error)
}
