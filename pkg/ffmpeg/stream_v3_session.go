package ffmpeg

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// A session isolates a prepared item from other markers, players and tabs
// using the same file. Empty sessions retain the legacy shared stream.
func ParseV3StreamSession(value string) (string, error) {
	if len(value) > 128 {
		return "", fmt.Errorf("invalid stream session")
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return "", fmt.Errorf("invalid stream session")
	}
	return value, nil
}

func (t V3StreamType) SessionDir(hash string, maxTranscodeSize int, session string) string {
	dir := t.FileDir(hash, maxTranscodeSize)
	if session == "" {
		return dir
	}
	return fmt.Sprintf("%s_session_%x", dir, sha256.Sum256([]byte(session)))
}

type v3StreamSessionKey struct {
	fileID  models.FileID
	session string
}

// Explicitly released sessions reject late segment requests. Aborting a
// browser fetch and sending the release beacon can arrive out of order;
// without this, rapid scrolling could resurrect an evicted encoder.
// Caller holds streamsMutex.
func (sm *StreamManager) v3SessionClosed(fileID models.FileID, session string, now time.Time) bool {
	key := v3StreamSessionKey{fileID, session}
	until, ok := sm.v3ClosedSessions[key]
	if ok && !until.After(now) {
		delete(sm.v3ClosedSessions, key)
		return false
	}
	return ok
}

func (sm *StreamManager) closeV3Session(fileID models.FileID, session string) {
	if session == "" {
		return
	}
	now := time.Now()
	for key, until := range sm.v3ClosedSessions {
		if !until.After(now) {
			delete(sm.v3ClosedSessions, key)
		}
	}
	if sm.v3ClosedSessions == nil {
		sm.v3ClosedSessions = make(map[v3StreamSessionKey]time.Time)
	}
	sm.v3ClosedSessions[v3StreamSessionKey{fileID, session}] = now.Add(5 * time.Minute)
}
