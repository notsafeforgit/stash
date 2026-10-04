package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// History describes an observed source assertion, not a request to touch files
// or replace the library's selected metadata. SourceTime retains the original
// timestamp; ObservedAt dates receipt of the source assertion.
type SourceFileHistory struct {
	UUID               string                      `json:"uuid" db:"uuid"`
	Kind               string                      `json:"kind" db:"kind"`
	CollectionUUID     string                      `json:"collection_uuid" db:"collection_uuid"`
	CollectionRevision int                         `json:"collection_revision" db:"collection_revision"`
	RootUUID           string                      `json:"root_uuid" db:"root_uuid"`
	RootRevision       int                         `json:"root_revision" db:"root_revision"`
	ReferenceNamespace string                      `json:"reference_namespace" db:"reference_namespace"`
	ReferenceValue     string                      `json:"reference_value" db:"reference_value"`
	SourceTime         string                      `json:"source_time" db:"source_time"`
	Origin             string                      `json:"origin" db:"origin"`
	ObservedAt         time.Time                   `json:"observed_at" db:"observed_at"`
	Details            json.RawMessage             `json:"details" db:"details"`
	CreatedAt          time.Time                   `json:"created_at" db:"created_at"`
	Locations          []SourceFileHistoryLocation `json:"locations" db:"-"`
	Edits              []SourceFileEdit            `json:"edits,omitempty" db:"-"`
	StateChange        *SourceFileStateChange      `json:"state_change,omitempty" db:"-"`
	Deduplication      *SourceFileDeduplication    `json:"deduplication,omitempty" db:"-"`
}

type SourceFileHistoryLocation struct {
	Position        int     `json:"position" db:"position"`
	RelativePath    string  `json:"relative_path" db:"relative_path"`
	ArchivePath     *string `json:"archive_path,omitempty" db:"archive_path"`
	ObservationUUID *string `json:"observation_uuid,omitempty" db:"observation_uuid"`
}

// A legacy relationship value contains names, not native UUIDs. Keeping its
// representation explicit prevents treating an ambiguous name as a resolved
// performer/studio/tag/group. Unsupported fields remain inspectable extensions.
type SourceFileEdit struct {
	Field       string          `json:"field" db:"field"`
	TargetField string          `json:"target_field" db:"target_field"`
	ValueType   string          `json:"value_type" db:"value_type"`
	Mode        string          `json:"mode" db:"mode"`
	Value       json.RawMessage `json:"value" db:"value_json"`
}

type SourceFileStateChange struct {
	OldState string `json:"old_state" db:"old_state"`
	NewState string `json:"new_state" db:"new_state"`
	Reason   string `json:"reason" db:"reason"`
}

type SourceFileDeduplication struct {
	ContentClaimUUID string  `json:"content_claim_uuid" db:"content_claim_uuid"`
	Stage            string  `json:"stage" db:"stage"`
	SurvivorPath     *string `json:"survivor_path,omitempty" db:"survivor_path"`
}

var ErrSourceFileHistoryInvalid = errors.New("invalid source file history")

type SourceFileHistoryReaderWriter interface {
	Record(context.Context, SourceFileHistory) (*SourceFileHistory, error)
	Find(context.Context, string) (*SourceFileHistory, error)
	// Both list operations use bounded keyset pagination and return summaries;
	// individual Find loads the bounded event's full locations and choices.
	ObservationHistory(context.Context, string, string, int) ([]SourceFileHistory, error)
	ClaimHistory(context.Context, string, string, int) ([]SourceFileHistory, error)
}
