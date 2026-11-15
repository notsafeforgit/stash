package sqlite

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// enrichmentBoundJob returns the job bound to one exact target revision, if any.
func enrichmentBoundJob(ctx context.Context, id string, revision int) (*models.ArchiveJob, error) {
	binding, err := (&EnrichmentJobStore{}).TargetBinding(ctx, id, revision)
	if err != nil || binding == nil {
		return nil, err
	}
	return (&ArchiveJobStore{}).Find(ctx, binding.JobUUID)
}

// cancelEnrichmentJob cancels a queued or running job whose target revision was superseded.
func cancelEnrichmentJob(ctx context.Context, job *models.ArchiveJob, now time.Time) error {
	if job == nil || (job.State != "queued" && job.State != "running") {
		return nil
	}
	_, err := (&ArchiveJobStore{}).Cancel(ctx, job.UUID, job.Revision, now)
	return err
}

// enrichmentWaitDeadline is the not_before an owner action keeps while the
// target's job still owes a retry deadline. A later explicit time wins. Retry and
// Schedule both use it, so neither owner action can shorten that backoff.
func enrichmentWaitDeadline(notBefore time.Time, job *models.ArchiveJob) time.Time {
	if job != nil && job.AvailableAt.After(notBefore) {
		return job.AvailableAt.UTC()
	}
	return notBefore
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
		job, err := enrichmentBoundJob(ctx, id, expected)
		if err != nil {
			return nil, err
		}
		if job == nil || (job.State != "failed" && job.State != "cancelled") {
			return nil, models.ErrEnrichmentConflict
		}
		deadline = enrichmentWaitDeadline(prior.NotBefore, job)
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
