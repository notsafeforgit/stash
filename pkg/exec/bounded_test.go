package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func boundedTestCommand(ctx context.Context, t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := CommandContext(ctx, executable, "-test.run=^TestBoundedChild$")
	cmd.Env = append(os.Environ(), "STASH_BOUNDED_TEST_CHILD="+mode)
	return cmd
}

func TestBoundedChild(t *testing.T) {
	mode := os.Getenv("STASH_BOUNDED_TEST_CHILD")
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	switch mode {
	case "success":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		fmt.Fprint(os.Stderr, "diagnostic\n")
	case "invalid":
		fmt.Fprint(os.Stdout, "not JSON\n")
		time.Sleep(30 * time.Second)
	case "stdout":
		fmt.Fprint(os.Stdout, strings.Repeat(" ", 4096))
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", 4096))
	case "sleep":
		time.Sleep(30 * time.Second)
	case "exit":
		fmt.Fprint(os.Stdout, "{}")
		os.Exit(7)
	default:
		os.Exit(8)
	}
	os.Exit(0)
}

func TestRunBounded(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want error
	}{
		{"success", nil},
		{"invalid", nil},
		{"stdout", ErrOutputLimit},
		{"stderr", ErrOutputLimit},
		{"sleep", context.DeadlineExceeded},
		{"exit", nil},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			limits := OutputLimits{Timeout: 5 * time.Second, Stdout: 1024, Stderr: 1024}
			if tc.mode == "sleep" {
				limits.Timeout = 100 * time.Millisecond
			}
			var cmd *exec.Cmd
			var stderr strings.Builder
			var output map[string]string
			start := time.Now()
			err := RunBounded(context.Background(), limits, func(ctx context.Context) *exec.Cmd {
				cmd = boundedTestCommand(ctx, t, tc.mode)
				return cmd
			}, strings.NewReader(`{"value":"received input"}`), func(r io.Reader) error {
				return json.NewDecoder(r).Decode(&output)
			}, &stderr)
			if tc.mode == "success" {
				require.NoError(t, err)
				require.Equal(t, map[string]string{"value": "received input"}, output)
				require.Equal(t, "diagnostic\n", stderr.String())
			} else {
				require.Error(t, err)
				require.Less(t, time.Since(start), 4*time.Second, "must stop promptly, not wait for the child's sleep")
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
			if tc.mode == "exit" {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, 7, exit.ExitCode())
			}
			if tc.mode == "invalid" {
				var syntax *json.SyntaxError
				require.ErrorAs(t, err, &syntax)
			}
			require.NotNil(t, cmd.ProcessState, "every started process must be reaped")
			require.ErrorIs(t, cmd.Process.Kill(), os.ErrProcessDone)
			require.LessOrEqual(t, stderr.Len(), int(limits.Stderr))
		})
	}
}

func TestRunBoundedCancellationAndStartFailure(t *testing.T) {
	limits := OutputLimits{Timeout: time.Minute, Stdout: 1024, Stderr: 1024}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunBounded(ctx, limits, func(ctx context.Context) *exec.Cmd {
		return boundedTestCommand(ctx, t, "sleep")
	}, nil, func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }, nil)
	require.ErrorIs(t, err, context.Canceled)

	err = RunBounded(context.Background(), limits, func(ctx context.Context) *exec.Cmd {
		return CommandContext(ctx, t.TempDir()+"/nonexistent-scraper")
	}, nil, func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }, nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, context.DeadlineExceeded))
}
