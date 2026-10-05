package models

import (
	"context"
	"errors"
	"time"
)

// MediaRootBinding is local deployment state. Root UUIDs and relative paths
// remain portable when the server's mount changes.
type MediaRootBinding struct {
	Path              string `json:"path"`
	DirectoryIdentity string `json:"directory_identity"`
}

type MediaRootDefinition struct {
	Label   string            `json:"label"`
	State   string            `json:"state"` // active, disabled, retired
	Binding *MediaRootBinding `json:"binding"`
}

type MediaRoot struct {
	UUID string `json:"uuid"`
	MediaRootDefinition
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
}

type MediaRootRevision struct {
	MediaRoot
	Origin     string    `json:"origin"`
	Reason     string    `json:"reason"`
	RecordedAt time.Time `json:"recorded_at"`
}

type MediaRootInput struct {
	UUID             string `json:"uuid"`              // optional for creation; imports can preserve UUIDs
	ExpectedRevision int    `json:"expected_revision"` // zero creates a new logical root
	MediaRootDefinition
	Origin string `json:"origin"` // review or migration
	Reason string `json:"reason"`
}

type SourceCollectionDefinition struct {
	Label       string  `json:"label"`
	Kind        string  `json:"kind"`
	Namespace   string  `json:"namespace"`
	State       string  `json:"state"` // active, disabled, retired
	TargetURL   string  `json:"target_url"`
	AccountUUID *string `json:"account_uuid"` // scrape target, not a depicted performer
	RootUUID    *string `json:"root_uuid"`
	PathPrefix  string  `json:"path_prefix"` // canonical relative path, or "." for the entire root
}

type SourceCollection struct {
	UUID string `json:"uuid"`
	SourceCollectionDefinition
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
}

type SourceCollectionRevision struct {
	SourceCollection
	Origin     string    `json:"origin"`
	Reason     string    `json:"reason"`
	RecordedAt time.Time `json:"recorded_at"`
}

type SourceCollectionInput struct {
	UUID             string `json:"uuid"`
	ExpectedRevision int    `json:"expected_revision"`
	SourceCollectionDefinition
	Origin string `json:"origin"` // review, migration, or ingest
	Reason string `json:"reason"`
}

type CollectionCapture struct {
	CaptureUUID        string    `json:"capture_uuid"`
	CollectionUUID     string    `json:"collection_uuid"`
	CollectionRevision int       `json:"collection_revision"`
	CreatedAt          time.Time `json:"created_at"`
}

// SourceDefinitionFilter searches current definitions only, never capture bodies
// or historical definitions. After and Limit bound each response.
type SourceDefinitionFilter struct {
	Query string
	State string
	Kind  string // collections only
	After string
	Limit int
}

// A capture can belong to more than one historical definition of a collection.
// Both fields are required to page those facts without skipping a revision.
type CollectionCaptureCursor struct {
	CaptureUUID        string `json:"capture_uuid"`
	CollectionRevision int    `json:"collection_revision"`
}

// CollectionMediaIntake records provenance for a manual batch or other intake.
// It needs no invented scrape capture/account and does not set media metadata.
type CollectionMediaIntake struct {
	UUID               string    `json:"uuid"` // stable event identity; required for replay
	CollectionUUID     string    `json:"collection_uuid"`
	CollectionRevision int       `json:"collection_revision"`
	MediaUUID          string    `json:"media_uuid"`
	SubmittedMediaUUID string    `json:"submitted_media_uuid"` // output: original submitted identity, retained across UUID adoption
	Origin             string    `json:"origin"`               // scan, ingest, review, or migration
	Reason             string    `json:"reason"`
	CreatedAt          time.Time `json:"created_at"`
}

var (
	ErrSourceDefinitionInvalid  = errors.New("invalid source definition")
	ErrSourceDefinitionConflict = errors.New("source definition changed or is retired")
	ErrCollectionIntakeReplay   = errors.New("collection intake UUID has different contents")
)

type MediaRootReaderWriter interface {
	Put(context.Context, MediaRootInput) (*MediaRoot, error)
	Find(context.Context, string) (*MediaRoot, error)
	List(context.Context, string, int) ([]*MediaRoot, error)
	Search(context.Context, SourceDefinitionFilter) ([]*MediaRoot, error)
	History(context.Context, string, int, int) ([]MediaRootRevision, error)
}

type SourceCollectionReaderWriter interface {
	Put(context.Context, SourceCollectionInput) (*SourceCollection, error)
	Find(context.Context, string) (*SourceCollection, error)
	List(context.Context, string, int) ([]*SourceCollection, error)
	Search(context.Context, SourceDefinitionFilter) ([]*SourceCollection, error)
	LookupTarget(context.Context, string, string, int) ([]*SourceCollection, error)
	LookupCurrentTargets(context.Context, []string, []string, *string, bool) ([]*SourceCollection, error)
	History(context.Context, string, int, int) ([]SourceCollectionRevision, error)
	RecordPostMembership(context.Context, CollectionPostMembership) (*CollectionPostMembership, error)
	Memberships(context.Context, string, string, int) ([]CollectionPostMembership, error)
	PostMemberships(context.Context, string, string, int) ([]CollectionPostMembership, error)
	RecordCapture(context.Context, CollectionCapture) error
	HasCapture(context.Context, CollectionCapture) (bool, error)
	Captures(context.Context, string, *CollectionCaptureCursor, int) ([]CollectionCapture, error)
	RecordMediaIntake(context.Context, CollectionMediaIntake) (*CollectionMediaIntake, error)
	MediaIntake(context.Context, string, string, int) ([]CollectionMediaIntake, error)
}
