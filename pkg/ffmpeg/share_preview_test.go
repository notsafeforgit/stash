package ffmpeg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestInlineVideoPreviewEncodesPlayableSDRMP4(t *testing.T) {
	encoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	output, err := exec.CommandContext(ctx, encoder, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=800x1200:rate=4", "-f", "lavfi", "-i", "sine",
		"-t", "0.5", "-c:v", "ffv1", "-pix_fmt", "yuv420p10le", "-color_trc", "arib-std-b67",
		"-color_primaries", "bt2020", "-colorspace", "bt2020nc", "-c:a", "pcm_s16le", source).CombinedOutput()
	require.NoError(t, err, "%s", output)
	transfer := "arib-std-b67"
	vf := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, VideoCodec: "ffv1", AudioCodec: "pcm_s16le",
		Width: 800, Height: 1200, FrameRate: 4, Duration: .5, ColorTransfer: &transfer}
	sm := &StreamManager{context: ctx, encoder: NewEncoder(encoder), config: encodingTestConfig{}, lockManager: fsutil.NewReadLockManager()}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/preview/video.mp4", nil).WithContext(ctx)
		sm.ServeDownload(w, r, DownloadOptions{VideoFile: vf, Mode: DownloadModeH264,
			Resolution: models.StreamingResolutionEnumStandardHd, Filename: "shared-video", Inline: true})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "video/mp4", w.Header().Get("Content-Type"))
		require.Equal(t, "inline; filename=shared-video.mp4", w.Header().Get("Content-Disposition"))
		require.Empty(t, w.Header().Get("Accept-Ranges"))
		if method == http.MethodHead {
			require.Empty(t, w.Body.String())
			continue
		}
		preview := filepath.Join(dir, "preview.mp4")
		require.NoError(t, os.WriteFile(preview, w.Body.Bytes(), 0600))
		output, err = exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "stream=codec_name,width,height,pix_fmt,color_transfer", "-of", "json", preview).Output()
		require.NoError(t, err)
		var decoded struct {
			Streams []struct {
				Codec         string `json:"codec_name"`
				Width, Height int
				PixelFormat   string `json:"pix_fmt"`
				Transfer      string `json:"color_transfer"`
			}
		}
		require.NoError(t, json.Unmarshal(output, &decoded))
		require.Len(t, decoded.Streams, 2)
		require.Equal(t, "h264", decoded.Streams[0].Codec)
		require.Equal(t, 720, decoded.Streams[0].Width)
		require.Equal(t, 1080, decoded.Streams[0].Height)
		require.Equal(t, "yuv420p", decoded.Streams[0].PixelFormat)
		require.Equal(t, "bt709", decoded.Streams[0].Transfer)
		require.Equal(t, "aac", decoded.Streams[1].Codec)
	}
}
