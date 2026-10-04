package ingest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// DiscoveryComparisonWorker processes only explicitly bound targets and already
// retained pages. It neither contacts services nor publishes post identities.
// Comparison progress is durable; the in-memory inspection cursor only rotates
// through eligible targets and can restart without replaying committed work.
type DiscoveryComparisonWorker struct {
	Service      *Service
	Now          func() time.Time
	PollInterval time.Duration
	mu           sync.Mutex
	after        string
}

type DiscoveryComparisonProgress struct {
	Receipt *models.DiscoveryMatchReceipt
	HasMore bool
	Blocked bool
}

func NewDiscoveryComparisonWorker(service *Service) *DiscoveryComparisonWorker {
	return &DiscoveryComparisonWorker{Service: service, Now: time.Now, PollInterval: 30 * time.Second}
}

func (w *DiscoveryComparisonWorker) Process(ctx context.Context) (*DiscoveryComparisonProgress, error) {
	if w.Service == nil || w.Now == nil {
		return nil, ErrInvalid
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var ready *models.DiscoveryComparisonCandidates
	err := w.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		ready, err = w.Service.Repo.DiscoveryMatch.Pending(ctx, w.after, 32, w.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	ret := &DiscoveryComparisonProgress{HasMore: ready.HasMore}
	if len(ready.Targets) == 0 {
		w.after = ready.After
		if !ready.HasMore {
			// Expose the idle boundary before wrapping. An immediate retry
			// from the start would keep a large set of waiting targets busy.
			w.after = ""
		}
		return ret, nil
	}
	selected := ready.Targets[0]
	// Advance inspection even when this target changes before commit, so one
	// stale target cannot starve the rest. Its database progress remains intact.
	w.after = selected.UUID
	ret.HasMore = ready.HasMore || len(ready.Targets) > 1
	if !ret.HasMore {
		w.after = ""
	}
	var prepared models.PreparedDiscoveryComparison
	err = w.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		prepared, err = w.Service.Repo.DiscoveryMatch.Prepare(ctx, selected.UUID, selected.AfterPage, w.Now())
		return err
	})
	if err == nil && prepared != nil {
		err = w.Service.Repo.WithTxn(ctx, func(ctx context.Context) error {
			var err error
			ret.Receipt, err = prepared.Commit(ctx, w.Now())
			return err
		})
	}
	if errors.Is(err, models.ErrDiscoveryConflict) {
		return ret, nil // another worker or native edit won; inspect other work
	}
	if errors.Is(err, models.ErrArchiveJobCapacity) {
		ret.Receipt, ret.Blocked = nil, true
		return ret, nil // keep the uncompared page and back off without truncation
	}
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (w *DiscoveryComparisonWorker) Run(ctx context.Context) error {
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
		if !result.Blocked && (result.Receipt != nil || result.HasMore) {
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
