package sqlite

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const activeEnrichmentSelect = `SELECT * FROM archive_jobs INDEXED BY archive_jobs_active_work
 WHERE kind='post.enrich' AND state IN ('queued','running') ORDER BY id LIMIT ?`

func activeEnrichmentJobs(ctx context.Context) ([]archiveJobRow, error) {
	var rows []archiveJobRow
	if err := dbWrapper.Select(ctx, &rows, activeEnrichmentSelect, archive.MaxEnrichmentJobs+1); err != nil {
		return nil, err
	}
	if len(rows) > archive.MaxEnrichmentJobs {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return rows, nil
}

func staleEnrichmentSource(err error) bool {
	return errors.Is(err, models.ErrEnrichmentConflict) || errors.Is(err, models.ErrSourcePostConflict) || errors.Is(err, models.ErrSourcePostForgotten)
}

// The partial active index bounds work independently of historical job count.
// Discovery is read-only; only a subsequent scoped claim grants ownership.
func (s *EnrichmentJobStore) Ready(ctx context.Context, collection, policy, extractor string, after int64, limit int, now time.Time) ([]models.EnrichmentJobCandidate, error) {
	if !validSourceRunUUID(collection) || !archive.ValidSHA256(policy) || extractor == "" || len(extractor) > 128 || strings.ContainsAny(extractor, "\r\n\x00") || after < 0 || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	rows, err := activeEnrichmentJobs(ctx)
	if err != nil {
		return nil, err
	}
	ret := []models.EnrichmentJobCandidate{}
	for _, row := range rows {
		job := row.resolve()
		if job.Sequence <= after || job.State != "queued" || now.Before(job.AvailableAt) {
			continue
		}
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return nil, err
		}
		executionPolicy, _, err := metadataWorkerPolicy(ctx, job.Kind, work.PolicySHA256)
		if err != nil {
			return nil, err
		}
		if work.CollectionUUID != collection || executionPolicy != policy || work.ExtractorVersion != extractor {
			continue
		}
		if _, err := enrichmentJobEligible(ctx, work, now); err != nil {
			if staleEnrichmentSource(err) {
				continue
			}
			return nil, err
		}
		ret = append(ret, models.EnrichmentJobCandidate{Sequence: job.Sequence, UUID: job.UUID})
		if len(ret) == limit {
			break
		}
	}
	return ret, nil
}

// Maintain is an application operation, never a producer-controlled route. It
// retains checkpoints and receipts while ending stale/expired ownership.
func (s *EnrichmentJobStore) Maintain(ctx context.Context, now time.Time) (*models.EnrichmentMaintenanceResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	if !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	rows, err := activeEnrichmentJobs(ctx)
	if err != nil {
		return nil, err
	}
	ret := &models.EnrichmentMaintenanceResult{}
	for _, row := range rows {
		job := row.resolve()
		if now.Before(job.UpdatedAt) {
			continue
		}
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return nil, err
		}
		target, err := (&EnrichmentWorkStore{}).Target(ctx, work.TargetUUID)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		stale := target.Revision != work.TargetRevision || target.State != "pending"
		if !stale {
			err = enrichmentSourceEligible(ctx, work, target)
			if err != nil && !staleEnrichmentSource(err) {
				return nil, err
			}
			stale = err != nil
		}
		if stale {
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
