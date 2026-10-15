package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Results share exact original/output text and provider/language facts across
// posts. Unknown values remain nil; they do not imply English or a provider.
type SourceTranslationInput struct {
	OriginalText   *string `json:"original_text" db:"original_text"`
	TranslatedText string  `json:"translated_text" db:"translated_text"`
	SourceLanguage *string `json:"source_language" db:"source_language"`
	TargetLanguage *string `json:"target_language" db:"target_language"`
	Provider       *string `json:"provider" db:"provider"`
}

type SourceTranslation struct {
	SourceTranslationInput
	UUID           string  `json:"uuid" db:"uuid"`
	OriginalSHA256 *string `json:"original_sha256" db:"original_sha256"`
}

// DeclaredInputHash is historical/provider evidence, not a verified content
// address. Exact original UTF-8 bytes have a separate hash on the result.
type SourceTranslationEvidence struct {
	UUID               string          `json:"uuid" db:"uuid"`
	TranslationUUID    string          `json:"translation_uuid" db:"translation_uuid"`
	PostUUID           string          `json:"post_uuid" db:"post_uuid"`
	CollectionUUID     *string         `json:"collection_uuid,omitempty" db:"collection_uuid"`
	CollectionRevision *int            `json:"collection_revision,omitempty" db:"collection_revision"`
	Provenance         string          `json:"provenance" db:"provenance"`
	DeclaredInputHash  *string         `json:"declared_input_hash" db:"declared_input_hash"`
	InputHashAlgorithm string          `json:"input_hash_algorithm" db:"input_hash_algorithm"`
	CapturedAt         string          `json:"captured_at" db:"captured_at"`
	Origin             string          `json:"origin" db:"origin"`
	Details            json.RawMessage `json:"details" db:"details"`
	RecordedAt         time.Time       `json:"recorded_at" db:"recorded_at"`
}

type SourceTranslationQuery struct {
	PostUUID       string
	OriginalSHA256 string
	TargetLanguage *string
	After          string
	Limit          int
}

type SourcePostDisplay struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Language    string `json:"language,omitempty"`
	Translated  bool   `json:"translated"`
}

type TranslationBackfillInput struct {
	Posts          []string `json:"posts"`
	TargetLanguage string   `json:"target_language"`
	ProviderPolicy string   `json:"provider_policy"`
	Priority       int      `json:"priority"`
	ApplyToLibrary bool     `json:"apply_to_library"`
	// An explicit repair may adopt only fields equal to retained source text or
	// its generated heading. It cannot adopt clears or a different manual value.
	AdoptMatchingText bool `json:"adopt_matching_text"`
}

type TranslationBackfillResult struct {
	PostUUID   string `json:"post_uuid"`
	Status     string `json:"status"`
	TargetUUID string `json:"target_uuid,omitempty"`
}

var (
	ErrSourceTranslationInvalid = errors.New("invalid retained source translation")
	ErrSourceTranslationReplay  = errors.New("translation evidence UUID has different contents")
	ErrSourceTranslationAtomic  = errors.New("translation write did not finish atomically")
)

type SourceTranslationReaderWriter interface {
	Retain(context.Context, SourceTranslationInput) (*SourceTranslation, error)
	Find(context.Context, string) (*SourceTranslation, error)
	RecordEvidence(context.Context, SourceTranslationEvidence) (*SourceTranslationEvidence, error)
	Evidence(context.Context, string) (*SourceTranslationEvidence, error)
	PostEvidence(context.Context, SourceTranslationQuery) ([]SourceTranslationEvidence, error)
	Display(context.Context, string, string, SourcePostMetadata) (*SourcePostDisplay, error)
	Backfill(context.Context, TranslationBackfillInput, time.Time) ([]TranslationBackfillResult, error)
	RefreshPending(context.Context, int) (int, error)
}
