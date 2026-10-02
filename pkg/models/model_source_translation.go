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
}
