package models

import "time"

// Activation releases exact held revisions. It preserves their priorities and
// deadlines; admission and provider execution remain separate operations.
type TranslationActivationInput struct {
	UUID           string                 `json:"uuid"`
	SnapshotUUID   string                 `json:"snapshot_uuid,omitempty"`
	ManifestSHA256 string                 `json:"manifest_sha256,omitempty"`
	Targets        []TranslationTargetRef `json:"targets"`
}

type TranslationActivationEntry struct {
	TranslationTargetRef
	RequestUUID        string    `json:"request_uuid"`
	PostUUID           string    `json:"post_uuid"`
	CollectionUUID     *string   `json:"collection_uuid,omitempty"`
	CollectionRevision *int      `json:"collection_revision,omitempty"`
	Field              string    `json:"field"`
	Priority           int       `json:"priority"`
	NotBefore          time.Time `json:"not_before"`
}

type TranslationActivationPlan struct {
	Version    int                          `json:"version"`
	Input      TranslationActivationInput   `json:"input"`
	Entries    []TranslationActivationEntry `json:"entries"`
	PlanSHA256 string                       `json:"plan_sha256,omitempty"`
}

type TranslationActivation struct {
	TranslationActivationPlan
	Activated []TranslationTargetRef `json:"activated"`
	CreatedAt time.Time              `json:"created_at"`
}

type AutomationTranslationCandidate struct {
	Ordinal int64 `json:"ordinal" db:"ordinal"`
	TranslationTargetRef
	CurrentRevision    int       `json:"current_revision" db:"current_revision"`
	State              string    `json:"state" db:"state"`
	PostState          string    `json:"post_state" db:"post_state"`
	RequestUUID        string    `json:"request_uuid" db:"request_uuid"`
	PostUUID           string    `json:"post_uuid" db:"post_uuid"`
	CollectionUUID     *string   `json:"collection_uuid,omitempty" db:"collection_uuid"`
	CollectionRevision *int      `json:"collection_revision,omitempty" db:"collection_revision"`
	Field              string    `json:"field" db:"field"`
	Priority           int       `json:"priority" db:"priority"`
	NotBefore          time.Time `json:"not_before" db:"not_before"`
	Disposition        string    `json:"disposition" db:"-"`
}
