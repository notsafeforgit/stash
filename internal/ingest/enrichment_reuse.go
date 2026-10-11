package ingest

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

func (c *EnrichmentCoordinator) ReuseCaptured(ctx context.Context, token, id string, expected int) (*models.EnrichmentCompletion, error) {
	var result *models.EnrichmentCompletion
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		target, err := c.Service.Repo.EnrichmentWork.Target(ctx, id)
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
		if collection == nil || !permitted(credential, collection.UUID, collection.RootUUID) {
			return ErrForbidden
		}
		result, err = c.Service.Repo.EnrichmentWork.ReuseCaptured(ctx, id, expected, c.Now())
		return err
	})
	return result, err
}
