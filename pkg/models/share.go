package models

import "context"

// ShareRecord is an independent capability. It must never be converted to the
// owner's session or accepted by the ordinary API authentication middleware.
type ShareRecord struct {
	ID        string `db:"id"`
	TokenHash []byte `db:"token_hash"`
	// Only authenticated share management returns this stored capability.
	Token               string `db:"token" json:"-"`
	Label               string `db:"label"`
	CreatedBy           string `db:"created_by"`
	CreatedAt           int64  `db:"created_at"`
	ExpiresAt           int64  `db:"expires_at"`
	RevokedAt           int64  `db:"revoked_at"`
	AllowDownload       bool   `db:"allow_download"`
	ShowMetadata        bool   `db:"show_metadata"`
	Version             int    `db:"version"`
	AccessCount         int    `db:"access_count"`
	LastAccessedAt      int64  `db:"last_accessed_at"`
	SourceJSON          []byte `db:"source"`
	PreparationState    string `db:"preparation_state"`
	PreparationRevision int    `db:"preparation_revision"`
	PreparationError    string `db:"preparation_error"`
	PreparedCount       int    `db:"prepared_count"`
	MediaCount          int    `db:"media_count"`
	SelectionCount      int    `db:"selection_count"`
}

// ShareSource is immutable owner-only scope. Filter references are library
// UUIDs, resolved afresh at query time; a deleted/reused local key cannot widen it.
// A nil source on older grants means an explicitly selected snapshot.
type ShareSource struct {
	Mode       string             `json:"mode"`
	Filter     *ShareFilter       `json:"filter,omitempty"`
	Entity     *ShareEntitySource `json:"entity,omitempty"`
	MediaTypes []string           `json:"media_types,omitempty"`
}

type ShareFilter struct {
	Kind string     `json:"kind"`
	Q    string     `json:"q,omitempty"`
	AST  *FilterAST `json:"ast,omitempty"`
}

type ShareEntitySource struct {
	Kind string `json:"kind"`
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type ShareSession struct {
	TokenHash []byte `db:"token_hash"`
	ShareID   string `db:"share_id"`
	Version   int    `db:"version"`
	ExpiresAt int64  `db:"expires_at"`
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
	Position   int      `json:"-" db:"position"`
	Key        string   `json:"key,omitempty"`
	Kind       string   `json:"kind"`
	ID         int      `json:"id"`
	UUID       string   `json:"uuid,omitempty"`
	Title      string   `json:"title"`
	MediaKeys  []string `json:"media_keys"`
	MediaCount int      `json:"media_count"`
}

type ShareTarget struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

// SharePreparedMedia is a frozen candidate, not a live library selection.
type SharePreparedMedia struct {
	Position      int    `db:"position"`
	MediaJSON     []byte `db:"media"`
	ExpectedSize  int64  `db:"expected_size"`
	ExpectedMtime int64  `db:"expected_mtime"`
}

type ShareMedia struct {
	FileUUID       string                `json:"file_uuid,omitempty"`
	FileGeneration int64                 `json:"file_generation,omitempty"`
	UUID           string                `json:"uuid,omitempty"`
	Key            string                `json:"key"`
	Kind           string                `json:"kind"`
	EntityID       int                   `json:"entity_id"`
	FileID         FileID                `json:"file_id"`
	Fingerprint    SceneCoverFingerprint `json:"fingerprint"`
	Title          string                `json:"title"`
	Width          int                   `json:"width"`
	Height         int                   `json:"height"`
	Duration       float64               `json:"duration"`
	VideoCodec     string                `json:"video_codec"`
	AudioCodec     string                `json:"audio_codec"`
	FrameRate      float64               `json:"frame_rate"`
}

type ShareReaderWriter interface {
	FreezeTargets(context.Context, string, []ShareTarget, []ShareFilter) error
	FreezeMedia(context.Context, string) error
	Preparation(context.Context, string, int) ([]SharePreparedMedia, error)
	NextPreparation(context.Context, int64) (*ShareRecord, error)
	CheckpointPreparation(context.Context, *ShareRecord, []ShareMedia, string) (bool, error)
	RetryPreparation(context.Context, string) error
	SnapshotEntries(context.Context, string, int, int, string, string) ([]ShareEntry, error)
	SnapshotMedia(context.Context, string, int, int, string, string) ([]SharePreparedMedia, error)
	SnapshotItem(context.Context, string, string) (*ShareMedia, error)
	SnapshotEntryKeys(context.Context, string, string, []string) ([]string, error)
	// QueryTargets reuses library filtering, with a stable keyset order and an
	// optional exact membership check. IDs remain repository-local.
	QueryTargets(context.Context, ShareFilter, int, int, *int) ([]int, error)
	Find(context.Context, string) (*ShareRecord, error)
	FindByTokenHash(context.Context, []byte) (*ShareRecord, error)
	List(context.Context, ShareListOptions) ([]*ShareRecord, error)
	Create(context.Context, *ShareRecord) error
	Update(context.Context, *ShareRecord) error
	Delete(context.Context, string) error
	DeleteInactive(context.Context, int64) ([]string, error)
	PutSession(context.Context, *ShareSession) error
	FindSession(context.Context, []byte) (*ShareSession, error)
	DeleteSessions(context.Context, string) error
	RecordAccess(context.Context, string, int64) error
}
