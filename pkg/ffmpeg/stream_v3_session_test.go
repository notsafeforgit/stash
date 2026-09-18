package ffmpeg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

func TestV3PreparedSessionsRetainOtherMarkersAndQualities(t *testing.T) {
	file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42}, Duration: 100}
	sm := &StreamManager{v3RunningStreams: make(map[string]*v3RunningStream)}
	add := func(session string, resolution int) string {
		dir := V3StreamTypeHLS.SessionDir("scene", resolution, session)
		sm.v3RunningStreams[dir] = &v3RunningStream{
			dir: dir, session: session, vf: file, outputDir: t.TempDir(),
			lastAccessed: time.Now().Add(-10 * time.Second),
		}
		return dir
	}
	oldQuality := add("marker-a", 720)
	newQuality := add("marker-a", 1080)
	neighbor := add("marker-b", 720)
	legacy := add("", 720)
	sm.v3RunningStreams[newQuality].lastAccessed = time.Now()
	// The foreground marker must not supersede another retained marker, even
	// though both use the same file and one has already filled its buffer.
	sm.checkV3Transcode(sm.v3RunningStreams[neighbor], time.Now())
	if sm.v3RunningStreams[neighbor] == nil {
		t.Fatal("foreground marker reaped the prepared neighbor")
	}
	sm.StopV3StreamsForSession(file.ID, "marker-a", newQuality, false)
	if sm.v3RunningStreams[oldQuality] != nil || sm.v3RunningStreams[newQuality] == nil || sm.v3RunningStreams[neighbor] == nil {
		t.Fatal("quality change did not isolate its own outgoing variant")
	}
	// Legacy players may still stop their unscoped stream without closing TV.
	sm.StopV3StreamsForSession(file.ID, "", "", false)
	if sm.v3RunningStreams[legacy] != nil || len(sm.v3RunningStreams) != 2 {
		t.Fatal("unscoped stop disturbed prepared sessions")
	}
	waiting := &v3WaitingSegment{available: make(chan error, 1)}
	sm.v3RunningStreams[newQuality].waitingSegments = []*v3WaitingSegment{waiting}
	sm.StopV3StreamsForSession(file.ID, "marker-a", "", true)
	if len(sm.v3RunningStreams) != 1 || sm.v3RunningStreams[neighbor] == nil {
		t.Fatal("eviction disturbed the retained neighbor")
	}
	if err := <-waiting.available; !errors.Is(err, context.Canceled) {
		t.Fatalf("pending segment was not cancelled: %v", err)
	}
	// File mutations still invalidate every owner, allowing the same session
	// to recreate its stream from the changed file without a release tombstone.
	sm.StopV3StreamsForFile(file.ID, "")
	if len(sm.v3RunningStreams) != 0 || sm.v3SessionClosed(file.ID, "marker-b", time.Now()) {
		t.Fatal("file invalidation did not clear all owners for reuse")
	}
}

func TestV3ReleasedSessionRejectsLateSegment(t *testing.T) {
	sm := &StreamManager{cacheDir: t.TempDir(), config: rotationTestStreamConfig{}}
	file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42}, Duration: 100, FrameRate: 30}
	sm.StopV3StreamsForSession(file.ID, "evicted", "", true)
	r := httptest.NewRequest(http.MethodGet, "/segment?stream_session=evicted", nil)
	w := httptest.NewRecorder()
	sm.ServeV3Segment(w, r, V3StreamOptions{
		StreamType: V3StreamTypeHLS, VideoFile: file, Hash: "scene", Track: TrackVideo, Segment: "5",
	})
	if w.Code != http.StatusGone || len(sm.v3RunningStreams) != 0 {
		t.Fatalf("late request recreated an evicted encoder: status=%d streams=%d", w.Code, len(sm.v3RunningStreams))
	}
	if sm.v3SessionClosed(file.ID+1, "evicted", time.Now()) {
		t.Fatal("release escaped its file scope")
	}
	if sm.v3SessionClosed(file.ID, "evicted", time.Now().Add(6*time.Minute)) || len(sm.v3ClosedSessions) != 0 {
		t.Fatal("expired release tombstone was not reclaimed")
	}
}

func TestV3StreamSessionIdentity(t *testing.T) {
	for _, invalid := range []string{"../path", "id/child", "id?key", strings.Repeat("x", 129)} {
		if _, err := ParseV3StreamSession(invalid); err == nil {
			t.Errorf("accepted invalid session %q", invalid)
		}
	}
	for _, valid := range []string{"", "a5dba701-2e91-4b18-853f-5e8b677e5e27"} {
		if got, err := ParseV3StreamSession(valid); err != nil || got != valid {
			t.Errorf("rejected valid session %q", valid)
		}
	}
	if V3StreamTypeHLS.SessionDir("scene", 720, "") != V3StreamTypeHLS.FileDir("scene", 720) {
		t.Fatal("legacy directory changed")
	}
	if V3StreamTypeHLS.SessionDir("scene", 720, "a") == V3StreamTypeHLS.SessionDir("scene", 720, "b") {
		t.Fatal("prepared marker directories collide")
	}
	r := httptest.NewRequest(http.MethodGet, "/scene/42/stream.master.m3u8?stream_session=marker-a&start=20&end=30", nil)
	if !strings.Contains(hlsMasterTrackURL(r, TrackVideo), "stream_session=marker-a") {
		t.Fatal("master playlist dropped session identity")
	}
}
