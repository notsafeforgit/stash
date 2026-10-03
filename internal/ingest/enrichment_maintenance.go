package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// Maintenance changes only native queue ownership. It never claims work,
// contacts websites, discards staged evidence or enables held targets.
type EnrichmentMaintenance struct {
	Service      *Service
	Now          func() time.Time
	PollInterval time.Duration
}

func NewEnrichmentMaintenance(service *Service) *EnrichmentMaintenance {
	return &EnrichmentMaintenance{Service: service, Now: time.Now, PollInterval: 30 * time.Second}
}

func (w *EnrichmentMaintenance) Process(ctx context.Context) (*models.EnrichmentMaintenanceResult, error) {
	if w.Service == nil || w.Now == nil {
		return nil, ErrInvalid
	}
	var ret *models.EnrichmentMaintenanceResult
	err := w.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = w.Service.Repo.EnrichmentJob.Maintain(ctx, w.Now())
		return err
	})
	return ret, err
}

func (w *EnrichmentMaintenance) Run(ctx context.Context) error {
	if w.Service == nil || w.Now == nil || w.PollInterval <= 0 {
		return ErrInvalid
	}
	for {
		if _, err := w.Process(ctx); err != nil {
			return err
		}
		timer := time.NewTimer(w.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
