package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// DiscoveryCoordinator owns scoped one-page attempts. Each committed page
// releases its source reservation; the listing retains progress across jobs.
type DiscoveryCoordinator struct {
	Service *Service
	Now     func() time.Time
}

func NewDiscoveryCoordinator(service *Service) *DiscoveryCoordinator {
	return &DiscoveryCoordinator{Service: service, Now: time.Now}
}

func (c *DiscoveryCoordinator) listing(ctx context.Context, credential *models.IngestCredential, id string) (*models.DiscoveryListing, error) {
	if !ValidUUID(id) {
		return nil, ErrInvalid
	}
	listing, err := c.Service.Repo.DiscoveryJob.Listing(ctx, id)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, ErrNotFound
	}
	if !permitted(credential, listing.CollectionUUID, listing.RootUUID) {
		return nil, ErrForbidden
	}
	return listing, nil
}

func (c *DiscoveryCoordinator) allowed(ctx context.Context, token, id string) (*models.IngestCredential, *models.ArchiveJob, *models.DiscoveryListing, error) {
	credential, err := c.Service.authenticate(ctx, token)
	if err != nil {
		return nil, nil, nil, err
	}
	if !ValidUUID(id) {
		return nil, nil, nil, ErrInvalid
	}
	job, err := c.Service.Repo.ArchiveJob.Find(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	if job == nil || job.Kind != models.ArchiveJobListAccount {
		return nil, nil, nil, ErrNotFound
	}
	work, err := archive.DecodeDiscoveryJob(job)
	if err != nil {
		return nil, nil, nil, err
	}
	listing, err := c.listing(ctx, credential, work.ListingUUID)
	if err != nil {
		return nil, nil, nil, err
	}
	if listing.Digest != work.DefinitionSHA256 || listing.CollectionUUID != work.CollectionUUID {
		return nil, nil, nil, models.ErrDiscoveryConflict
	}
	return credential, job, listing, nil
}

func (c *DiscoveryCoordinator) guard(ctx context.Context, token string, result *models.ArchiveJob, deadline *time.Time) {
	revision, state, fence := result.Revision, result.State, result.Fence
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if deadline != nil && !c.Now().Before(*deadline) {
			return models.ErrArchiveJobLease
		}
		credential, current, listing, err := c.allowed(ctx, token, result.UUID)
		if err != nil {
			return err
		}
		if current.Revision != revision || current.State != state || current.Fence != fence {
			return models.ErrArchiveJobConflict
		}
		if deadline != nil {
			if current.State == "running" {
				_, err = c.Service.Repo.DiscoveryJob.CheckLease(ctx, models.DiscoveryJobLease{ArchiveJobLease: current.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
			} else {
				_, err = c.Service.Repo.DiscoveryJob.CheckListing(ctx, listing.UUID, c.Now())
			}
		}
		return err
	})
}

func (c *DiscoveryCoordinator) Admit(ctx context.Context, token, id, digest, policy, extractor string) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		listing, err := c.listing(ctx, credential, id)
		if err != nil {
			return err
		}
		executionPolicy, err := c.Service.Repo.WorkerPolicy.Resolve(ctx, models.ArchiveJobListAccount, listing.PolicySHA256)
		if err != nil {
			return err
		}
		if listing.Digest != digest || executionPolicy != policy && listing.PolicySHA256 != policy || listing.ExtractorVersion != extractor {
			return models.ErrDiscoveryConflict
		}
		result, err = c.Service.Repo.DiscoveryJob.Admit(ctx, id, c.Now())
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

func (c *DiscoveryCoordinator) Claim(ctx context.Context, token, id string, expected int64, owner, policy, extractor string, duration time.Duration) (*models.ArchiveJob, error) {
	if !ValidUUID(owner) || expected < 1 || duration < 5*time.Second || duration > 15*time.Minute {
		return nil, ErrInvalid
	}
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, listing, err := c.allowed(ctx, token, id)
		if err != nil {
			return err
		}
		if current.ExecutionPolicySHA256 != policy || listing.ExtractorVersion != extractor {
			return models.ErrDiscoveryConflict
		}
		if current.State == "running" && current.OwnerUUID == owner && current.LeaseUntil != nil && c.Now().Before(*current.LeaseUntil) {
			result, err = c.Service.Repo.DiscoveryJob.CheckLease(ctx, models.DiscoveryJobLease{ArchiveJobLease: current.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
		} else {
			result, err = c.Service.Repo.ArchiveJob.ClaimByID(ctx, id, expected, owner, c.Now(), duration)
			if err == nil && result != nil {
				err = c.Service.Repo.DiscoveryJob.BindAttempt(ctx, models.DiscoveryJobLease{ArchiveJobLease: result.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
			}
		}
		if err != nil {
			return err
		}
		if result != nil {
			c.guard(ctx, token, result, result.LeaseUntil)
		} else {
			c.guard(ctx, token, current, nil)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *DiscoveryCoordinator) Renew(ctx context.Context, token string, lease models.ArchiveJobLease, duration time.Duration) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, _, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		before, err := c.Service.Repo.DiscoveryJob.CheckLease(ctx, models.DiscoveryJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, c.Now())
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

func (c *DiscoveryCoordinator) AppendPage(ctx context.Context, token string, lease models.ArchiveJobLease, ordinal int, body json.RawMessage) (*models.DiscoveryPageReceipt, error) {
	var result *models.DiscoveryPageReceipt
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, before, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.DiscoveryJob.AppendPage(ctx, models.DiscoveryJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, ordinal, body, c.Now())
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

func (c *DiscoveryCoordinator) ReserveSource(ctx context.Context, token string, lease models.ArchiveJobLease, url string) (bool, error) {
	var ready bool
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		ready, err = c.Service.Repo.DiscoveryJob.ReserveSource(ctx, models.DiscoveryJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, url, c.Now())
		if err == nil {
			c.guard(ctx, token, current, current.LeaseUntil)
		}
		return err
	})
	return ready, err
}
