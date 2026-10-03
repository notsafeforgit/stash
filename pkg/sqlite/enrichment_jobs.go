package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type EnrichmentJobStore struct{}

func enrichmentRetryAt(job *models.ArchiveJob, now time.Time) time.Time {
	shift := job.Fence - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 8 {
		shift = 8
	}
	return now.Add((5 * time.Minute) << shift)
}

func (s *EnrichmentJobStore) Binding(ctx context.Context, id string) (*models.EnrichmentJobBinding, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := &models.EnrichmentJobBinding{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM enrichment_job_targets WHERE job_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (s *EnrichmentJobStore) TargetBinding(ctx context.Context, id string, revision int) (*models.EnrichmentJobBinding, error) {
	if !validSourceRunUUID(id) || revision < 1 {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := &models.EnrichmentJobBinding{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM enrichment_job_targets WHERE target_uuid=? AND target_revision=?", id, revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func enrichmentJobSubmissionGuard(ctx context.Context, request string) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		current, err := (&ArchiveJobStore{}).FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		work, err := archive.DecodeEnrichmentJob(current)
		if err != nil {
			return err
		}
		binding, err := (&EnrichmentJobStore{}).Binding(ctx, current.UUID)
		if err != nil {
			return err
		}
		if binding == nil || binding.TargetUUID != work.TargetUUID || binding.TargetRevision != work.TargetRevision {
			return models.ErrEnrichmentAtomic
		}
		return nil
	})
}

func enrichmentJobAttemptGuard(ctx context.Context, id string, fence int64) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		attempt, err := (&EnrichmentJobStore{}).Attempt(ctx, id, fence)
		if err != nil {
			return err
		}
		if attempt == nil {
			return models.ErrEnrichmentAtomic
		}
		return nil
	})
}

// Mount paths are not metadata job identity. The pinned collection's logical
// root still has to match, and disabling that root prevents continued work.
func enrichmentJobEligible(ctx context.Context, work *models.EnrichmentJobArguments, now time.Time) (*models.EnrichmentTarget, error) {
	target, err := (&EnrichmentWorkStore{}).Target(ctx, work.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Revision != work.TargetRevision || target.PostUUID != work.PostUUID ||
		target.CollectionUUID != work.CollectionUUID || target.CollectionRevision != work.CollectionRevision ||
		target.State != "pending" || now.Before(target.NotBefore) || now.Before(target.UpdatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	if err := enrichmentEligible(ctx, target.EnrichmentTargetInput); err != nil {
		return nil, err
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, target.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || !reflect.DeepEqual(collection.RootUUID, work.RootUUID) {
		return nil, models.ErrEnrichmentConflict
	}
	if work.RootUUID != nil {
		root, err := (&MediaRootStore{}).Find(ctx, *work.RootUUID)
		if err != nil {
			return nil, err
		}
		if root == nil || root.State != "active" {
			return nil, models.ErrEnrichmentConflict
		}
	}
	return target, nil
}

func (s *EnrichmentJobStore) Bind(ctx context.Context, id string, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	complete := enrichmentAtomic(ctx)
	if !validJobTime(now) {
		return models.ErrEnrichmentInvalid
	}
	current, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return err
	}
	work, err := archive.DecodeEnrichmentJob(current)
	if err != nil {
		return err
	}
	prior, err := s.TargetBinding(ctx, work.TargetUUID, work.TargetRevision)
	if err != nil {
		return err
	}
	if prior != nil {
		if prior.JobUUID != id {
			return models.ErrEnrichmentConflict
		}
		*complete = true
		return nil
	}
	if current.State != "queued" || current.Fence != 0 {
		return models.ErrEnrichmentConflict
	}
	if _, err := enrichmentJobEligible(ctx, work, now); err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO enrichment_job_targets VALUES(?,?,?)", id, work.TargetUUID, work.TargetRevision)
	*complete = err == nil
	return err
}

func (s *EnrichmentJobStore) Attempt(ctx context.Context, id string, fence int64) (*models.EnrichmentJobAttempt, error) {
	if !validSourceRunUUID(id) || fence < 1 {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := &models.EnrichmentJobAttempt{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM enrichment_job_attempts WHERE job_uuid=? AND fence=?", id, fence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (s *EnrichmentJobStore) BindAttempt(ctx context.Context, lease models.EnrichmentJobLease, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	complete := enrichmentAtomic(ctx)
	if !validSourceRunUUID(lease.ProducerUUID) {
		return models.ErrEnrichmentInvalid
	}
	current, err := (&ArchiveJobStore{}).CheckLease(ctx, lease.ArchiveJobLease, now)
	if err != nil {
		return err
	}
	work, err := archive.DecodeEnrichmentJob(current)
	if err != nil {
		return err
	}
	if _, err := enrichmentJobEligible(ctx, work, now); err != nil {
		return err
	}
	prior, err := s.Attempt(ctx, lease.JobUUID, lease.Fence)
	if err != nil {
		return err
	}
	if prior != nil {
		if prior.ProducerUUID != lease.ProducerUUID {
			return models.ErrArchiveJobLease
		}
		*complete = true
		return nil
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO enrichment_job_attempts VALUES(?,?,?)", lease.JobUUID, lease.Fence, lease.ProducerUUID)
	*complete = err == nil
	return err
}

func (s *EnrichmentJobStore) ownedAttempt(ctx context.Context, lease models.EnrichmentJobLease) error {
	if !validSourceRunUUID(lease.JobUUID) || !validSourceRunUUID(lease.ProducerUUID) || !validSourceRunUUID(lease.OwnerUUID) || lease.Fence < 1 {
		return models.ErrArchiveJobLease
	}
	var owned bool
	err := dbWrapper.Get(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM enrichment_job_attempts p
 JOIN archive_job_attempts a ON a.job_uuid=p.job_uuid AND a.fence=p.fence
 WHERE p.job_uuid=? AND p.fence=? AND p.producer_uuid=? AND a.owner_uuid=?)`, lease.JobUUID, lease.Fence, lease.ProducerUUID, lease.OwnerUUID)
	if err != nil {
		return err
	}
	if !owned {
		return models.ErrArchiveJobLease
	}
	return nil
}

func (s *EnrichmentJobStore) CheckLease(ctx context.Context, lease models.EnrichmentJobLease, now time.Time) (*models.ArchiveJob, error) {
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	current, err := (&ArchiveJobStore{}).CheckLease(ctx, lease.ArchiveJobLease, now)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeEnrichmentJob(current)
	if err != nil {
		return nil, err
	}
	if _, err := enrichmentJobEligible(ctx, work, now); err != nil {
		return nil, err
	}
	return current, nil
}
