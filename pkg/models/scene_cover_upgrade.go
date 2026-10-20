package models

import "context"

// SceneCoverClaim retains only authored evidence from the original preview
// manifests. It is not a rendition cache or an ongoing observation history.
type SceneCoverClaim struct {
	Key string  `db:"source_key"`
	At  float64 `db:"at"`
}

type SceneCoverUpgrade struct {
	SceneID  int    `db:"scene_id"`
	Checksum string `db:"cover_checksum"`
	Captured bool   `db:"captured"`
}

type SceneCoverUpgradeReaderWriter interface {
	Page(context.Context, int) ([]SceneCoverUpgrade, error)
	Claims(context.Context, int) ([]SceneCoverClaim, error)
	Capture(context.Context, int, []SceneCoverClaim) error
	Discard(context.Context, int) error
	Pending(context.Context, int) (bool, error)
}
