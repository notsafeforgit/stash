package ffmpeg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestV3ShortStreamIgnoresSuccessfulEncoderDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("encoder diagnostic wrapper requires a POSIX shell")
	}
	encoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the stream completion regression")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for the stream completion regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	output, err := exec.CommandContext(ctx, encoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30", "-t", "3", "-c:v", "libx264", source).CombinedOutput()
	require.NoError(t, err, "%s", output)
	wrapper := filepath.Join(dir, "ffmpeg-diagnostic")
	script := "#!/bin/sh\nprintf '%s\\n' 'libva info: harmless driver diagnostic' >&2\nexec '" + strings.ReplaceAll(encoder, "'", "'\"'\"'") + "' \"$@\"\n"
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0o700))
	sm := NewStreamManager(t.TempDir(), NewEncoder(wrapper), NewFFProbe(probe), rotationTestStreamConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42, Path: source}, VideoCodec: H264, Width: 160, Height: 90, Duration: 3, FrameRate: 30}
	for _, segment := range []string{"init", "0", "1"} {
		r := httptest.NewRequest(http.MethodGet, "/stream.m3u8/video/"+segment+"?stream_session=browser", nil)
		r = r.WithContext(WithPrivateV3Stream(ctx, "server-owned"))
		w := httptest.NewRecorder()
		sm.ServeV3Segment(w, r, V3StreamOptions{
			StreamType: V3StreamTypeHLS, VideoFile: file, Hash: "short-video", Track: TrackVideo, Segment: segment,
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Greater(t, w.Body.Len(), 32)
	}
}

func TestPrivateV3SegmentsUseServerSession(t *testing.T) {
	sm := &StreamManager{cacheDir: t.TempDir(), config: rotationTestStreamConfig{}}
	file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42}, Duration: 10, FrameRate: 30}
	sm.StopV3StreamsForSession(file.ID, "server-owned", "", true)
	for _, segment := range []string{"init", "0"} {
		r := httptest.NewRequest(http.MethodGet, "/stream.m3u8/video/"+segment+"?stream_session=browser", nil)
		r = r.WithContext(WithPrivateV3Stream(r.Context(), "server-owned"))
		w := httptest.NewRecorder()
		sm.ServeV3Segment(w, r, V3StreamOptions{
			StreamType: V3StreamTypeHLS, VideoFile: file, Hash: "scene", Track: TrackVideo, Segment: segment,
		})
		require.Equal(t, http.StatusGone, w.Code, "released guest encoders must reject late initialization and segment requests")
		require.Empty(t, sm.v3RunningStreams)
	}
	r := httptest.NewRequest(http.MethodGet, "/stream?stream_session=owner", nil)
	session, err := V3StreamSessionFromRequest(r)
	require.NoError(t, err)
	require.Equal(t, "owner", session)
}
