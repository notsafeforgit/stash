package sqlite

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// Limit applies to inspected primary-key rows, not just eligible results. A run
// of completed or blocked targets cannot turn this into a full-library query.
// Readiness needs no source page bodies and grants no publication ownership.
func (s *DiscoveryMatchStore) PendingPublications(ctx context.Context, after string, limit int, now time.Time) (*models.DiscoveryPublicationCandidates, error) {
	if (after != "" && !validSourceRunUUID(after)) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	var rows []struct {
		UUID      string    `db:"uuid"`
		Complete  bool      `db:"enumeration_complete"`
		UpdatedAt time.Time `db:"updated_at"`
		Published bool      `db:"published"`
	}
	err = dbWrapper.Select(ctx, &rows, `SELECT t.uuid,t.enumeration_complete,t.updated_at,
 EXISTS(SELECT 1 FROM discovery_match_publications p WHERE p.target_uuid=t.uuid) AS published
 FROM discovery_match_targets t WHERE t.uuid>? ORDER BY t.uuid LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryPublicationCandidates{Targets: []models.DiscoveryPublicationInput{}, After: after, HasMore: len(rows) > limit}
	if ret.HasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		ret.After = row.UUID
		if !row.Complete || row.Published || now.Before(row.UpdatedAt) {
			continue
		}
		review, err := s.Review(ctx, row.UUID)
		if err != nil {
			return nil, err
		}
		if review != nil && review.Publication == nil && review.Coverage.Complete && len(review.Blockers) == 0 {
			input := models.DiscoveryPublicationInput{TargetUUID: row.UUID, ExpectedTargetRevision: review.Target.Revision}
			if review.Detail != nil {
				if now.Before(review.Detail.CreatedAt) {
					continue
				}
				input.DetailJobUUID = review.Detail.JobUUID
			}
			ret.Targets = append(ret.Targets, input)
		}
	}
	return ret, nil
}
