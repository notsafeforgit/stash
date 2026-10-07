package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/stashapp/stash/pkg/models"
)

// DeduplicationProof records the exact database view and filesystem inspections
// reviewed before byte verification. The receipt also binds the verified SHA-256.
type DeduplicationProof struct {
	Version int                           `json:"version"`
	State   models.FileDeduplicationState `json:"state"`
	Keep    *MediaFileInspection          `json:"keep"`
	Remove  *MediaFileInspection          `json:"remove"`
}

func (p *DeduplicationProof) Signature() (string, error) {
	encoded, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return DeduplicationSignature(encoded)
}

// Retained proofs are signed as their original bytes. Future additions to Go
// models must not invalidate old receipts by re-encoding new default fields.
func DeduplicationSignature(encoded []byte) (string, error) {
	if len(encoded) > 2<<20 || !json.Valid(encoded) {
		return "", models.ErrFileDeduplicationConflict
	}
	h := sha256.Sum256(append([]byte("stash-physical-deduplication-v1\x00"), encoded...))
	return hex.EncodeToString(h[:]), nil
}
