package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// A policy is owned by a collection, including a directory or manual batch.
// Constants use native UUIDs; names are resolved only by an explicit matcher.
type MetadataMapping struct {
	JQ             string          `json:"jq,omitempty"`
	Value          json.RawMessage `json:"value,omitempty"`
	PerformerNames bool            `json:"performer_names,omitempty"`
}

type MetadataPolicyRule struct {
	OnCreate              bool                       `json:"on_create"`
	OnExisting            bool                       `json:"on_existing"`
	SkipOrganizedOnCreate bool                       `json:"skip_organized_on_create"`
	MarkOrganized         bool                       `json:"mark_organized"`
	FilenameTitleFallback bool                       `json:"filename_title_fallback"`
	Mappings              map[string]MetadataMapping `json:"mappings"`
}

type MetadataPolicyDefinition struct {
	Enabled      bool                                     `json:"enabled"`
	ApplyToScans bool                                     `json:"apply_to_scans"`
	Rules        map[ArchiveEntityKind]MetadataPolicyRule `json:"rules"`
}

type MetadataPolicyRef struct {
	CollectionUUID string `json:"collection_uuid"`
	Revision       int    `json:"revision"`
}

type MetadataPolicy struct {
	MetadataPolicyRef
	CollectionRevision int                      `json:"collection_revision"`
	Definition         MetadataPolicyDefinition `json:"definition"`
	Origin             string                   `json:"origin"`
	Reason             string                   `json:"reason"`
	CreatedAt          time.Time                `json:"created_at"`
}

type MetadataPolicyInput struct {
	CollectionUUID             string                   `json:"collection_uuid"`
	ExpectedRevision           int                      `json:"expected_revision"`
	ExpectedCollectionRevision int                      `json:"expected_collection_revision"`
	Definition                 MetadataPolicyDefinition `json:"definition"`
	Origin                     string                   `json:"origin"`
	Reason                     string                   `json:"reason"`
}

type MetadataScanMatch struct {
	Policy       *MetadataPolicy
	Collection   *SourceCollection
	Root         *MediaRoot
	RelativePath string
	Directory    string
}

var ErrMetadataPolicyConflict = errors.New("metadata policy or collection changed; review a fresh preview")

var ErrMetadataPolicyInvalid = errors.New("invalid metadata policy")

type MetadataPolicyReaderWriter interface {
	Find(context.Context, string) (*MetadataPolicy, error)
	History(context.Context, string, int, int) ([]*MetadataPolicy, error)
	Put(context.Context, MetadataPolicyInput) (*MetadataPolicy, error)
	// MatchScan returns only the most specific active directory scope. Multiple
	// matches at that depth require review; disabled policies can mask a parent.
	MatchScan(context.Context, string) ([]MetadataScanMatch, error)
}

// ScanMetadataHandler runs after registration and before the scan commits.
type ScanMetadataHandler func(context.Context, ArchiveEntityKind, int, File, bool) error
