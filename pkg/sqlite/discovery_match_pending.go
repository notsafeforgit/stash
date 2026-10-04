package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// Pending bounds inspected rows, including stale bindings and targets waiting
// for source data. It reads only target metadata and the next page's existence;
// page decoding belongs to Prepare under a separate read transaction.
func (s *DiscoveryMatchStore) Pending(ctx context.Context, after string, limit int, now time.Time) (*models.DiscoveryComparisonCandidates, error) {
	if (after != "" && !validSourceRunUUID(after)) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	var targets []models.DiscoveryMatchTarget
	err = dbWrapper.Select(ctx, &targets, `SELECT * FROM discovery_match_targets INDEXED BY discovery_match_targets_pending
 WHERE enumeration_complete=0 AND uuid>? ORDER BY uuid LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryComparisonCandidates{Targets: []models.DiscoveryComparisonCandidate{}, After: after, HasMore: len(targets) > limit}
	if ret.HasMore {
		targets = targets[:limit]
	}
	for _, target := range targets {
		ret.After = target.UUID
		if now.Before(target.UpdatedAt) {
			continue
		}
		if err := checkDiscoveryMatchTarget(ctx, &target, now); err != nil {
			if errors.Is(err, models.ErrDiscoveryConflict) {
				continue
			}
			return nil, err
		}
		var exists bool
		if err := dbWrapper.Get(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM discovery_pages WHERE listing_uuid=? AND ordinal=?)", target.ListingUUID, target.LastPage+1); err != nil {
			return nil, err
		}
		if exists {
			ret.Targets = append(ret.Targets, models.DiscoveryComparisonCandidate{UUID: target.UUID, AfterPage: target.LastPage})
		}
	}
	return ret, nil
}
