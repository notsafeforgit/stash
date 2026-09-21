package models

import "context"

// ShareRecord is an independent capability. It must never be converted to the
// owner's session or accepted by the ordinary API authentication middleware.
type ShareRecord struct {
	ID             string `db:"id"`
	TokenHash      []byte `db:"token_hash"`
	Label          string `db:"label"`
	CreatedBy      string `db:"created_by"`
	CreatedAt      int64  `db:"created_at"`
	ExpiresAt      int64  `db:"expires_at"`
	RevokedAt      int64  `db:"revoked_at"`
	AllowDownload  bool   `db:"allow_download"`
	ShowMetadata   bool   `db:"show_metadata"`
	Version        int    `db:"version"`
	AccessCount    int    `db:"access_count"`
	LastAccessedAt int64  `db:"last_accessed_at"`
	SnapshotJSON   []byte `db:"snapshot"`
}

type ShareSession struct {
	TokenHash    []byte `db:"token_hash"`
	ShareID      string `db:"share_id"`
	Version      int    `db:"version"`
	ExpiresAt    int64  `db:"expires_at"`
	Preview      bool   `db:"preview"`
	ExchangeOnly bool   `db:"exchange_only"`
}

type ShareListOptions struct {
	Limit, Offset int
	// Nil includes all shares. Filtering happens before pagination.
	Active *bool
	Now    int64
}

// ShareSnapshot freezes membership, ordering and the small metadata allowlist.
// File identities are pinned too: replacing an entity's file does not expand an
// existing grant. No filesystem paths, owner activity or free-form notes appear
// in the public representation of these objects.
type ShareSnapshot struct {
	Entries []ShareEntry `json:"entries"`
	Media   []ShareMedia `json:"media"`
}

type ShareEntry struct {
	Kind      string   `json:"kind"`
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	MediaKeys []string `json:"media_keys"`
}

type ShareMedia struct {
	Key         string                `json:"key"`
	Kind        string                `json:"kind"`
	EntityID    int                   `json:"entity_id"`
	FileID      FileID                `json:"file_id"`
	Fingerprint SceneCoverFingerprint `json:"fingerprint"`
	Title       string                `json:"title"`
	Width       int                   `json:"width"`
	Height      int                   `json:"height"`
	Duration    float64               `json:"duration"`
	VideoCodec  string                `json:"video_codec"`
	AudioCodec  string                `json:"audio_codec"`
	FrameRate   float64               `json:"frame_rate"`
}

type ShareReaderWriter interface {
	Find(context.Context, string) (*ShareRecord, error)
	List(context.Context, ShareListOptions) ([]*ShareRecord, error)
	Create(context.Context, *ShareRecord) error
	Update(context.Context, *ShareRecord) error
	Delete(context.Context, string) error
	PutSession(context.Context, *ShareSession) error
	FindSession(context.Context, []byte) (*ShareSession, error)
	DeleteSession(context.Context, []byte) error
	DeleteSessions(context.Context, string) error
	RecordAccess(context.Context, string, int64) error
}
