package ffmpeg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type pendingInitTestConfig struct{ rotationTestStreamConfig }

// Make the initial segment take longer than a monitor tick, as a real 4K
// transcode does. A tiny unrestricted fixture can finish before a bad restart.
func (pendingInitTestConfig) GetLiveTranscodeInputArgs() []string { return []string{"-re"} }

func TestV3PendingInitDoesNotStarveDistantMedia(t *testing.T) {
	encoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	out, err := exec.CommandContext(ctx, encoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=size=160x90:rate=30:duration=28", "-c:v", "libx264", "-g", "60", source).CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, streamType := range []*V3StreamType{V3StreamTypeHLS, V3StreamTypeHLSScrub} {
		t.Run(streamType.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(ctx)
			sm := &StreamManager{
				context: ctx, cancelFunc: cancel, cacheDir: t.TempDir(),
				encoder: NewEncoder(encoder), ffprobe: NewFFProbe(probe),
				config: pendingInitTestConfig{}, lockManager: fsutil.NewReadLockManager(),
				v3RunningStreams: make(map[string]*v3RunningStream),
			}
			t.Cleanup(sm.Shutdown)
			file := &models.VideoFile{BaseFile: &models.BaseFile{ID: 42, Path: source}, VideoCodec: H264, Width: 160, Height: 90, Duration: 28, FrameRate: 30}
			responses := make(chan *httptest.ResponseRecorder, 2)
			for i, segment := range []string{"init", "12"} {
				go func() {
					r := httptest.NewRequest(http.MethodGet, "/segment?stream_session=preview", nil).WithContext(ctx)
					w := httptest.NewRecorder()
					sm.ServeV3Segment(w, r, V3StreamOptions{
						StreamType: streamType, VideoFile: file, Hash: "concurrent-init", Resolution: "LOW", Track: TrackVideo, Segment: segment,
					})
					responses <- w
				}()
				// Reproduce init arriving first, with a distant media request
				// queued before the monitor has produced either response.
				require.Eventually(t, func() bool {
					sm.streamsMutex.Lock()
					defer sm.streamsMutex.Unlock()
					for _, stream := range sm.v3RunningStreams {
						return len(stream.waitingSegments) == i+1
					}
					return false
				}, time.Second, time.Millisecond)
			}
			require.Eventually(t, func() bool {
				sm.monitorV3Streams()
				return len(responses) == 2
			}, 6*time.Second, 20*time.Millisecond, "init must not repeatedly restart at zero and starve the requested segment")
			for range 2 {
				response := <-responses
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.Greater(t, response.Body.Len(), 32)
			}
		})
	}
}
