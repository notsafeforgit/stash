package models

import (
	"context"
	"errors"
	"time"
)

// File generations fence a location's observed changes; content UUIDs identify
// verified bytes. Neither is an identity for a scene, image, or performer.
type MediaContent struct {
	UUID      string
	SHA256    string
	Size      int64
	CreatedAt time.Time
}

type FileSnapshot struct {
	Identity    string    `json:"identity"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modified_at"`
	ChangeToken string    `json:"change_token"`
}

type FileContentVerification struct {
	FileUUID     string
	Generation   int64
	Content      MediaContent
	RootUUID     string
	RootRevision int
	RelativePath string
	Snapshot     FileSnapshot
	VerifiedAt   time.Time
}

type FileContentInput struct {
	FileUUID             string
	ExpectedGeneration   int64
	SHA256               string
	RootUUID             string
	ExpectedRootRevision int
	RelativePath         string
	Snapshot             FileSnapshot
}

type FileContentLocation struct {
	FileUUID   string
	FileID     FileID
	Generation int64
}

var ErrFileGenerationConflict = errors.New("file generation changed or is no longer active")
var ErrFileContentConflict = errors.New("file generation already has a different verified content")

type FileContentReaderWriter interface {
	FindContent(context.Context, string) (*MediaContent, error)
	FindBySHA256(context.Context, string) (*MediaContent, error)
	Current(context.Context, string) (*FileContentVerification, error)
	History(context.Context, string, int64, int) ([]FileContentVerification, error)
	Locations(context.Context, string, string, int) ([]FileContentLocation, error)
	// Owners and MediaCandidates return at most two active library identities:
	// enough to distinguish no match, a unique match, and an ambiguous match.
	Owners(context.Context, string) ([]*ArchiveEntity, error)
	HasOwner(context.Context, string, string) (bool, error)
	MediaCandidates(context.Context, string) ([]*ArchiveEntity, error)
	// RecordVerification records server-verified bytes in the caller's managed
	// write transaction. Callers retain and recheck the descriptor through commit;
	// a producer's checksum alone is not sufficient evidence for this operation.
	RecordVerification(context.Context, FileContentInput) (*FileContentVerification, error)
	// Advance invalidates a generation when new byte evidence was detected even
	// though a pathname/size/mtime comparison did not reveal the replacement.
	Advance(context.Context, string, int64) (int64, error)
}
