package manager

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/job"
	"github.com/stretchr/testify/require"
)

// Sequential scanning finishes every generation step, including scene covers,
// before the scan moves on. Otherwise steps are queued alongside the scan.
func TestRunScanGeneratorFollowsSequentialScanning(t *testing.T) {
	jobs := job.NewManager()
	jobs.SetSync(true)
	t.Cleanup(jobs.Stop)

	var sequentialRanInline, queuedReturnedEarly, queuedRan atomic.Bool
	_, err := jobs.Add(t.Context(), "scan generation", job.MakeJobExec(func(ctx context.Context, progress *job.Progress) error {
		queue := job.NewTaskQueue(ctx, progress, 10, 1)

		var sequentialRan atomic.Bool
		runScanGenerator(ctx, true, queue, "sequential", func(context.Context) {
			sequentialRan.Store(true)
		})
		sequentialRanInline.Store(sequentialRan.Load())

		release := make(chan struct{})
		runScanGenerator(ctx, false, queue, "queued", func(context.Context) {
			<-release
			queuedRan.Store(true)
		})
		queuedReturnedEarly.Store(!queuedRan.Load())

		close(release)
		queue.Close()
		return nil
	}))
	require.NoError(t, err)

	require.Eventually(t, queuedRan.Load, 5*time.Second, 10*time.Millisecond)
	require.True(t, sequentialRanInline.Load(), "sequential step must finish before returning")
	require.True(t, queuedReturnedEarly.Load(), "non-sequential step must be queued, not run inline")
}
