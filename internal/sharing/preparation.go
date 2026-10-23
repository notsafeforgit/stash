package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

const PreparationBatchSize = 50

func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run resumes durable preparation after restart. One interrupted batch can be
// repeated; neither a partial grant nor a newly evaluated filter is published.
func (s *Service) Run(ctx context.Context) error {
	for {
		var row *models.ShareRecord
		err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
			var err error
			row, err = s.Repo.Share.NextPreparation(ctx, time.Now().Unix())
			return err
		})
		if err != nil {
			return err
		}
		if row != nil {
			if err := s.PrepareBatch(ctx, row); err != nil {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wake:
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *Service) PrepareBatch(ctx context.Context, row *models.ShareRecord) error {
	if row.PreparationState != "PREPARING" || !Active(row, time.Now()) {
		return nil
	}
	var candidates []models.SharePreparedMedia
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		candidates, err = s.Repo.Share.Preparation(ctx, row.ID, PreparationBatchSize)
		return err
	})
	if err != nil {
		return err
	}
	prepared := []models.ShareMedia{}
	failure := ""
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		var item models.ShareMedia
		if err := json.Unmarshal(candidate.MediaJSON, &item); err != nil {
			return err
		}
		var file models.File
		err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error { var err error; file, _, err = s.currentFile(ctx, item); return err })
		if err != nil && !errors.Is(err, ErrUnavailable) {
			return err
		}
		if err == nil {
			item.Fingerprint, err = Fingerprint(file)
			if err == nil {
				if candidate.ExpectedSize != item.Fingerprint.Size || candidate.ExpectedMtime != time.Unix(0, item.Fingerprint.ModTimeNano).Unix() {
					err = ErrUnavailable
				}
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failure = fmt.Sprintf("Cannot verify %s. Its original file is unavailable or has changed. Restore it and retry, or create a new snapshot.", item.Key)
			break
		}
		prepared = append(prepared, item)
	}
	return s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := s.Repo.Share.CheckpointPreparation(ctx, row, prepared, failure)
		return err
	})
}

func (s *Service) Retry(ctx context.Context, id string) (*models.ShareRecord, error) {
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row, err := s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if !Active(row, time.Now()) {
			return ErrUnavailable
		}
		return s.Repo.Share.RetryPreparation(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	s.Wake()
	return s.Find(ctx, id)
}
