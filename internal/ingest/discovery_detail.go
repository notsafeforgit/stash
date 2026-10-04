package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// DiscoveryDetailCoordinator owns candidate-bound metadata attempts. Source
// extraction stays external; completion retains a comparison without accepting identity.
type DiscoveryDetailCoordinator struct {
	Service *Service
	Now     func() time.Time
}

func NewDiscoveryDetailCoordinator(service *Service) *DiscoveryDetailCoordinator {
	return &DiscoveryDetailCoordinator{Service: service, Now: time.Now}
}

func (c *DiscoveryDetailCoordinator) allowed(ctx context.Context, token, id string) (*models.IngestCredential, *models.ArchiveJob, *models.DiscoveryDetailJobArguments, error) {
	credential, err := c.Service.authenticate(ctx, token)
	if err != nil {
		return nil, nil, nil, err
	}
	if !ValidUUID(id) {
		return nil, nil, nil, ErrInvalid
	}
	current, err := c.Service.Repo.ArchiveJob.Find(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	if current == nil || current.Kind != models.ArchiveJobVerifyCandidate {
		return nil, nil, nil, ErrNotFound
	}
	work, err := archive.DecodeDiscoveryDetailJob(current)
	if err != nil {
		return nil, nil, nil, err
	}
	if !permitted(credential, work.CollectionUUID, work.RootUUID) {
		return nil, nil, nil, ErrForbidden
	}
	return credential, current, work, nil
}

func (c *DiscoveryDetailCoordinator) guard(ctx context.Context, token string, result *models.ArchiveJob, deadline *time.Time) {
	revision, state, fence := result.Revision, result.State, result.Fence
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if deadline != nil && !c.Now().Before(*deadline) {
			return models.ErrArchiveJobLease
		}
		credential, current, _, err := c.allowed(ctx, token, result.UUID)
		if err != nil {
			return err
		}
		if current.Revision != revision || current.State != state || current.Fence != fence {
			return models.ErrArchiveJobConflict
		}
		if deadline != nil {
			if current.State == "running" {
				_, err = c.Service.Repo.DiscoveryDetail.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: current.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
			} else {
				_, err = c.Service.Repo.DiscoveryDetail.CheckJob(ctx, current.UUID, c.Now())
			}
		}
		return err
	})
}

func (c *DiscoveryDetailCoordinator) Find(ctx context.Context, token, id string) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		_, result, _, err = c.allowed(ctx, token, id)
		return err
	})
	return result, err
}

func (c *DiscoveryDetailCoordinator) Claim(ctx context.Context, token, id string, expected int64, owner, policy, extractor string, duration time.Duration) (*models.ArchiveJob, error) {
	if !ValidUUID(owner) || expected < 1 || duration < 5*time.Second || duration > 15*time.Minute {
		return nil, ErrInvalid
	}
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, work, err := c.allowed(ctx, token, id)
		if err != nil {
			return err
		}
		if work.PolicySHA256 != policy || work.ExtractorVersion != extractor {
			return models.ErrDiscoveryConflict
		}
		if current.State == "running" && current.OwnerUUID == owner && current.LeaseUntil != nil && c.Now().Before(*current.LeaseUntil) {
			result, err = c.Service.Repo.DiscoveryDetail.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: current.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
		} else {
			result, err = c.Service.Repo.ArchiveJob.ClaimByID(ctx, id, expected, owner, c.Now(), duration)
			if err == nil && result != nil {
				err = c.Service.Repo.DiscoveryDetail.BindAttempt(ctx, models.EnrichmentJobLease{ArchiveJobLease: result.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
			}
		}
		if err != nil {
			return err
		}
		if result != nil {
			c.guard(ctx, token, result, result.LeaseUntil)
		} else {
			// A blocked claim may record live interest for scheduling fairness.
			c.guard(ctx, token, current, nil)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *DiscoveryDetailCoordinator) Renew(ctx context.Context, token string, lease models.ArchiveJobLease, duration time.Duration) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, _, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		before, err := c.Service.Repo.DiscoveryDetail.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, c.Now())
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.ArchiveJob.Renew(ctx, lease, c.Now(), duration)
		if err == nil {
			c.guard(ctx, token, result, before.LeaseUntil)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *DiscoveryDetailCoordinator) Checkpoint(ctx context.Context, token string, lease models.ArchiveJobLease, expected int, body json.RawMessage) (*models.EnrichmentCheckpointReceipt, error) {
	var result *models.EnrichmentCheckpointReceipt
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, before, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.DiscoveryDetail.Checkpoint(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, expected, body, c.Now())
		if err != nil {
			return err
		}
		after, err := c.Service.Repo.ArchiveJob.Find(ctx, lease.JobUUID)
		if err != nil {
			return err
		}
		var deadline *time.Time
		if before.Revision != after.Revision {
			deadline = before.LeaseUntil
		}
		c.guard(ctx, token, after, deadline)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *DiscoveryDetailCoordinator) CheckpointHead(ctx context.Context, token, id string) (*models.EnrichmentCheckpoint, error) {
	var result *models.EnrichmentCheckpoint
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.DiscoveryDetail.CheckpointHead(ctx, id)
		return err
	})
	return result, err
}
func (c *DiscoveryDetailCoordinator) ReadyJobs(ctx context.Context, token, collectionID, policy, extractor string, after int64, limit int) ([]models.DiscoveryJobCandidate, error) {
	var ret []models.DiscoveryJobCandidate
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if !ValidUUID(collectionID) {
			return ErrInvalid
		}
		collection, err := c.Service.Repo.SourceCollection.Find(ctx, collectionID)
		if err != nil {
			return err
		}
		if collection == nil {
			return ErrNotFound
		}
		if !permitted(credential, collection.UUID, collection.RootUUID) {
			return ErrForbidden
		}
		ret, err = c.Service.Repo.DiscoveryDetail.Ready(ctx, collectionID, policy, extractor, after, limit, c.Now())
		return err
	})
	return ret, err
}

