package manager

import (
	"fmt"
	"sync"

	"github.com/stashapp/stash/pkg/logger"
)

// taskFailures retains a bounded summary while batch tasks continue processing
// independent records. Export workers may report failures concurrently.
type taskFailures struct {
	mu    sync.Mutex
	first error
	count int
}

func (f *taskFailures) addf(format string, args ...any) {
	err := fmt.Errorf(format, args...)
	logger.Error(err.Error())
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.first == nil {
		f.first = err
	}
	f.count++
}

func (f *taskFailures) err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.first == nil {
		return nil
	}
	return fmt.Errorf("%d operation(s) failed; first failure: %w", f.count, f.first)
}
