package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// MetadataFieldDefinition describes a curated target. A mapping cannot address
// arbitrary entity columns or mutate retained source evidence.
type MetadataFieldDefinition struct {
	Name          string
	Type          string // string, date, integer, boolean, urls, custom_fields, reference, references, groups
	ClearValue    json.RawMessage
	ReferenceKind ArchiveEntityKind
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
		{Name: "urls", Type: "urls", ClearValue: json.RawMessage(`[]`)},
		{Name: "custom_fields", Type: "custom_fields", ClearValue: json.RawMessage(`{}`)},
		{Name: "studio", Type: "reference", ClearValue: json.RawMessage(`null`), ReferenceKind: ArchiveStudio},
		{Name: "performers", Type: "references", ClearValue: json.RawMessage(`[]`), ReferenceKind: ArchivePerformer},
		{Name: "tags", Type: "references", ClearValue: json.RawMessage(`[]`), ReferenceKind: ArchiveTag},
	}
	if kind == ArchiveScene {
		ret = append(ret, MetadataFieldDefinition{Name: "director", Type: "string", ClearValue: json.RawMessage(`""`)},
			MetadataFieldDefinition{Name: "production_date", Type: "date", ClearValue: json.RawMessage(`null`)},
			MetadataFieldDefinition{Name: "groups", Type: "groups", ClearValue: json.RawMessage(`[]`), ReferenceKind: ArchiveGroup})
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
	// References supply the current target revisions for reviewed relationship
	// choices. Pending is visible only inside the transaction doing an edit.
	References []*ArchiveEntity
	Pending    bool
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
	// Every nonempty relationship choice must name the reviewed revision of
	// each target. A redirected or deleted target needs a fresh review.
	ReferenceRevisions map[string]int
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
