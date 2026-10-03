package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// RunCoordinator shares native run ownership across producer installations.
// Site credentials and process execution remain with the external worker.
type RunCoordinator struct {
	Service   *Service
	Now       func() time.Time
	MaxActive int
}

func NewRunCoordinator(service *Service) *RunCoordinator {
	return &RunCoordinator{Service: service, Now: time.Now, MaxActive: 10000}
}

func (c *RunCoordinator) allowed(ctx context.Context, token, id string) (*models.IngestCredential, *models.SourceRun, error) {
	credential, err := c.Service.authenticate(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	r, err := c.Service.Repo.SourceRun.Find(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if r == nil {
		return nil, nil, ErrNotFound
	}
	if !permitted(credential, r.CollectionUUID, r.RootUUID) {
		return nil, nil, ErrForbidden
	}
	return credential, r, nil
}

func (c *RunCoordinator) guard(ctx context.Context, token string, result *models.SourceRun, deadline *time.Time) {
	if result == nil {
		return
	}
	revision, state, fence := result.Revision, result.State, result.Fence
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if deadline != nil && !c.Now().Before(*deadline) {
			return models.ErrSourceRunLease
		}
		_, r, err := c.allowed(ctx, token, result.UUID)
		if err != nil {
			return err
		}
		if r.Revision != revision || r.State != state || r.Fence != fence {
			return models.ErrSourceRunConflict
		}
		return nil
	})
}

func (c *RunCoordinator) Submit(ctx context.Context, token string, input models.SourceRunRequest) (*models.SourceRun, error) {
	var result *models.SourceRun
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		hasCollection := false
		for _, scope := range credential.Scopes {
			if scope.CollectionUUID == input.CollectionUUID {
				hasCollection = true
				break
			}
		}
		// Admission replay may identify a historical root after a collection
		// moves. Check the returned immutable admission inside this transaction
		// rather than rejecting it against only today's collection definition.
		if !hasCollection && len(credential.RootUUIDs) == 0 {
			return ErrForbidden
		}
		result, err = c.Service.Repo.SourceRun.Submit(ctx, credential.ProducerUUID, input, c.Now(), c.MaxActive)
		if err != nil {
			return err
		}
		if !permitted(credential, result.CollectionUUID, result.RootUUID) {
			return ErrForbidden
		}
		c.guard(ctx, token, result, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *RunCoordinator) Find(ctx context.Context, token, id string) (*models.SourceRun, error) {
	var result *models.SourceRun
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error { var err error; _, result, err = c.allowed(ctx, token, id); return err })
	return result, err
}

func (c *RunCoordinator) List(ctx context.Context, token, collection string, after int64) ([]models.SourceRun, error) {
	result := make([]models.SourceRun, 0)
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		roots := make([]*string, 0, len(credential.RootUUIDs)+1)
		for i := range credential.RootUUIDs {
			roots = append(roots, &credential.RootUUIDs[i])
		}
		for _, scope := range credential.Scopes {
			if scope.CollectionUUID == collection {
				roots = append(roots, scope.RootUUID)
				break
			}
		}
		if len(roots) == 0 {
			return ErrForbidden
		}
		rows, err := c.Service.Repo.SourceRun.List(ctx, collection, roots, after, 50)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !permitted(credential, r.CollectionUUID, r.RootUUID) {
				return ErrForbidden
			}
			result = append(result, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Ready discovers only download work permitted at this worker's root. It does
// not grant a lease. A root grant includes every registered collection there.
func (c *RunCoordinator) Ready(ctx context.Context, token, root, policy string, after int64) ([]models.SourceRunCandidate, error) {
	var result []models.SourceRunCandidate
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := c.Service.authenticate(ctx, token)
		if err != nil {
			return err
		}
		collections := make([]string, 0, len(credential.Scopes))
		for _, scope := range credential.Scopes {
			if scope.RootUUID != nil && *scope.RootUUID == root {
				collections = append(collections, scope.CollectionUUID)
			}
		}
		allCollections := permittedRoot(credential, root)
		if len(collections) == 0 && !allCollections {
			return ErrForbidden
		}
		result, err = c.Service.Repo.SourceRun.Ready(ctx, collections, allCollections, root, policy, after, 50, c.Now())
		return err
	})
	return result, err
}

func (c *RunCoordinator) Attempts(ctx context.Context, token, id string, after int64) ([]models.SourceRunAttempt, error) {
	var result []models.SourceRunAttempt
	err := c.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, _, err := c.allowed(ctx, token, id); err != nil {
			return err
		}
		var err error
		result, err = c.Service.Repo.SourceRun.Attempts(ctx, id, after, 50)
		return err
	})
	return result, err
}

func (c *RunCoordinator) Claim(ctx context.Context, token, id, owner, policy string, duration time.Duration) (*models.SourceRun, error) {
	var result *models.SourceRun
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, _, err := c.allowed(ctx, token, id)
		if err != nil {
			return err
		}
		result, err = c.Service.Repo.SourceRun.Claim(ctx, id, credential.ProducerUUID, owner, policy, c.Now(), duration)
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

func (c *RunCoordinator) owned(ctx context.Context, token string, lease models.SourceRunLease, apply func(context.Context, models.SourceRunLease) (*models.SourceRun, error)) (*models.SourceRun, error) {
	var result *models.SourceRun
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, _, err := c.allowed(ctx, token, lease.RunUUID)
		if err != nil {
			return err
		}
		lease.ProducerUUID = credential.ProducerUUID
		before, err := c.Service.Repo.SourceRun.CheckLease(ctx, lease, c.Now())
		if err != nil {
			return err
		}
		result, err = apply(ctx, lease)
		if err != nil {
			return err
		}
		c.guard(ctx, token, result, before.LeaseUntil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *RunCoordinator) Renew(ctx context.Context, token string, lease models.SourceRunLease, duration time.Duration) (*models.SourceRun, error) {
	return c.owned(ctx, token, lease, func(ctx context.Context, lease models.SourceRunLease) (*models.SourceRun, error) {
		return c.Service.Repo.SourceRun.Renew(ctx, lease, c.Now(), duration)
	})
}

func (c *RunCoordinator) ReserveSource(ctx context.Context, token string, lease models.SourceRunLease, url string) (*models.SourceRunServiceReservation, error) {
	var result *models.SourceRunServiceReservation
	err := c.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, current, err := c.allowed(ctx, token, lease.RunUUID)
		if err != nil {
			return err
		}
		lease.ProducerUUID = credential.ProducerUUID
		result, err = c.Service.Repo.SourceRun.ReserveSource(ctx, lease, url, c.Now())
		if err != nil {
			return err
		}
		c.guard(ctx, token, current, current.LeaseUntil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (c *RunCoordinator) Progress(ctx context.Context, token string, lease models.SourceRunLease, progress models.SourceRunProgress) (*models.SourceRun, error) {
	return c.owned(ctx, token, lease, func(ctx context.Context, lease models.SourceRunLease) (*models.SourceRun, error) {
		return c.Service.Repo.SourceRun.Progress(ctx, lease, progress, c.Now())
	})
}
func (c *RunCoordinator) Finish(ctx context.Context, token string, lease models.SourceRunLease, outcome models.SourceRunOutcome) (*models.SourceRun, error) {
	return c.owned(ctx, token, lease, func(ctx context.Context, lease models.SourceRunLease) (*models.SourceRun, error) {
		return c.Service.Repo.SourceRun.Finish(ctx, lease, outcome, c.Now())
	})
}
