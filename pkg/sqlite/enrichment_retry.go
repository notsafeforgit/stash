package sqlite

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

func cancelEnrichmentTargetJob(ctx context.Context, id string, revision int, now time.Time) error {
	binding, err := (&EnrichmentJobStore{}).TargetBinding(ctx, id, revision)
	if err != nil || binding == nil {
		return err
	}
	job, err := (&ArchiveJobStore{}).Find(ctx, binding.JobUUID)
	if err != nil {
		return err
	}
	if job != nil && (job.State == "queued" || job.State == "running") {
		_, err = (&ArchiveJobStore{}).Cancel(ctx, job.UUID, job.Revision, now)
	}
	return err
}

// Retry is an explicit owner action. A timer or repeat admission cannot release
// a hold, rebind a completed attempt, or shorten its existing retry deadline.
func (s *EnrichmentWorkStore) Retry(ctx context.Context, id string, expected int, now time.Time) (*models.EnrichmentTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || expected < 1 {
		return nil, models.ErrEnrichmentInvalid
	}
	prior, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior == nil || prior.Revision != expected || now.Before(prior.UpdatedAt) || (prior.State != "held" && prior.State != "pending") {
		return nil, models.ErrEnrichmentConflict
	}
	deadline := prior.NotBefore
	if prior.State == "pending" {
		binding, err := (&EnrichmentJobStore{}).TargetBinding(ctx, id, expected)
		if err != nil {
			return nil, err
		}
		if binding == nil {
			return nil, models.ErrEnrichmentConflict
		}
		job, err := (&ArchiveJobStore{}).Find(ctx, binding.JobUUID)
		if err != nil {
			return nil, err
		}
		if job == nil || (job.State != "failed" && job.State != "cancelled") {
			return nil, models.ErrEnrichmentConflict
		}
		if job.AvailableAt.After(deadline) {
			deadline = job.AvailableAt
		}
	}
	if err := enrichmentEligible(ctx, prior.EnrichmentTargetInput); err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, "UPDATE enrichment_targets SET state='pending',reason='',not_before=?,revision=revision+1,updated_at=? WHERE uuid=?", deadline.UTC(), now.UTC(), id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	*complete = err == nil
	return ret, err
}
