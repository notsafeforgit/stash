package ingest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// DiscoveryPublicationWorker publishes only explicitly bound, completely
// compared matches corroborated by listing or authenticated detail evidence.
// It uses the same atomic service as application
// publication and never fetches, assigns performers or releases source pages.
type DiscoveryPublicationWorker struct {
	Service      *Service
	Now          func() time.Time
	PollInterval time.Duration
	mu           sync.Mutex
	after        string
}

type DiscoveryPublicationProgress struct {
	Publication *models.DiscoveryMatchPublication
	HasMore     bool
	Blocked     bool
}

func NewDiscoveryPublicationWorker(service *Service) *DiscoveryPublicationWorker {
	return &DiscoveryPublicationWorker{Service: service, Now: time.Now, PollInterval: 30 * time.Second}
}

func (w *DiscoveryPublicationWorker) Process(ctx context.Context) (*DiscoveryPublicationProgress, error) {
	if w.Service == nil || w.Now == nil {
		return nil, ErrInvalid
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var ready *models.DiscoveryPublicationCandidates
	err := w.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		ready, err = w.Service.Repo.DiscoveryMatch.PendingPublications(ctx, w.after, 32, w.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	ret := &DiscoveryPublicationProgress{HasMore: ready.HasMore}
	if len(ready.Targets) == 0 {
		w.after = ready.After
		if !ready.HasMore {
			// Finish this traversal before wrapping so waiting and review
			// targets cannot prevent the idle pause.
			w.after = ""
		}
		return ret, nil
	}
	selected := ready.Targets[0]
	// Advance even if native choices change before publication. Its original
	// evidence stays available and cannot starve other targets in this traversal.
	w.after = selected.TargetUUID
	ret.HasMore = ready.HasMore || len(ready.Targets) > 1
	if !ret.HasMore {
		w.after = ""
	}
	ret.Publication, err = w.Service.publishDiscoveryMatch(ctx, selected, w.Now)
	if errors.Is(err, models.ErrDiscoveryConflict) {
		return ret, nil
	}
	if errors.Is(err, models.ErrArchiveJobCapacity) {
		ret.Publication, ret.Blocked = nil, true
		return ret, nil
	}
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (w *DiscoveryPublicationWorker) Run(ctx context.Context) error {
	if w.Service == nil || w.Now == nil || w.PollInterval <= 0 {
		return ErrInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := w.Process(ctx)
		if err != nil {
			return err
		}
		delay := w.PollInterval
		if !result.Blocked && (result.Publication != nil || result.HasMore) {
			delay = 20 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
