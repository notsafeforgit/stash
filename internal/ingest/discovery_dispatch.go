package ingest

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

func (c *DiscoveryCoordinator) allowedCollection(ctx context.Context, token, id string) error {
	credential, err := c.Service.authenticate(ctx, token)
	if err != nil {
		return err
	}
	if !ValidUUID(id) {
		return ErrInvalid
	}
	collection, err := c.Service.Repo.SourceCollection.Find(ctx, id)
	if err != nil {
		return err
	}
	if collection == nil {
		return ErrNotFound
	}
	if !permitted(credential, collection.UUID, collection.RootUUID) {
		return ErrForbidden
	}
	return nil
}

// Readiness grants no lease or source access. Admission and claim revalidate
// candidates against the producer's current grants in their write transaction.
func (c *DiscoveryCoordinator) ReadyJobs(ctx context.Context, token, collection, policy, extractor string, after int64, limit int) ([]models.DiscoveryJobCandidate, error) {
	var ret []models.DiscoveryJobCandidate
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if err := c.allowedCollection(ctx, token, collection); err != nil {
			return err
		}
		var err error
		ret, err = c.Service.Repo.DiscoveryJob.Ready(ctx, collection, policy, extractor, after, limit, c.Now())
		return err
	})
	return ret, err
}

func (c *DiscoveryCoordinator) ReadyListings(ctx context.Context, token, collection, policy, extractor, after string, limit int) (*models.DiscoveryListingCandidates, error) {
	var ret *models.DiscoveryListingCandidates
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if err := c.allowedCollection(ctx, token, collection); err != nil {
			return err
		}
		var err error
		ret, err = c.Service.Repo.DiscoveryJob.ReadyListings(ctx, collection, policy, extractor, after, limit, c.Now())
		return err
	})
	return ret, err
}

func (c *DiscoveryCoordinator) ReadyCollections(ctx context.Context, token, after string, limit int) ([]models.DiscoveryCollectionCandidate, error) {
	var ret []models.DiscoveryCollectionCandidate
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		ret, err = c.Service.Repo.DiscoveryJob.Collections(ctx, models.DiscoveryCollectionQuery{
			Scopes: credential.Scopes, Roots: credential.RootUUIDs, After: after, Limit: limit,
		})
		return err
	})
	return ret, err
}
