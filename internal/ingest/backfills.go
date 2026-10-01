package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// Account-wide history includes several collections and permanent pre-native
// decisions. Access requires an explicit root grant; a single collection grant
// does not reveal other accounts' or collections' backfill history.
func (s *Service) backfillAuthority(ctx context.Context, token, root string) (*models.IngestCredential, error) {
	credential, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if !permittedRoot(credential, root) {
		return nil, ErrForbidden
	}
	return credential, nil
}

func (s *Service) BackfillStatus(ctx context.Context, token string, subject models.BackfillSubject, component string) (*models.BackfillStatus, error) {
	var result *models.BackfillStatus
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, err := s.backfillAuthority(ctx, token, subject.RootUUID); err != nil {
			return err
		}
		var err error
		result, err = s.Repo.SourceBackfill.Status(ctx, subject, component)
		return err
	})
	return result, err
}

func (s *Service) CompleteBackfill(ctx context.Context, token string, input models.BackfillCompletion) (*models.BackfillDecision, error) {
	var result *models.BackfillDecision
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := s.backfillAuthority(ctx, token, input.RootUUID)
		if err != nil {
			return err
		}
		result, err = s.Repo.SourceBackfill.Complete(ctx, credential.ProducerUUID, input, time.Now())
		if err != nil {
			return err
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			_, err := s.backfillAuthority(ctx, token, input.RootUUID)
			return err
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
