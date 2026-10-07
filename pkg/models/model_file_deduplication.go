package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Deduplication removes a redundant physical location, never a scene/image or
// its selected metadata. Distinct media owners require a separate merge review.
type FileDeduplicationInput struct {
	RootUUID   string `json:"root_uuid"`
	KeepPath   string `json:"keep_path"`
	RemovePath string `json:"remove_path"`
}

type FileDeduplicationFile struct {
	UUID       string          `json:"uuid"`
	Revision   int             `json:"revision"`
	ID         FileID          `json:"id"`
	Generation int64           `json:"generation"`
	Size       int64           `json:"size"`
	ModifiedAt time.Time       `json:"modified_at"`
	Primary    bool            `json:"primary"`
	Metadata   json.RawMessage `json:"metadata"`
}

// State is read in one database view and included in the preview signature.
// Matches are bounded and retain the original source-to-file-generation proof.
type FileDeduplicationState struct {
	Input         FileDeduplicationInput `json:"input"`
	Root          MediaRoot              `json:"root"`
	Kept          *FileDeduplicationFile `json:"kept"`
	Removed       *FileDeduplicationFile `json:"removed"`
	Owner         *ArchiveEntity         `json:"owner"`
	Matches       []SourceFileMatch      `json:"matches"`
	BlockedReason string                 `json:"blocked_reason,omitempty"`
}

type FileDeduplicationReceipt struct {
	UUID              string          `json:"uuid" db:"uuid"`
	Signature         string          `json:"signature" db:"signature"`
	RootUUID          string          `json:"root_uuid" db:"root_uuid"`
	KeepPath          string          `json:"keep_path" db:"keep_path"`
	RemovePath        string          `json:"remove_path" db:"remove_path"`
	KeptUUID          string          `json:"kept_file_uuid" db:"kept_file_uuid"`
	KeptGeneration    int64           `json:"kept_generation" db:"kept_generation"`
	RemovedUUID       string          `json:"removed_file_uuid" db:"removed_file_uuid"`
	RemovedGeneration int64           `json:"removed_generation" db:"removed_generation"`
	MediaUUID         string          `json:"media_uuid" db:"media_uuid"`
	SHA256            string          `json:"sha256" db:"sha256"`
	Proof             json.RawMessage `json:"proof,omitempty" db:"proof"`
	CommittedAt       time.Time       `json:"committed_at" db:"committed_at"`
}

var ErrFileDeduplicationInvalid = errors.New("invalid file deduplication request")
var ErrFileDeduplicationConflict = errors.New("file deduplication preview changed or requires review")
var ErrFileDeduplicationReplay = errors.New("file deduplication UUID names a different request")

type FileDeduplicationReaderWriter interface {
	Inspect(context.Context, FileDeduplicationInput) (*FileDeduplicationState, error)
	Find(context.Context, string) (*FileDeduplicationReceipt, error)
	// Record preserves verified provenance and redirects the removed identity.
	// The caller must remove its library file row in this managed transaction,
	// after guarding and staging the actual filesystem deletion.
	Record(context.Context, FileDeduplicationState, FileDeduplicationReceipt) (*FileDeduplicationReceipt, error)
}
