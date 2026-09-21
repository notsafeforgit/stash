package manager

import (
	"fmt"
	"sync"
)

// Keep the job result bounded even for a library-wide backfill. Individual
// failures (including the scene ID) are already emitted to the task log.
type coverGenerationFailures struct {
	mu    sync.Mutex
	count int
	first error
}

func (r *coverGenerationFailures) Add(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	if r.first == nil {
		r.first = err
	}
}

func (r *coverGenerationFailures) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		return nil
	}
	return fmt.Errorf("%d scene covers could not be regenerated; existing covers were kept. Check the task log for scene IDs. First error: %w", r.count, r.first)
}
