//go:build linux

package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// readProcState returns the single-letter process state from
// /proc/<pid>/status's "State:" line. 'R' (running), 'S' (sleeping),
// 'T' (stopped via SIGSTOP), etc.
func readProcState(pid int) (byte, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && len(fields[1]) > 0 {
				return fields[1][0], nil
			}
		}
	}
	return 0, fmt.Errorf("State line not found in /proc/%d/status", pid)
}

// waitForState polls the proc state up to `timeout` waiting for it
// to match `want`. Returns the last seen state on failure for
// diagnostics.
func waitForState(pid int, want byte, timeout time.Duration) (byte, bool) {
	deadline := time.Now().Add(timeout)
	var last byte
	for time.Now().Before(deadline) {
		s, err := readProcState(pid)
		if err == nil {
			last = s
			if s == want {
				return s, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return last, false
}

// TestSuspendResume_RealProcess spawns a real child, sends
// SIGSTOP via `suspendProcess`, then SIGCONT via `resumeProcess`,
// verifying the kernel-visible state through /proc. Pinned to
// linux for the /proc dependency; the syscall path is identical on
// macOS/BSD.
func TestSuspendResume_RealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn sleep: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Wait until the child is in 'S' (sleeping in sleep(2)). Right
	// after fork+exec it may still be 'R' or transitioning.
	if state, ok := waitForState(pid, 'S', 2*time.Second); !ok {
		t.Fatalf("child did not reach sleeping state, last state %q", state)
	}

	if err := suspendProcess(cmd.Process); err != nil {
		t.Fatalf("suspendProcess: %v", err)
	}
	if state, ok := waitForState(pid, 'T', 2*time.Second); !ok {
		t.Fatalf("child did not reach stopped state after suspend, last state %q", state)
	}

	if err := resumeProcess(cmd.Process); err != nil {
		t.Fatalf("resumeProcess: %v", err)
	}
	if state, ok := waitForState(pid, 'S', 2*time.Second); !ok {
		t.Fatalf("child did not reach sleeping state after resume, last state %q", state)
	}
}

// TestResumeProcess_IdempotentOnRunningProcess verifies that SIGCONT
// on an already-running process is a safe no-op. checkTranscode
// relies on this when handling a pending segment request — it always
// sends resume without first checking whether the process was
// actually suspended.
func TestResumeProcess_IdempotentOnRunningProcess(t *testing.T) {
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	if err := resumeProcess(cmd.Process); err != nil {
		t.Fatalf("resumeProcess on running process returned error: %v", err)
	}
}
