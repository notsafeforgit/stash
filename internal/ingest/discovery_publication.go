package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// PublishDiscoveryMatch is application-authorized work over retained evidence.
// It makes no website request and grants no producer permission. Preparation
// decodes source data under a read transaction; publication rechecks choices and
// commits the verified identity, shared capture effects and receipt together.
func (s *Service) PublishDiscoveryMatch(ctx context.Context, input models.DiscoveryPublicationInput) (*models.DiscoveryMatchPublication, error) {
	var prepared models.PreparedDiscoveryPublication
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		prepared, err = s.Repo.DiscoveryMatch.PreparePublication(ctx, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	var result *models.DiscoveryMatchPublication
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		now := time.Now()
		var err error
		result, err = prepared.Publish(ctx, func(ctx context.Context, capture models.SourceCaptureInput, collection *models.SourceCollection) error {
			raw, err := archive.RestoreCapture(&capture.Payload)
			if err != nil {
				return err
			}
			album, err := archive.ExtractCapturedAlbum(raw)
			if err != nil {
				return err
			}
			_, _, err = s.recordPreparedCapture(ctx, capture, album, collection, now)
			return err
		}, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
