package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// A source's claim about an asset is not a verified content identity. In
// particular, a path-derived legacy asset ID is never a checksum. Locations
// share this claim without copying its declared digest or source provenance.
type SourceContentClaim struct {
	UUID               string          `db:"uuid"`
	CollectionUUID     string          `db:"collection_uuid"`
	CollectionRevision int             `db:"collection_revision"`
	ReferenceNamespace string          `db:"reference_namespace"`
	ReferenceValue     string          `db:"reference_value"`
	DigestAlgorithm    *string         `db:"digest_algorithm"`
	Digest             *string         `db:"digest"`
	Size               *int64          `db:"size"`
	SourceCreatedAt    string          `db:"source_created_at"`
	Origin             string          `db:"origin"`
	ObservedAt         time.Time       `db:"observed_at"`
	Details            json.RawMessage `db:"details"`
	CreatedAt          time.Time       `db:"created_at"`
}

// File observations retain what a source reported, including unavailable or
// pending locations. They do not create library files or assert current bytes.
// The logical root can be offline. SourceFirstObserved belongs to the source;
// ObservedAt dates this observation of its claims, not filesystem verification.
type SourceFileObservation struct {
	UUID                string          `db:"uuid"`
	ContentClaimUUID    *string         `db:"content_claim_uuid"`
	CollectionUUID      string          `db:"collection_uuid"`
	CollectionRevision  int             `db:"collection_revision"`
	RootUUID            string          `db:"root_uuid"`
	RootRevision        int             `db:"root_revision"`
	RelativePath        string          `db:"relative_path"`
	ArchivePath         *string         `db:"archive_path"` // ZIP path within the root; RelativePath is then the member path
	State               string          `db:"state"`        // present, missing, pending, deduplicated
	Role                string          `db:"role"`         // local, converted-source, source-media-reference
	Size                *int64          `db:"size"`
	ModifiedAtNS        *int64          `db:"modified_at_ns"`
	SourceFirstObserved string          `db:"source_first_observed"`
	SurvivorPath        *string         `db:"survivor_path"` // within the same root/archive
	Origin              string          `db:"origin"`
	ObservedAt          time.Time       `db:"observed_at"`
	Details             json.RawMessage `db:"details"`
	CreatedAt           time.Time       `db:"created_at"`
}

// A match records a library file generation associated with a source location.
// It remains historical evidence after a move, deletion, or byte change. Path
// matching is not byte verification and cannot populate media_contents.
type SourceFileMatch struct {
	UUID              string          `db:"uuid"`
	ObservationUUID   string          `db:"observation_uuid"`
	FileUUID          string          `db:"file_uuid"`
	Generation        int64           `db:"generation"`
	ArchiveFileUUID   *string         `db:"archive_file_uuid"`
	ArchiveGeneration *int64          `db:"archive_generation"`
	LibraryRootPath   string          `db:"library_root_path"` // reviewed historical prefix, not a live root grant
	Basis             string          `db:"basis"`             // exact-path, survivor-path, verified-content, review
	Origin            string          `db:"origin"`            // migration, ingest, review, scan
	Details           json.RawMessage `db:"details"`
	CreatedAt         time.Time       `db:"created_at"`
}

// A post can retain unavailable file evidence without inventing an attachment,
// capture, source order or playable media. This is distinct from a selected
// attachment-to-scene/image association.
type SourcePostFileEvidence struct {
	SourcePostEvidence
	ObservationUUID string
}

var ErrSourceFileEvidenceReplay = errors.New("source file evidence UUID has different contents")
var ErrSourceFileEvidenceInvalid = errors.New("invalid source file evidence")

type SourceFileReaderWriter interface {
	RecordContentClaim(context.Context, SourceContentClaim) (*SourceContentClaim, error)
	ContentClaim(context.Context, string) (*SourceContentClaim, error)
	RecordObservation(context.Context, SourceFileObservation) (*SourceFileObservation, error)
	Observation(context.Context, string) (*SourceFileObservation, error)
	ClaimObservations(context.Context, string, string, int) ([]SourceFileObservation, error)
	LocationObservations(context.Context, string, *string, string, string, int) ([]SourceFileObservation, error)
	RecordMatch(context.Context, SourceFileMatch) (*SourceFileMatch, error)
	Matches(context.Context, string, string, int) ([]SourceFileMatch, error)
	RecordPostEvidence(context.Context, SourcePostFileEvidence) (*SourcePostFileEvidence, error)
	PostEvidence(context.Context, string, string, int) ([]SourcePostFileEvidence, error)
}
