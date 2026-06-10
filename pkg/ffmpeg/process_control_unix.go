//go:build !windows

package ffmpeg

import (
	"os"
	"syscall"
)

// suspendProcess freezes a running process in place via SIGSTOP. The
// process retains all its memory, open file descriptors, and (for
// ffmpeg) any in-flight hardware encoder state — it just stops being
// scheduled until SIGCONT arrives. No restart, no output, no audio
// priming discontinuity on resume.
func suspendProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGSTOP)
}

// resumeProcess wakes a SIGSTOPped process via SIGCONT. Safe to call
// on a process that wasn't suspended (SIGCONT is a no-op then). Must
// be called before sending termination signals — SIGTERM is queued
// while STOPped and delivered on CONT, so a suspended process won't
// shut down cleanly otherwise. SIGKILL bypasses this (and gets sent
// by the context-cancel path), but we want SIGTERM-style cleanup to
// stay available too.
func resumeProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGCONT)
}
