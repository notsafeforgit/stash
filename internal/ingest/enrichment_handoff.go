package ingest

import (
	"context"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (c *EnrichmentCoordinator) AdmitHandoff(ctx context.Context, token, id, expected, policy, extractor string) (*models.ArchiveJob, error) {
	var result *models.ArchiveJob
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		// Check replay using the job's pinned scope; subsequent collection edits
		// cannot turn a lost admission acknowledgement into a second execution.
		prior, err := c.Service.Repo.EnrichmentJob.HandoffJob(ctx, id)
		if err != nil {
			return err
		}
		if prior != nil {
			var work *models.EnrichmentJobArguments
			_, result, work, err = c.allowed(ctx, token, prior.UUID)
			if err != nil {
				return err
			}
			if work.Handoff == nil || work.Handoff.UUID != id || work.Handoff.PlanSHA256 != expected || work.PolicySHA256 != policy || work.ExtractorVersion != extractor {
				return models.ErrEnrichmentConflict
			}
			c.guard(ctx, token, result, nil)
			return nil
		}
		handoff, err := c.Service.Repo.AutomationCheckpointImport.Handoff(ctx, id)
		if err != nil {
			return err
		}
		if handoff == nil {
			return ErrNotFound
		}
		if !permitted(credential, handoff.Collection.UUID, handoff.Collection.RootUUID) {
			return ErrForbidden
		}
		if !archive.ValidSHA256(expected) || handoff.PlanSHA256 != expected || handoff.Input.PolicySHA256 != policy || handoff.Input.ExtractorVersion != extractor {
			return models.ErrEnrichmentConflict
		}
		result, err = c.Service.Repo.EnrichmentJob.AdmitHandoff(ctx, id, expected, c.Now())
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

func (c *EnrichmentCoordinator) Seed(ctx context.Context, token, id string) (*models.CheckpointHandoffSeed, error) {
	var result *models.CheckpointHandoffSeed
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.EnrichmentJob.Seed(ctx, id)
		return err
	})
	return result, err
}