func (c *DiscoveryDetailCoordinator) ReserveSource(ctx context.Context, token string, lease models.ArchiveJobLease, url string) (bool, error) {
	var ready bool
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		ready, err = c.Service.Repo.DiscoveryDetail.ReserveSource(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, url, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, current, current.LeaseUntil)
		return nil
	})
	return ready && err == nil, err
}

func (c *DiscoveryDetailCoordinator) Fail(ctx context.Context, token string, lease models.ArchiveJobLease, code string) (*EnrichmentFailureReceipt, error) {
	state := enrichmentFailureState(code)
	if state == "" || !ValidUUID(lease.JobUUID) || !ValidUUID(lease.OwnerUUID) || lease.Fence < 1 {
		return nil, ErrInvalid
	}
	var ret *EnrichmentFailureReceipt
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		bound, err := c.Service.Repo.DiscoveryDetail.Attempt(ctx, lease.JobUUID, lease.Fence)
		if err != nil {
			return err
		}
		if bound == nil || bound.ProducerUUID != credential.ProducerUUID {
			return models.ErrArchiveJobLease
		}
		attempts, err := c.Service.Repo.ArchiveJob.Attempts(ctx, lease.JobUUID, lease.Fence-1, 1)
		if err != nil {
			return err
		}
		if len(attempts) != 1 || attempts[0].Fence != lease.Fence || attempts[0].OwnerUUID != lease.OwnerUUID {
			return models.ErrArchiveJobLease
		}
		expected := state
		if lease.Fence >= int64(current.MaxAttempts) {
			expected = "failed"
		}
		if attempts[0].EndedAt != nil {
			if attempts[0].ErrorCode != code || attempts[0].Outcome != expected {
				return models.ErrDiscoveryConflict
			}
			ret = &EnrichmentFailureReceipt{ArchiveJobAttempt: attempts[0], ProducerUUID: credential.ProducerUUID}
			c.guard(ctx, token, current, nil)
			return nil
		}
		before, err := c.Service.Repo.DiscoveryDetail.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, c.Now())
		if err != nil {
			return err
		}
		outcome := models.ArchiveJobOutcome{State: state, ErrorCode: code, Result: []byte(`{}`)}
		if state == "retry" {
			// The repository applies the attempt-based minimum backoff.
			outcome.RetryAt = c.Now().Add(5 * time.Minute)
		}
		after, err := c.Service.Repo.ArchiveJob.Finish(ctx, lease, c.Now(), outcome)
		if err != nil {
			return err
		}
		attempts, err = c.Service.Repo.ArchiveJob.Attempts(ctx, lease.JobUUID, lease.Fence-1, 1)
		if err != nil {
			return err
		}
		if len(attempts) != 1 || attempts[0].EndedAt == nil || attempts[0].Outcome != expected || attempts[0].ErrorCode != code {
			return models.ErrDiscoveryAtomic
		}
		ret = &EnrichmentFailureReceipt{ArchiveJobAttempt: attempts[0], ProducerUUID: credential.ProducerUUID}
		c.guard(ctx, token, after, nil)
		txn.AddPreCommitHook(ctx, func(context.Context) error {
			if !c.Now().Before(*before.LeaseUntil) {
				return models.ErrArchiveJobLease
			}
			return nil
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *DiscoveryDetailCoordinator) Admit(ctx context.Context, token string, input models.DiscoveryDetailAdmission) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		target, err := c.Service.Repo.DiscoveryMatch.Target(ctx, input.TargetUUID)
		if err != nil {
			return err
		}
		if target == nil {
			return ErrNotFound
		}
		listing, err := c.Service.Repo.DiscoveryJob.Listing(ctx, target.ListingUUID)
		if err != nil {
			return err
		}
		if listing == nil {
			return ErrDefinition
		}
		if !permitted(credential, listing.CollectionUUID, listing.RootUUID) {
			return ErrForbidden
		}
		result, err = c.Service.Repo.DiscoveryDetail.Admit(ctx, input, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, result, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (c *DiscoveryDetailCoordinator) Retry(ctx context.Context, token, id string) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.DiscoveryDetail.Retry(ctx, id, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, result, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (c *DiscoveryDetailCoordinator) Complete(ctx context.Context, token string, lease models.ArchiveJobLease, revision int, digest string) (*models.DiscoveryDetailResult, error) {
	var result *models.DiscoveryDetailResult
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, before, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.DiscoveryDetail.Complete(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, revision, digest, c.Now())
		if err != nil {
			return err
		}
		after, err := c.Service.Repo.ArchiveJob.Find(ctx, lease.JobUUID)
		if err != nil {
			return err
		}
		var deadline *time.Time
		if before.Revision != after.Revision {
			deadline = before.LeaseUntil
		}
		c.guard(ctx, token, after, deadline)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (c *DiscoveryDetailCoordinator) Result(ctx context.Context, token, id string) (*models.DiscoveryDetailResult, error) {
	var result *models.DiscoveryDetailResult
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.DiscoveryDetail.Result(ctx, id)
		return err
	})
	return result, err
}
