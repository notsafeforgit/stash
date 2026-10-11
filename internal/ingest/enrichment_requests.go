package ingest

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

func (c *EnrichmentCoordinator) RequestSource(ctx context.Context, token string, lease models.ArchiveJobLease, input models.SourceRequestInput) (*models.SourceRequestPermit, error) {
	var result *models.SourceRequestPermit
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, job, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.EnrichmentJob.RequestSource(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, input, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, job, job.LeaseUntil)
		return nil
	})
	return result, err
}

func (c *EnrichmentCoordinator) ReportSourceRequest(ctx context.Context, token string, lease models.ArchiveJobLease, input models.SourceRequestResult) error {
	return c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, job, _, err := c.allowed(ctx, token, lease.JobUUID)
		if err != nil {
			return err
		}
		if err = c.Service.Repo.EnrichmentJob.ReportSourceRequest(ctx, models.EnrichmentJobLease{ArchiveJobLease: lease, ProducerUUID: credential.ProducerUUID}, input, c.Now()); err != nil {
			return err
		}
		c.guard(ctx, token, job, job.LeaseUntil)
		return nil
	})
}
