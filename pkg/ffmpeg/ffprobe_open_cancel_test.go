//go:build !windows

package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenedProbeCancelsRunningSubprocess(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep unavailable")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ffprobe")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 10\n"), 0700))
	path := filepath.Join(dir, "file.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0600))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	probe := &FFProbe{path: script}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = probe.NewVideoFileFromOpen(ctx, f, path)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 3*time.Second)
}
