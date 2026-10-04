package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func activeDiscoveryDetails(ctx context.Context) ([]archiveJobRow, error) {
	var rows []archiveJobRow
	err := dbWrapper.Select(ctx, &rows, `SELECT * FROM archive_jobs INDEXED BY archive_jobs_active_work
 WHERE kind='post.verify_candidate' AND state IN ('queued','running') ORDER BY id LIMIT ?`, archive.MaxDiscoveryDetailJobs+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > archive.MaxDiscoveryDetailJobs {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return rows, nil
}
func (s *DiscoveryDetailStore) Ready(ctx context.Context, collection, policy, extractor string, after int64, limit int, now time.Time) ([]models.DiscoveryJobCandidate, error) {
	if !validDiscoveryProfileRequest(collection, policy, extractor, now) || after < 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	rows, err := activeDiscoveryDetails(ctx)
	if err != nil {
		return nil, err
	}
	ret := []models.DiscoveryJobCandidate{}
	for _, row := range rows {
		job := row.resolve()
		if job.Sequence <= after || job.State != "queued" || now.Before(job.AvailableAt) {
			continue
		}
		work, err := archive.DecodeDiscoveryDetailJob(job)
		if err != nil {
			return nil, err
		}
		if work.CollectionUUID != collection || work.PolicySHA256 != policy || work.ExtractorVersion != extractor {
			continue
		}
		if err := discoveryDetailEligible(ctx, work, now); err != nil {
			if errors.Is(err, models.ErrDiscoveryConflict) {
				continue
			}
			return nil, err
		}
		ret = append(ret, models.DiscoveryJobCandidate{Sequence: job.Sequence, UUID: job.UUID})
		if len(ret) == limit {
			break
		}
	}
	return ret, nil
}
func (s *DiscoveryDetailStore) Maintain(ctx context.Context, now time.Time) (*models.DiscoveryMaintenanceResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	complete := discoveryAtomic(ctx)
	rows, err := activeDiscoveryDetails(ctx)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryMaintenanceResult{}
	for _, row := range rows {
		job := row.resolve()
		if now.Before(job.UpdatedAt) {
			continue
		}
		work, err := archive.DecodeDiscoveryDetailJob(job)
		if err != nil {
			return nil, err
		}
		err = discoveryDetailEligible(ctx, work, now)
		if err != nil && !errors.Is(err, models.ErrDiscoveryConflict) {
			return nil, err
		}
		if err != nil {
			if _, err := (&ArchiveJobStore{}).Cancel(ctx, job.UUID, job.Revision, now); err != nil {
				return nil, err
			}
			ret.Cancelled++
		} else if job.State == "running" && job.LeaseUntil != nil && !now.Before(*job.LeaseUntil) {
			if err := recoverArchiveJob(ctx, job, now); err != nil {
				return nil, err
			}
			ret.Recovered++
		}
	}
	*complete = true
	return ret, nil
}
