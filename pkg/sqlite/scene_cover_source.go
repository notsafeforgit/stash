package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

func (qb *SceneStore) GetCoverSource(ctx context.Context, sceneID int) (*models.SceneCoverSource, error) {
	var row struct {
		Checksum    string        `db:"cover_checksum"`
		FileID      models.FileID `db:"source_file_id"`
		At          float64       `db:"at"`
		Fingerprint []byte        `db:"source_fingerprint"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT source.cover_checksum, source.source_file_id, source.at, source.source_fingerprint
FROM fork_scene_cover_sources AS source
JOIN scenes ON scenes.id = source.scene_id AND scenes.cover_blob = source.cover_checksum
WHERE source.scene_id = ?`, sceneID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ret := &models.SceneCoverSource{CoverChecksum: row.Checksum, FileID: row.FileID, At: row.At}
	if err := json.Unmarshal(row.Fingerprint, &ret.Fingerprint); err != nil {
		return nil, fmt.Errorf("reading scene cover source: %w", err)
	}
	return ret, nil
}

func (qb *SceneStore) SetCoverSource(ctx context.Context, sceneID int, source *models.SceneCoverSource) error {
	if source == nil {
		_, err := dbWrapper.Exec(ctx, "DELETE FROM fork_scene_cover_sources WHERE scene_id = ?", sceneID)
		return err
	}
	if err := source.Validate(); err != nil {
		return err
	}
	fingerprint, err := json.Marshal(source.Fingerprint)
	if err != nil {
		return err
	}
	result, err := dbWrapper.Exec(ctx, `INSERT INTO fork_scene_cover_sources
(scene_id, cover_checksum, source_file_id, at, source_fingerprint)
SELECT ?, ?, ?, ?, ?
WHERE EXISTS (SELECT 1 FROM scenes WHERE id = ? AND cover_blob = ?)
ON CONFLICT(scene_id) DO UPDATE SET
  cover_checksum = excluded.cover_checksum,
  source_file_id = excluded.source_file_id,
  at = excluded.at,
  source_fingerprint = excluded.source_fingerprint`, sceneID, source.CoverChecksum, source.FileID, source.At, fingerprint, sceneID, source.CoverChecksum)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("scene cover changed before its source could be saved")
	}
	return nil
}

func (qb *SceneStore) invalidateCoverSource(ctx context.Context, sceneID int) error {
	_, err := dbWrapper.Exec(ctx, `DELETE FROM fork_scene_cover_sources
WHERE scene_id = ? AND NOT EXISTS (
  SELECT 1 FROM scenes WHERE scenes.id = fork_scene_cover_sources.scene_id
    AND scenes.cover_blob = fork_scene_cover_sources.cover_checksum
)`, sceneID)
	return err
}
