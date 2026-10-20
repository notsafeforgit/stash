package sqlite

import (
	"context"
	"errors"
	"math"

	"github.com/stashapp/stash/pkg/models"
)

type SceneCoverUpgradeStore struct{}

// Page visits only the finite migration cohort, including unresolved evidence.
// Callers advance by scene ID even when an offline selection remains pending.
func (*SceneCoverUpgradeStore) Page(ctx context.Context, after int) ([]models.SceneCoverUpgrade, error) {
	var ret []models.SceneCoverUpgrade
	err := dbWrapper.Select(ctx, &ret, `SELECT scene_id,cover_checksum,captured FROM scene_cover_upgrade
WHERE scene_id>? ORDER BY scene_id LIMIT 50`, after)
	return ret, err
}

func (*SceneCoverUpgradeStore) Claims(ctx context.Context, id int) ([]models.SceneCoverClaim, error) {
	var ret []models.SceneCoverClaim
	err := dbWrapper.Select(ctx, &ret, `SELECT source_key,at FROM scene_cover_claims WHERE scene_id=? ORDER BY source_key LIMIT 257`, id)
	if len(ret) > 256 {
		return nil, errors.New("cover selection evidence exceeds 256 claims")
	}
	return ret, err
}

func (*SceneCoverUpgradeStore) Capture(ctx context.Context, id int, claims []models.SceneCoverClaim) error {
	if len(claims) > 256 {
		return errors.New("cover selection evidence exceeds 256 claims")
	}
	result, err := dbWrapper.Exec(ctx, "UPDATE scene_cover_upgrade SET captured=1 WHERE scene_id=? AND captured=0", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("cover selection upgrade already captured or superseded")
	}
	for _, claim := range claims {
		if math.IsNaN(claim.At) || math.IsInf(claim.At, 0) || claim.At < 0 {
			return errors.New("invalid cover selection timestamp")
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO scene_cover_claims(scene_id,source_key,at) VALUES(?,?,?)", id, claim.Key, claim.At); err != nil {
			return err
		}
	}
	return nil
}

func (*SceneCoverUpgradeStore) Discard(ctx context.Context, id int) error {
	_, err := dbWrapper.Exec(ctx, "DELETE FROM scene_cover_upgrade WHERE scene_id=?", id)
	return err
}

func (*SceneCoverUpgradeStore) Pending(ctx context.Context, id int) (bool, error) {
	var ret bool
	err := dbWrapper.Get(ctx, &ret, `SELECT EXISTS(SELECT 1 FROM scene_cover_upgrade AS upgrade
JOIN scenes ON scenes.id=upgrade.scene_id AND scenes.cover_blob=upgrade.cover_checksum WHERE upgrade.scene_id=?)`, id)
	return ret, err
}
