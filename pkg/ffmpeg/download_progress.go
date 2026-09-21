package ffmpeg

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DownloadProgressState string

const (
	DownloadProcessing DownloadProgressState = "processing"
	DownloadFinished   DownloadProgressState = "finished"
	DownloadFailed     DownloadProgressState = "failed"
)

// DownloadProgress describes server work, not completion of the client's file
// write. The browser owns the final transition to a saved offline scene.
type DownloadProgress struct {
	RequestID        string                `json:"request_id"`
	State            DownloadProgressState `json:"state"`
	ProcessedSeconds float64               `json:"processed_seconds"`
	DurationSeconds  float64               `json:"duration_seconds"`
}

type downloadProgressKey struct {
	sceneID   int
	requestID string
}

type trackedDownload struct {
	DownloadProgress
	finishedAt time.Time
}

// Progress is transient and bounded; it never changes the library database.
// Terminal entries briefly survive EOF so clients can check encoder failures.
type downloadProgressStore struct {
	mu      sync.Mutex
	entries map[downloadProgressKey]*trackedDownload
}

const downloadProgressRetention = 5 * time.Minute
const downloadProgressHistory = 128

func ValidDownloadRequestID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func (s *downloadProgressStore) prune(now time.Time) {
	var oldest downloadProgressKey
	var oldestTime time.Time
	completed := 0
	for key, entry := range s.entries {
		if entry.finishedAt.IsZero() {
			continue
		}
		if now.Sub(entry.finishedAt) >= downloadProgressRetention {
			delete(s.entries, key)
			continue
		}
		completed++
		if oldestTime.IsZero() || entry.finishedAt.Before(oldestTime) {
			oldest, oldestTime = key, entry.finishedAt
		}
	}
	if completed >= downloadProgressHistory {
		delete(s.entries, oldest)
	}
}

func (s *downloadProgressStore) begin(sceneID int, requestID string, duration float64) (*trackedDownload, bool) {
	if sceneID <= 0 || requestID == "" {
		return nil, true // Existing download clients need no progress token.
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(time.Now())
	key := downloadProgressKey{sceneID, requestID}
	if previous := s.entries[key]; previous != nil && previous.State == DownloadProcessing {
		return nil, false
	}
	if !finiteNonnegative(duration) {
		duration = 0
	}
	entry := &trackedDownload{DownloadProgress: DownloadProgress{
		RequestID: requestID, State: DownloadProcessing, DurationSeconds: duration,
	}}
	if s.entries == nil {
		s.entries = make(map[downloadProgressKey]*trackedDownload)
	}
	s.entries[key] = entry
	return entry, true
}

func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *downloadProgressStore) advance(entry *trackedDownload, seconds float64) {
	if entry == nil || !finiteNonnegative(seconds) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.State != DownloadProcessing {
		return
	}
	if entry.DurationSeconds > 0 {
		seconds = math.Min(seconds, entry.DurationSeconds)
	}
	entry.ProcessedSeconds = math.Max(entry.ProcessedSeconds, seconds)
}

func (s *downloadProgressStore) finish(entry *trackedDownload, succeeded bool) {
	if entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.State = DownloadFailed
	if succeeded {
		entry.State = DownloadFinished
		entry.ProcessedSeconds = math.Max(entry.ProcessedSeconds, entry.DurationSeconds)
	}
	entry.finishedAt = time.Now()
	s.prune(entry.finishedAt)
}

func (sm *StreamManager) GetDownloadProgress(sceneID int, requestID string) *DownloadProgress {
	s := &sm.downloadProgress
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[downloadProgressKey{sceneID, requestID}]
	if entry == nil || !entry.finishedAt.IsZero() && time.Since(entry.finishedAt) >= downloadProgressRetention {
		return nil
	}
	ret := entry.DownloadProgress
	return &ret
}

// FFmpeg's machine-readable -progress output shares stderr with diagnostics.
// Keep draining even malformed/oversized lines, and bound the retained log tail.
// exec.Cmd.Wait joins its stderr writer before the caller reads tail.
type downloadProgressWriter struct {
	advance func(float64)
	tail    []byte
	line    []byte
	tooLong bool
}

func (w *downloadProgressWriter) Write(p []byte) (int, error) {
	const maxTail = 16 * 1024
	n := len(p)
	if len(p) >= maxTail {
		w.tail = append(w.tail[:0], p[len(p)-maxTail:]...)
	} else {
		if extra := len(w.tail) + len(p) - maxTail; extra > 0 {
			w.tail = w.tail[extra:]
		}
		w.tail = append(w.tail, p...)
	}
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		part := p
		if end >= 0 {
			part = p[:end]
		}
		if len(w.line)+len(part) > 256 {
			w.tooLong = true
		}
		if !w.tooLong {
			w.line = append(w.line, part...)
		}
		if end < 0 {
			break
		}
		if !w.tooLong {
			if value, ok := strings.CutPrefix(strings.TrimSpace(string(w.line)), "out_time_us="); ok {
				if microseconds, err := strconv.ParseFloat(value, 64); err == nil && w.advance != nil {
					w.advance(microseconds / 1_000_000)
				}
			}
		}
		w.line, w.tooLong = w.line[:0], false
		p = p[end+1:]
	}
	return n, nil
}
