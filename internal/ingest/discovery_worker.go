package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type DiscoveryJobDescription struct {
	Job     *models.ArchiveJob           `json:"job"`
	Listing *models.DiscoveryListing     `json:"listing"`
	Cursor  map[string]string            `json:"cursor"`
	Receipt *models.DiscoveryPageReceipt `json:"receipt"`
}

// Describe returns the exact requested page's input even if later jobs have
// advanced the listing. Large page bodies have a separate explicit read path.
func (c *DiscoveryCoordinator) Describe(ctx context.Context, token, id string) (*DiscoveryJobDescription, error) {
	var result *DiscoveryJobDescription
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		_, job, listing, err := c.allowed(ctx, token, id)
		if err != nil {
			return err
		}
		work, err := archive.DecodeDiscoveryJob(job)
		if err != nil {
			return err
		}
		result = &DiscoveryJobDescription{Job: job, Listing: listing, Cursor: listing.InitialCursor}
		if work.PageOrdinal > 1 {
			previous, err := c.Service.Repo.DiscoveryJob.Page(ctx, listing.UUID, work.PageOrdinal-1)
			if err != nil {
				return err
			}
			if previous == nil || previous.Complete {
				return models.ErrDiscoveryAtomic
			}
			page, err := archive.ParseDiscoveryPage(previous.Body)
			if err != nil {
				return err
			}
			result.Cursor = page.NextCursor
		}
		page, err := c.Service.Repo.DiscoveryJob.Page(ctx, listing.UUID, work.PageOrdinal)
		if err != nil {
			return err
		}
		// An explicit retry can complete this ordinal in a different job.
		// Its receipt cannot acknowledge the failed predecessor's attempt.
		if page != nil && page.JobUUID == job.UUID {
			result.Receipt = &page.DiscoveryPageReceipt
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type DiscoveryFailureReceipt struct {
	models.ArchiveJobAttempt
	ProducerUUID string `json:"producer_uuid"`
}

func discoveryFailureState(code string) string {
	switch code {
	case "rate_limited", "timeout", "extraction_failed", "worker_failed", "source_busy":
		return "retry"
	case "authentication", "access_denied", "challenge", "not_found", "unsupported_extractor", "result_too_large", "invalid_checkpoint", "not_a_post_url", "runtime_changed", "unsupported_profile", "pagination_stalled":
		return "failed"
	default:
		return ""
	}
}

func (c *DiscoveryCoordinator) Fail(ctx context.Context, token string, lease models.ArchiveJobLease, code string) (*DiscoveryFailureReceipt, error) {
	state := discoveryFailureState(code)
	if state == "" || !ValidUUID(lease.JobUUID) || !ValidUUID(lease.OwnerUUID) || lease.Fence < 1 {
		return nil, ErrInvalid
	}
	var result *DiscoveryFailureReceipt
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		bound, err := c.Service.Repo.DiscoveryJob.Attempt(ctx, lease.JobUUID, lease.Fence)
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
			result = &DiscoveryFailureReceipt{ArchiveJobAttempt: attempts[0], ProducerUUID: credential.ProducerUUID}
			c.guard(ctx, token, current, nil)
			return nil
		}
		before, err := c.Service.Repo.DiscoveryJob.CheckLease(ctx, models.DiscoveryJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, c.Now())
		if err != nil {
			return err
		}
		outcome := models.ArchiveJobOutcome{State: state, ErrorCode: code, Result: []byte(`{}`)}
		if state == "retry" {
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
		result = &DiscoveryFailureReceipt{ArchiveJobAttempt: attempts[0], ProducerUUID: credential.ProducerUUID}
		c.guard(ctx, token, after, before.LeaseUntil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
