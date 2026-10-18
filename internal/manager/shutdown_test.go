package manager

import (
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stretchr/testify/require"
)

func TestBeginShutdownKeepsStreamReferenceForActiveHandlers(t *testing.T) {
	streams := ffmpeg.NewStreamManager(t.TempDir(), nil, nil, nil, nil)
	t.Cleanup(streams.Shutdown)
	m := Manager{StreamManager: streams}
	m.BeginShutdown()
	require.Same(t, streams, m.StreamManager)
	m.BeginShutdown()
	require.Same(t, streams, m.StreamManager)
}
