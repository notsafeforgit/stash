//go:build !windows

package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBoundedDescendantChild(t *testing.T) {
	mode := os.Getenv("STASH_BOUNDED_DESCENDANT")
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	marker := os.Getenv("STASH_BOUNDED_DESCENDANT_MARKER")
	if mode == "heartbeat" {
		for i := range 1500 {
			if err := os.WriteFile(marker, []byte(fmt.Sprint(i)), 0600); err != nil {
				os.Exit(2)
			}
			time.Sleep(20 * time.Millisecond)
		}
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(3)
	}
	child := exec.Command(executable, "-test.run=^TestBoundedDescendantChild$")
	child.Env = append(os.Environ(), "STASH_BOUNDED_DESCENDANT=heartbeat")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(4)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(5)
		}
		time.Sleep(time.Millisecond)
	}
	if mode == "invalid" {
		fmt.Print("not JSON")
		time.Sleep(30 * time.Second)
	} else {
		fmt.Print("{}")
	}
	os.Exit(0) // The descendant deliberately retains both output pipes.
}

func TestRunBoundedStopsDescendants(t *testing.T) {
	for _, mode := range []string{"invalid", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "heartbeat")
			executable, err := os.Executable()
			require.NoError(t, err)
			limits := OutputLimits{Timeout: 5 * time.Second, Stdout: 1024, Stderr: 1024}
			var cmd *exec.Cmd
			err = RunBounded(context.Background(), limits, func(ctx context.Context) *exec.Cmd {
				cmd = CommandContext(ctx, executable, "-test.run=^TestBoundedDescendantChild$")
				cmd.Env = append(os.Environ(), "STASH_BOUNDED_DESCENDANT="+mode, "STASH_BOUNDED_DESCENDANT_MARKER="+marker)
				return cmd
			}, strings.NewReader(""), func(r io.Reader) error {
				var value any
				return json.NewDecoder(r).Decode(&value)
			}, nil)
			// Cleanup also runs if a regression leaves the descendant alive.
			t.Cleanup(func() { _ = cmd.Cancel() })
			require.Error(t, err)
			if mode == "orphan" {
				require.ErrorIs(t, err, exec.ErrWaitDelay)
			}
			require.NotNil(t, cmd.ProcessState)
			// The child had started and was writing before its parent emitted
			// output. It must no longer do work after the call has returned.
			before, err := os.ReadFile(marker)
			require.NoError(t, err)
			time.Sleep(150 * time.Millisecond)
			after, err := os.ReadFile(marker)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
		})
	}
}
