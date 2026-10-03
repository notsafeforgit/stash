package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// EnrichmentCoordinator owns scoped admission and checkpointing. Extraction
// stays external; a saved transcript is not a published capture or completion.
type EnrichmentCoordinator struct {
	Service *Service
	Now     func() time.Time
}

func NewEnrichmentCoordinator(service *Service) *EnrichmentCoordinator {
	return &EnrichmentCoordinator{Service: service, Now: time.Now}
}

func (c *EnrichmentCoordinator) allowed(ctx context.Context, token, id string) (*models.IngestCredential, *models.ArchiveJob, *models.EnrichmentJobArguments, error) {
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
	if current == nil || current.Kind != models.ArchiveJobEnrichPost {
		return nil, nil, nil, ErrNotFound
	}
	work, err := archive.DecodeEnrichmentJob(current)
	if err != nil {
		return nil, nil, nil, err
	}
	if !permitted(credential, work.CollectionUUID, work.RootUUID) {
		return nil, nil, nil, ErrForbidden
	}
	return credential, current, work, nil
}

func (c *EnrichmentCoordinator) guard(ctx context.Context, token string, result *models.ArchiveJob, deadline *time.Time) {
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
			_, err = c.Service.Repo.EnrichmentJob.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: current.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
		}
		return err
	})
}

func (c *EnrichmentCoordinator) Admit(ctx context.Context, token, targetID string, expected int, policy, extractor string) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		binding, err := c.Service.Repo.EnrichmentJob.TargetBinding(ctx, targetID, expected)
		if err != nil {
			return err
		}
		if binding != nil {
			var work *models.EnrichmentJobArguments
			_, result, work, err = c.allowed(ctx, token, binding.JobUUID)
			if err != nil {
				return err
			}
			if work.PolicySHA256 != policy || work.ExtractorVersion != extractor {
				return models.ErrEnrichmentConflict
			}
			c.guard(ctx, token, result, nil)
			return nil
		}
		target, err := c.Service.Repo.EnrichmentWork.Target(ctx, targetID)
		if err != nil {
			return err
		}
		if target == nil {
			return ErrNotFound
		}
		collection, err := c.Service.Repo.SourceCollection.Find(ctx, target.CollectionUUID)
		if err != nil {
			return err
		}
		if collection == nil {
			return ErrDefinition
		}
		if !permitted(credential, collection.UUID, collection.RootUUID) {
			return ErrForbidden
		}
		if target.Revision != expected || target.State != "pending" || collection.Revision != target.CollectionRevision || collection.State != "active" {
			return models.ErrEnrichmentConflict
		}
		input, err := archive.PrepareEnrichmentJob(models.EnrichmentJobArguments{Version: 1, TargetUUID: target.UUID, TargetRevision: target.Revision,
			PostUUID: target.PostUUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: collection.RootUUID,
			PolicySHA256: policy, ExtractorVersion: extractor})
		if err != nil {
			return err
		}
		input.Priority, input.AvailableAt = target.Priority, target.NotBefore
		result, err = c.Service.Repo.ArchiveJob.Submit(ctx, input, c.Now(), 10000)
		if err != nil {
			return err
		}
		if err := c.Service.Repo.EnrichmentJob.Bind(ctx, result.UUID, c.Now()); err != nil {
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

func (c *EnrichmentCoordinator) Find(ctx context.Context, token, id string) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		_, result, _, err = c.allowed(ctx, token, id)
		return err
	})
	return result, err
}

func (c *EnrichmentCoordinator) Claim(ctx context.Context, token, id string, expected int64, owner, policy, extractor string, duration time.Duration) (*models.ArchiveJob, error) {
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
			return models.ErrEnrichmentConflict
		}
		if current.State == "running" && current.OwnerUUID == owner && current.LeaseUntil != nil && c.Now().Before(*current.LeaseUntil) {
			result, err = c.Service.Repo.EnrichmentJob.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: current.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
		} else {
			result, err = c.Service.Repo.ArchiveJob.ClaimByID(ctx, id, expected, owner, c.Now(), duration)
			if err == nil && result != nil {
				err = c.Service.Repo.EnrichmentJob.BindAttempt(ctx, models.EnrichmentJobLease{ArchiveJobLease: result.Lease(), ProducerUUID: credential.ProducerUUID}, c.Now())
			}
		}
		if err != nil {
			return err
		}
		if result != nil {
			c.guard(ctx, token, result, result.LeaseUntil)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *EnrichmentCoordinator) Renew(ctx context.Context, token string, lease models.ArchiveJobLease, duration time.Duration) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, _, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		before, err := c.Service.Repo.EnrichmentJob.CheckLease(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, c.Now())
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

func (c *EnrichmentCoordinator) Checkpoint(ctx context.Context, token string, lease models.ArchiveJobLease, expected int, body json.RawMessage) (*models.EnrichmentCheckpointReceipt, error) {
	var result *models.EnrichmentCheckpointReceipt
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, before, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.EnrichmentJob.Checkpoint(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, expected, body, c.Now())
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

func (c *EnrichmentCoordinator) CheckpointHead(ctx context.Context, token, id string) (*models.EnrichmentCheckpoint, error) {
	var result *models.EnrichmentCheckpoint
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.EnrichmentJob.CheckpointHead(ctx, id)
		return err
	})
	return result, err
}
