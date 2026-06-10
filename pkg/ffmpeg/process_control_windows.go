//go:build windows

package ffmpeg

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Windows has no SIGSTOP/SIGCONT. The functional equivalents are
// NtSuspendProcess / NtResumeProcess — undocumented but stable NTAPI
// calls in ntdll.dll, used by Process Explorer and every other tool
// that suspends processes on Windows. Resolved lazily so this file
// compiles even on systems that pre-date one of them (none in
// practice — both have existed since NT 4.0, but lazy resolution is
// the idiomatic pattern for ntdll calls anyway).
var (
	ntdll              = windows.NewLazySystemDLL("ntdll.dll")
	procNtSuspendProc  = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProc   = ntdll.NewProc("NtResumeProcess")
)

func processHandle(p *os.Process) (windows.Handle, error) {
	// `os.Process.Pid` is the raw PID; we need a HANDLE with
	// PROCESS_SUSPEND_RESUME (0x0800) access to call the NT APIs.
	const access = uint32(0x0800)
	return windows.OpenProcess(access, false, uint32(p.Pid))
}

// suspendProcess freezes a running process in place via
// NtSuspendProcess. See the Unix variant for the rationale (no
// restart, in-flight encoder state preserved).
func suspendProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	h, err := processHandle(p)
	if err != nil {
		return fmt.Errorf("openprocess for suspend: %w", err)
	}
	defer windows.CloseHandle(h)
	r, _, err := procNtSuspendProc.Call(uintptr(h))
	if r != 0 {
		return fmt.Errorf("NtSuspendProcess: %w", err)
	}
	return nil
}

// resumeProcess wakes a suspended process via NtResumeProcess. Safe
// to call on a process that wasn't suspended.
func resumeProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	h, err := processHandle(p)
	if err != nil {
		return fmt.Errorf("openprocess for resume: %w", err)
	}
	defer windows.CloseHandle(h)
	r, _, err := procNtResumeProc.Call(uintptr(h))
	if r != 0 {
		return fmt.Errorf("NtResumeProcess: %w", err)
	}
	return nil
}
