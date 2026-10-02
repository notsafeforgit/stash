//go:build linux

package translation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBingProviderCancellationKillsDescendants(t *testing.T) {
	root := t.TempDir()
	executable, pidPath := filepath.Join(root, "provider"), filepath.Join(root, "child.pid")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nsleep 30 &\nchild=$!\nprintf '%s\\n' \"$child\" > \"$STASH_TEST_TRANSLATION_CHILD\"\nwait\n"), 0700))
	t.Setenv("STASH_TEST_TRANSLATION_CHILD", pidPath)
	provider, err := NewBingTranslateShell(executable)
	require.NoError(t, err)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := provider.Translate(ctx, bingRequest("Cancelled fixture")); done <- err }()
	pid := 0
	require.Eventually(t, func() bool {
		body, err := os.ReadFile(pidPath)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(body)))
		return err == nil && pid > 0
	}, 3*time.Second, 10*time.Millisecond)
	stop()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("provider process did not stop after cancellation")
	}
	require.Eventually(t, func() bool {
		body, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if os.IsNotExist(err) {
			return true
		}
		if err != nil {
			return false
		}
		end := bytes.LastIndex(body, []byte(") "))
		return end >= 0 && len(body) > end+2 && body[end+2] == 'Z'
	}, 3*time.Second, 10*time.Millisecond, "the provider's child must be killed, not just its shell")
}
