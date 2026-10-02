package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

// The HTTP server owns this worker's lifetime. main shuts the server down before
// the manager/database. Cancellation waits for the active attempt to stop;
// committed phase checkpoints remain available to the next process.
type archiveWorkerRuntime struct {
	worker interface{ Run(context.Context) error }
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	closed bool
}

func (r *archiveWorkerRuntime) start() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil || r.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	go func() {
		defer close(r.done)
		for ctx.Err() == nil {
			err := r.worker.Run(ctx)
			if ctx.Err() != nil {
				return
			}
			if !errors.Is(err, models.ErrArchiveJobLease) && !errors.Is(err, models.ErrArchiveJobConflict) {
				// Persisted attempts have machine-readable failure codes. Raw
				// arguments, paths and plugin output do not belong in this log.
				logger.Warn("Native archive worker interrupted; retrying")
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (r *archiveWorkerRuntime) stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
