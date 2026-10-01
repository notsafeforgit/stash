package models

import (
	"context"
	"errors"
	"time"
)

// MediaRootBinding is local deployment state. Root UUIDs and relative paths
// remain portable when the server's mount changes.
type MediaRootBinding struct {
	Path              string
	DirectoryIdentity string
}

type MediaRootDefinition struct {
	Label   string
	State   string // active, disabled, retired
	Binding *MediaRootBinding
}

type MediaRoot struct {
	UUID string
	MediaRootDefinition
	Revision  int
	CreatedAt time.Time
}

type MediaRootRevision struct {
	MediaRoot
	Origin     string
	Reason     string
	RecordedAt time.Time
}

type MediaRootInput struct {
	UUID             string // optional for creation; imports can preserve UUIDs
	ExpectedRevision int    // zero creates a new logical root
	MediaRootDefinition
	Origin string // review or migration
	Reason string
}

type SourceCollectionDefinition struct {
	Label       string
	Kind        string
	Namespace   string
	State       string // active, disabled, retired
	TargetURL   string
	AccountUUID *string // scrape target, not a depicted performer
	RootUUID    *string
	PathPrefix  string // canonical relative path, or "." for the entire root
}

type SourceCollection struct {
	UUID string
	SourceCollectionDefinition
	Revision  int
	CreatedAt time.Time
}

type SourceCollectionRevision struct {
	SourceCollection
	Origin     string
	Reason     string
	RecordedAt time.Time
}

type SourceCollectionInput struct {
	UUID             string
	ExpectedRevision int
	SourceCollectionDefinition
	Origin string // review, migration, or ingest
	Reason string
}

type CollectionCapture struct {
	CaptureUUID        string
	CollectionUUID     string
	CollectionRevision int
	CreatedAt          time.Time
}

// A capture can belong to more than one historical definition of a collection.
// Both fields are required to page those facts without skipping a revision.
type CollectionCaptureCursor struct {
	CaptureUUID        string
	CollectionRevision int
}

// CollectionMediaIntake records provenance for a manual batch or other intake.
// It needs no invented scrape capture/account and does not set media metadata.
type CollectionMediaIntake struct {
	UUID               string // stable event identity; required for replay
	CollectionUUID     string
	CollectionRevision int
	MediaUUID          string
	SubmittedMediaUUID string // output: original submitted identity, retained across UUID adoption
	Origin             string // scan, ingest, review, or migration
	Reason             string
	CreatedAt          time.Time
}

var (
	ErrSourceDefinitionConflict = errors.New("source definition changed or is retired")
	ErrCollectionIntakeReplay   = errors.New("collection intake UUID has different contents")
)

type MediaRootReaderWriter interface {
	Put(context.Context, MediaRootInput) (*MediaRoot, error)
	Find(context.Context, string) (*MediaRoot, error)
	List(context.Context, string, int) ([]*MediaRoot, error)
	History(context.Context, string, int, int) ([]MediaRootRevision, error)
}

type SourceCollectionReaderWriter interface {
	Put(context.Context, SourceCollectionInput) (*SourceCollection, error)
	Find(context.Context, string) (*SourceCollection, error)
	List(context.Context, string, int) ([]*SourceCollection, error)
	LookupTarget(context.Context, string, string, int) ([]*SourceCollection, error)
	History(context.Context, string, int, int) ([]SourceCollectionRevision, error)
	RecordCapture(context.Context, CollectionCapture) error
	HasCapture(context.Context, CollectionCapture) (bool, error)
	Captures(context.Context, string, *CollectionCaptureCursor, int) ([]CollectionCapture, error)
	RecordMediaIntake(context.Context, CollectionMediaIntake) (*CollectionMediaIntake, error)
	MediaIntake(context.Context, string, string, int) ([]CollectionMediaIntake, error)
}
