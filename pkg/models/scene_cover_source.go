package models

import (
	"context"
	"fmt"
	"math"
)

// SceneCoverSource is authored cover state, not a generated-image recipe.
// CoverChecksum prevents an upstream/uploaded replacement inheriting an old
// frame selection. FileID is historical: deleting a file must not erase the
// explanation for why a retained cover can no longer be regenerated.
type SceneCoverSource struct {
	CoverChecksum string
	FileID        FileID
	At            float64
	Fingerprint   SceneCoverFingerprint
}

// SceneCoverFingerprint excludes the path so moving the same file is safe.
// Filesystem size/time detect edits before a rescan; recorded scan hashes also
// detect replacements recognized by a rescan. New hashes do not invalidate an
// unchanged file, and derived hashes such as phash are deliberately excluded.
type SceneCoverFingerprint struct {
	Version     int    `json:"version"`
	Size        int64  `json:"size"`
	ModTimeNano int64  `json:"mod_time_ns"`
	MD5         string `json:"md5,omitempty"`
	OSHash      string `json:"oshash,omitempty"`
}

func (s SceneCoverSource) Validate() error {
	if s.CoverChecksum == "" || s.FileID <= 0 || s.At < 0 || math.IsNaN(s.At) || math.IsInf(s.At, 0) ||
		s.Fingerprint.Version != 1 || s.Fingerprint.Size < 0 {
		return fmt.Errorf("invalid scene cover source")
	}
	return nil
}

type SceneCoverSourceReader interface {
	GetCoverSource(ctx context.Context, sceneID int) (*SceneCoverSource, error)
}

type SceneCoverSourceWriter interface {
	SetCoverSource(ctx context.Context, sceneID int, source *SceneCoverSource) error
}
