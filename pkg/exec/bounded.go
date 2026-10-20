package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// OutputLimits bounds a subprocess's runtime and total output, including output
// that the consumer discards. All limits must be positive.
type OutputLimits struct {
	Timeout time.Duration
	Stdout  int64
	Stderr  int64
}

// ErrOutputLimit identifies a subprocess that exceeded its output allowance.
var ErrOutputLimit = errors.New("subprocess output limit exceeded")

// RunBounded owns a command and its I/O until it has been reaped. newCommand must
// use its supplied context (usually via CommandContext). consume may reject
// stdout early; rejection stops the command before this function returns.
//
// input and stderr must not block independently of the process. In particular,
// use an in-memory input rather than a live network stream. consume must return
// when its reader is closed, and must not retain that reader.
func RunBounded(ctx context.Context, limits OutputLimits, newCommand func(context.Context) *exec.Cmd, input io.Reader, consume func(io.Reader) error, stderr io.Writer) error {
	if limits.Timeout <= 0 || limits.Stdout <= 0 || limits.Stderr <= 0 {
		return errors.New("subprocess limits must be positive")
	}
	bounded, timeout := context.WithTimeout(ctx, limits.Timeout)
	defer timeout()
	running, cancel := context.WithCancelCause(bounded)
	defer cancel(nil)

	cmd := newCommand(running)
	if cmd == nil || cmd.Cancel == nil {
		return errors.New("bounded subprocess requires a context-aware command")
	}
	prepareBoundedCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stdin = input

	reader, writer := io.Pipe()
	defer reader.Close()
	cmd.Stdout = &outputLimitWriter{dst: writer, remaining: limits.Stdout, stream: "stdout", cancel: cancel}
	if stderr == nil {
		stderr = io.Discard
	}
	cmd.Stderr = &outputLimitWriter{dst: stderr, remaining: limits.Stderr, stream: "stderr", cancel: cancel}

	done := make(chan error, 1)
	go func() {
		err := cmd.Run()
		// WaitDelay also bounds descendants that inherit an output descriptor
		// after the direct child exits. Stop the remaining process group.
		if errors.Is(err, exec.ErrWaitDelay) {
			_ = cmd.Cancel()
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()

	outputErr := consume(reader)
	if outputErr == nil {
		// Even consumers that need only one value must account for all output.
		_, outputErr = io.Copy(io.Discard, reader)
	}
	if outputErr != nil {
		cancel(outputErr)
	}
	_ = reader.Close()
	runErr := <-done
	// Keep the original timeout, limit or decoding failure, not just the
	// consequent "signal: killed" or closed-pipe error.
	return errors.Join(context.Cause(running), outputErr, runErr)
}

type outputLimitWriter struct {
	dst       io.Writer
	remaining int64
	stream    string
	cancel    context.CancelCauseFunc
}

func (w *outputLimitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		err := fmt.Errorf("%w (%s)", ErrOutputLimit, w.stream)
		w.cancel(err)
		return 0, err
	}
	n, err := w.dst.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		w.cancel(err)
	}
	return n, err
}
