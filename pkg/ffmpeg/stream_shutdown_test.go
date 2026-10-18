package ffmpeg

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamShutdownDeadlineAndRetry(t *testing.T) {
	sm := NewStreamManager(t.TempDir(), nil, nil, nil, nil)
	// A stalled stream operation must not block cancellation or consume an
	// unbounded amount of the process shutdown deadline.
	sm.streamsMutex.Lock()
	locked := true
	defer func() {
		if locked {
			sm.streamsMutex.Unlock()
		}
		sm.Shutdown()
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, sm.ShutdownContext(ctx), context.DeadlineExceeded)
	require.ErrorIs(t, sm.context.Err(), context.Canceled)
	require.NotNil(t, sm.runningStreams, "cleanup cannot finish while a handler holds the lock")
	sm.streamsMutex.Unlock()
	locked = false
	deadline, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	require.NoError(t, sm.ShutdownContext(deadline))
	require.Nil(t, sm.runningStreams)
	require.Nil(t, sm.v3RunningStreams)
	require.NoError(t, sm.ShutdownContext(deadline))
}
