package ingest

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// DiscoveryMaintenance only ends stale or expired ownership. Source access and
// new listing admission remain explicitly scoped producer operations.
type DiscoveryMaintenance struct {
	Service      *Service
	Now          func() time.Time
	PollInterval time.Duration
}

func NewDiscoveryMaintenance(service *Service) *DiscoveryMaintenance {
	return &DiscoveryMaintenance{Service: service, Now: time.Now, PollInterval: 30 * time.Second}
}

func (w *DiscoveryMaintenance) Process(ctx context.Context) (*models.DiscoveryMaintenanceResult, error) {
	if w.Service == nil || w.Now == nil {
		return nil, ErrInvalid
	}
	var ret *models.DiscoveryMaintenanceResult
	err := w.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = w.Service.Repo.DiscoveryJob.Maintain(ctx, w.Now())
		if err != nil {
			return err
		}
		details, err := w.Service.Repo.DiscoveryDetail.Maintain(ctx, w.Now())
		if err != nil {
			return err
		}
		ret.Recovered += details.Recovered
		ret.Cancelled += details.Cancelled
		return nil
	})
	return ret, err
}

func (w *DiscoveryMaintenance) Run(ctx context.Context) error {
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
