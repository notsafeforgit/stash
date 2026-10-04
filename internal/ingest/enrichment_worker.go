package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type EnrichmentExecution struct {
	Job                 *models.ArchiveJob                    `json:"job"`
	Target              *models.EnrichmentTarget              `json:"target"`
	DiscoveryResolution *models.EnrichmentDiscoveryResolution `json:"discovery_resolution,omitempty"`
}

// Describe uses the job's historical scope. Its target URL/input is immutable;
// a later scheduling revision cannot authorize executing this older job.
func (c *EnrichmentCoordinator) Describe(ctx context.Context, token, id string) (*EnrichmentExecution, error) {
	var ret *EnrichmentExecution
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		_, job, work, err := c.allowed(ctx, token, id)
		if err != nil {
			return err
		}
		target, err := c.Service.Repo.EnrichmentWork.Target(ctx, work.TargetUUID)
		if err != nil {
			return err
		}
		if target == nil {
			return models.ErrEnrichmentConflict
		}
		ret = &EnrichmentExecution{Job: job, Target: target}
		ret.DiscoveryResolution, err = c.Service.Repo.EnrichmentJob.DiscoveryResolution(ctx, id)
		if err != nil {
			return err
		}
		return nil
	})
	return ret, err
}

// Ready exposes only the explicitly selected, currently granted collection.
// Admission/claim revalidate the returned candidates in their write transaction.
func (c *EnrichmentCoordinator) Ready(ctx context.Context, token, collectionID string, limit int) ([]models.EnrichmentTarget, error) {
	return c.ReadyPage(ctx, token, collectionID, nil, limit)
}

func (c *EnrichmentCoordinator) ReadyPage(ctx context.Context, token, collectionID string, after *models.EnrichmentTargetCursor, limit int) ([]models.EnrichmentTarget, error) {
	var ret []models.EnrichmentTarget
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
		ret, err = c.Service.Repo.EnrichmentWork.ReadyPage(ctx, collection.UUID, c.Now(), after, limit)
		return err
	})
	return ret, err
}

func (c *EnrichmentCoordinator) ReadyJobs(ctx context.Context, token, collectionID, policy, extractor string, after int64, limit int) ([]models.EnrichmentJobCandidate, error) {
	var ret []models.EnrichmentJobCandidate
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
		ret, err = c.Service.Repo.EnrichmentJob.Ready(ctx, collectionID, policy, extractor, after, limit, c.Now())
		return err
	})
	return ret, err
}

func (c *EnrichmentCoordinator) ReadyCollections(ctx context.Context, token, policy, extractor, after string, limit int) ([]models.EnrichmentCollectionCandidate, error) {
	var ret []models.EnrichmentCollectionCandidate
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		ret, err = c.Service.Repo.EnrichmentJob.Collections(ctx, models.EnrichmentCollectionQuery{
			Scopes: credential.Scopes, Roots: credential.RootUUIDs, PolicySHA256: policy, ExtractorVersion: extractor, After: after, Limit: limit,
		}, c.Now())
		return err
	})
	return ret, err
}

type EnrichmentFailureReceipt struct {
	models.ArchiveJobAttempt
	ProducerUUID string `json:"producer_uuid"`
}

// ReserveSource coordinates contact with a linked host before any source I/O.
// Authority, immutable work and the current producer/lease are rechecked inside
// the write transaction and immediately before committing the reservation.
func (c *EnrichmentCoordinator) ReserveSource(ctx context.Context, token string, lease models.ArchiveJobLease, url string) (bool, error) {
	var ready bool
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		ready, err = c.Service.Repo.EnrichmentJob.ReserveSource(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, url, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, current, current.LeaseUntil)
		return nil
	})
	return ready && err == nil, err
}

func enrichmentFailureState(code string) string {
	switch code {
	case "rate_limited", "extraction_failed", "timeout", "worker_failed", "source_busy":
		return "retry"
	case "authentication", "access_denied", "challenge", "not_found", "unsupported_extractor",
		"result_too_large", "invalid_checkpoint", "not_a_post_url", "runtime_changed", "post_identity_conflict":
		return "failed"
	default:
		return ""
	}
}

// Fail records only a controlled failure code, never raw extractor output or a
// caller-chosen success/retry deadline. Immutable attempts recover lost replies.
func (c *EnrichmentCoordinator) Fail(ctx context.Context, token string, lease models.ArchiveJobLease, code string) (*EnrichmentFailureReceipt, error) {
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
		bound, err := c.Service.Repo.EnrichmentJob.Attempt(ctx, lease.JobUUID, lease.Fence)
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
				return models.ErrEnrichmentConflict
			}
			ret = &EnrichmentFailureReceipt{ArchiveJobAttempt: attempts[0], ProducerUUID: credential.ProducerUUID}
			c.guard(ctx, token, current, nil)
			return nil
		}
		before, err := c.Service.Repo.EnrichmentJob.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, c.Now())
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
			return models.ErrEnrichmentAtomic
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
