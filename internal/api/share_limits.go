package api

import (
	"context"
	"github.com/stashapp/stash/pkg/models"
	"net"
	"net/http"
	"sync"
	"time"
)

type shareWindow struct {
	at    time.Time
	count int
}
type shareStreamLease struct {
	shareID, session string
	fileID           models.FileID
	at, expires      time.Time
	timer            *time.Timer
}
type shareBudget struct {
	mu         sync.Mutex
	exchanges  map[string]shareWindow
	streams    map[string]shareStreamLease
	writes     map[string]time.Time
	archives   chan struct{}
	stopStream func(models.FileID, string)
}

func newShareBudget() *shareBudget {
	return &shareBudget{exchanges: make(map[string]shareWindow), streams: make(map[string]shareStreamLease), writes: make(map[string]time.Time), archives: make(chan struct{}, 2)}
}

func (b *shareBudget) exchange(peer string) bool {
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for key, window := range b.exchanges {
		if now.Sub(window.at) >= time.Minute {
			delete(b.exchanges, key)
		}
	}
	if len(b.exchanges) >= 4096 {
		return false
	}
	window := b.exchanges[peer]
	if window.at.IsZero() {
		window.at = now
	}
	window.count++
	b.exchanges[peer] = window
	return window.count <= 30
}

func (b *shareBudget) stream(shareID, key string, fileID models.FileID, session string, expires time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	shareCount := 0
	for _, lease := range b.streams {
		if lease.shareID == shareID {
			shareCount++
		}
	}
	if _, exists := b.streams[key]; !exists && (shareCount >= 3 || len(b.streams) >= 12) {
		return false
	}
	lease := b.streams[key]
	if lease.timer != nil {
		lease.timer.Stop()
	}
	lease = shareStreamLease{shareID: shareID, session: session, fileID: fileID, at: now, expires: expires}
	lease.timer = time.AfterFunc(max(0, min(90*time.Second, time.Until(expires))), func() { b.expireStream(key) })
	b.streams[key] = lease
	return true
}

func (b *shareBudget) expireStream(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	lease, ok := b.streams[key]
	if !ok || (time.Since(lease.at) < 90*time.Second && time.Now().Before(lease.expires)) {
		return
	}
	b.releaseSessionLocked(lease.session)
}

func (b *shareBudget) releaseSessionLocked(session string) {
	for key, lease := range b.streams {
		if lease.session == session {
			lease.timer.Stop()
			delete(b.streams, key)
			if b.stopStream != nil {
				b.stopStream(lease.fileID, session)
			}
		}
	}
}

func (b *shareBudget) releaseSession(session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.releaseSessionLocked(session)
}
func (b *shareBudget) releaseShare(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, lease := range b.streams {
		if lease.shareID == id {
			b.releaseSessionLocked(lease.session)
		}
	}
}

func (b *shareBudget) forgetOtherVariants(session, keep string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, lease := range b.streams {
		if lease.session == session && key != keep {
			lease.timer.Stop()
			delete(b.streams, key)
		}
	}
}

func (b *shareBudget) wait(ctx context.Context, shareID string, bytes int) error {
	b.mu.Lock()
	now := time.Now()
	next := b.writes[shareID]
	if next.Before(now) {
		next = now
	}
	b.writes[shareID] = next.Add(time.Duration(bytes) * time.Second / (20 * 1024 * 1024))
	if len(b.writes) > 1024 {
		for id, at := range b.writes {
			if now.Sub(at) > time.Minute {
				delete(b.writes, id)
			}
		}
	}
	b.mu.Unlock()
	if delay := time.Until(next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

// Keep cancellation and cache policy effective even when a reused media handler
// overrides headers or http.ServeContent copies a long range in one response.
type shareResponse struct {
	http.ResponseWriter
	ctx          context.Context
	budget       *shareBudget
	shareID      string
	status       int
	errorWritten bool
}

func (w *shareResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *shareResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.Header().Set("Cache-Control", "private, no-store")
	if status >= 400 {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *shareResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.status >= 400 {
		if !w.errorWritten {
			w.errorWritten = true
			if _, err := w.ResponseWriter.Write([]byte("Media unavailable\n")); err != nil {
				return 0, err
			}
		}
		return len(data), nil
	}
	total := 0
	for len(data) > 0 {
		n := min(len(data), 64*1024)
		if err := w.budget.wait(w.ctx, w.shareID, n); err != nil {
			return total, err
		}
		written, err := w.ResponseWriter.Write(data[:n])
		total += written
		if err != nil {
			return total, err
		}
		data = data[written:]
	}
	return total, nil
}
func (w *shareResponse) Flush() {
	if w.ctx.Err() == nil {
		if w.status == 0 {
			w.WriteHeader(http.StatusOK)
		}
		_ = http.NewResponseController(w.ResponseWriter).Flush()
	}
}
