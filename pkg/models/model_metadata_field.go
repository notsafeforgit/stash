package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// MetadataFieldDefinition describes a curated scalar target. Relationships and
// source evidence have separate contracts; a mapping cannot address arbitrary
// entity columns or mutate the retained source payload.
type MetadataFieldDefinition struct {
	Name       string
	Type       string // string, date, integer, boolean
	ClearValue json.RawMessage
}

func MetadataFields(kind ArchiveEntityKind) []MetadataFieldDefinition {
	if kind != ArchiveScene && kind != ArchiveImage && kind != ArchiveGallery {
		return nil
	}
	ret := []MetadataFieldDefinition{
		{Name: "title", Type: "string", ClearValue: json.RawMessage(`""`)},
		{Name: "code", Type: "string", ClearValue: json.RawMessage(`""`)},
		{Name: "details", Type: "string", ClearValue: json.RawMessage(`""`)},
		{Name: "date", Type: "date", ClearValue: json.RawMessage(`null`)},
		{Name: "rating100", Type: "integer", ClearValue: json.RawMessage(`null`)},
		{Name: "organized", Type: "boolean", ClearValue: json.RawMessage(`false`)},
	}
	if kind == ArchiveScene {
		ret = append(ret, MetadataFieldDefinition{Name: "director", Type: "string", ClearValue: json.RawMessage(`""`)},
			MetadataFieldDefinition{Name: "production_date", Type: "date", ClearValue: json.RawMessage(`null`)})
	} else {
		ret = append(ret, MetadataFieldDefinition{Name: "photographer", Type: "string", ClearValue: json.RawMessage(`""`)})
	}
	return ret
}

type MetadataFieldDecision struct {
	UUID        string
	Sequence    int
	EntityUUID  string
	Field       string
	Mode        string // set, clear, inherit, preserved
	Origin      string // library, review, migration, legacy, unattributed, source, policy, filename
	Value       json.RawMessage
	CaptureUUID *string
	Reason      string
	CreatedAt   time.Time
}

type MetadataFieldState struct {
	Entity    *ArchiveEntity
	Field     string
	Value     json.RawMessage
	Mode      string
	Origin    string
	Protected bool
	Decision  *MetadataFieldDecision
}

type MetadataFieldDecisionInput struct {
	EntityUUID             string
	ExpectedEntityRevision int
	Field                  string
	Mode                   string // set, clear, inherit
	// Value is required for set. Clear uses the schema's empty value. Inherit
	// accepts a value selected by the policy service, or its empty value when no
	// candidate is selected. Returning to inheritance is an explicit review act.
	Value       json.RawMessage
	Origin      string // review or migration for Decide; source, policy or filename for ApplyAutomatic
	CaptureUUID string
	Reason      string
}

var (
	ErrMetadataFieldConflict  = errors.New("metadata entity changed; review a fresh preview")
	ErrMetadataFieldProtected = errors.New("metadata field has a preserved or explicit choice")
)

type MetadataFieldReaderWriter interface {
	State(context.Context, string, string) (*MetadataFieldState, error)
	History(context.Context, string, string, int, int) ([]MetadataFieldDecision, error)
	Decide(context.Context, MetadataFieldDecisionInput) (*MetadataFieldState, error)
	// ApplyAutomatic may update inherited values only. Producers must go through
	// the domain policy service; this repository does not authenticate evidence
	// or choose between competing source posts.
	ApplyAutomatic(context.Context, MetadataFieldDecisionInput) (*MetadataFieldState, error)
}
