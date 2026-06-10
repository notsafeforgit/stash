package ffmpeg

import (
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// newCheckTranscodeFixture builds a minimal `StreamManager` /
// `v3RunningStream` / `v3TranscodeProcess` triple suitable for driving
// `checkV3Transcode` in isolation. Cleanup is wired so the test
// process spawned for `tp.cmd.Process` is reaped even on failure.
func newCheckTranscodeFixture(t *testing.T) (*StreamManager, *v3RunningStream, *v3TranscodeProcess) {
	t.Helper()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn sleep for fixture process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	tp := &v3TranscodeProcess{
		cmd:   cmd,
		start: 0,
		// progress is per-track; for the lookahead math we just need
		// `maxProgress()` to return something, so seed one track.
		progress: map[Track]int{TrackVideo: -1},
		tracks:   []Track{TrackVideo},
	}
	stream := &v3RunningStream{
		dir:          "fixture",
		outputDir:    t.TempDir(),
		lastAccessed: time.Now(),
		tp:           tp,
	}
	sm := &StreamManager{
		v3RunningStreams: map[string]*v3RunningStream{"fixture": stream},
	}
	return sm, stream, tp
}

// withSuspendStubs installs recording stubs for suspendFn/resumeFn
// and restores the originals on test cleanup. Returns counters that
// the caller can read after each checkTranscode invocation.
func withSuspendStubs(t *testing.T) (suspendCalls, resumeCalls *int) {
	t.Helper()
	origSuspend := suspendFn
	origResume := resumeFn
	var sc, rc int
	var mu sync.Mutex
	suspendFn = func(p *os.Process) error {
		mu.Lock()
		sc++
		mu.Unlock()
		return nil
	}
	resumeFn = func(p *os.Process) error {
		mu.Lock()
		rc++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		suspendFn = origSuspend
		resumeFn = origResume
	})
	return &sc, &rc
}

func TestCheckTranscode_SuspendsWhenLookaheadExceeds(t *testing.T) {
	suspendCalls, _ := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	// Simulate: ffmpeg has produced segments far ahead of what the
	// client has fetched. No waiting segments (client is paused).
	tp.progress[TrackVideo] = v3SuspendLookaheadSegments
	stream.lastSegment = 0

	sm.checkV3Transcode(stream, time.Now())

	if *suspendCalls != 1 {
		t.Fatalf("expected 1 suspend call, got %d", *suspendCalls)
	}
	if !tp.suspended {
		t.Fatal("expected tp.suspended=true after threshold-crossing tick")
	}
}

func TestCheckTranscode_NoSuspendUnderThreshold(t *testing.T) {
	suspendCalls, _ := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	tp.progress[TrackVideo] = v3SuspendLookaheadSegments - 1
	stream.lastSegment = 0

	sm.checkV3Transcode(stream, time.Now())

	if *suspendCalls != 0 {
		t.Fatalf("expected 0 suspend calls at lookahead %d (threshold %d), got %d",
			v3SuspendLookaheadSegments-1, v3SuspendLookaheadSegments, *suspendCalls)
	}
	if tp.suspended {
		t.Fatal("expected tp.suspended=false")
	}
}

func TestCheckTranscode_HysteresisHoldsSuspend(t *testing.T) {
	suspendCalls, resumeCalls := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	// Already suspended; lookahead sits between the resume threshold
	// and the suspend threshold. Should stay suspended (no resume
	// fires, no new suspend either).
	tp.suspended = true
	tp.progress[TrackVideo] = (v3SuspendLookaheadSegments + v3ResumeLookaheadSegments) / 2
	stream.lastSegment = 0

	sm.checkV3Transcode(stream, time.Now())

	if *suspendCalls != 0 {
		t.Errorf("did not expect a suspend call in hysteresis band, got %d", *suspendCalls)
	}
	if *resumeCalls != 0 {
		t.Errorf("did not expect a resume call in hysteresis band, got %d", *resumeCalls)
	}
	if !tp.suspended {
		t.Error("expected tp.suspended to remain true")
	}
}

func TestCheckTranscode_ResumesWhenClientDrains(t *testing.T) {
	_, resumeCalls := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	// Started suspended; client has caught back up to within the
	// resume threshold (typical "user pressed play" recovery).
	tp.suspended = true
	tp.progress[TrackVideo] = v3ResumeLookaheadSegments
	stream.lastSegment = 0

	sm.checkV3Transcode(stream, time.Now())

	if *resumeCalls != 1 {
		t.Fatalf("expected 1 resume call, got %d", *resumeCalls)
	}
	if tp.suspended {
		t.Fatal("expected tp.suspended=false after drain")
	}
}

func TestCheckTranscode_ResumesOnPendingRequest(t *testing.T) {
	_, resumeCalls := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	// Suspended, but a new segment request has arrived (e.g. user
	// scrubbed forward, or hit play after a long pause and hls.js
	// requested the next segment). Resume should fire regardless of
	// lookahead distance — the waiting segment may sit far past the
	// produced range and only ensureTranscode/checkSegments will
	// produce it.
	tp.suspended = true
	tp.progress[TrackVideo] = v3SuspendLookaheadSegments * 2
	stream.lastSegment = 0
	stream.waitingSegments = []*v3WaitingSegment{{
		idx:      v3SuspendLookaheadSegments * 3,
		accessed: time.Now(),
	}}

	sm.checkV3Transcode(stream, time.Now())

	if *resumeCalls != 1 {
		t.Fatalf("expected 1 resume call on pending request, got %d", *resumeCalls)
	}
	if tp.suspended {
		t.Fatal("expected tp.suspended=false after request-triggered resume")
	}
}

func TestCheckTranscode_NoSuspendWhileClientWaiting(t *testing.T) {
	suspendCalls, _ := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	// Lookahead exceeds the threshold AND there's a pending request.
	// Suspend would starve the request — must not fire.
	tp.progress[TrackVideo] = v3SuspendLookaheadSegments
	stream.lastSegment = 0
	stream.waitingSegments = []*v3WaitingSegment{{idx: 1, accessed: time.Now()}}

	sm.checkV3Transcode(stream, time.Now())

	if *suspendCalls != 0 {
		t.Fatalf("expected 0 suspend calls with pending request, got %d", *suspendCalls)
	}
	if tp.suspended {
		t.Fatal("expected tp.suspended=false")
	}
}

func TestStopTranscode_ResumesBeforeCancel(t *testing.T) {
	_, resumeCalls := withSuspendStubs(t)
	sm, stream, tp := newCheckTranscodeFixture(t)

	// Replace tp.cancel with a recording cancel; verify suspend was
	// cleared *before* cancel fired so the context-cancel path can
	// deliver SIGTERM (which SIGSTOP would otherwise queue
	// indefinitely until SIGCONT).
	cancelCalled := false
	tp.cancel = func() { cancelCalled = true }
	tp.suspended = true

	sm.stopV3Transcode(stream)

	if *resumeCalls != 1 {
		t.Fatalf("expected 1 resume call before cancel, got %d", *resumeCalls)
	}
	if !cancelCalled {
		t.Fatal("expected cancel to be called by stopTranscode")
	}
	if tp.suspended {
		t.Fatal("expected tp.suspended=false after stopTranscode")
	}
	if !tp.cancelled {
		t.Fatal("expected tp.cancelled=true after stopTranscode")
	}
}
