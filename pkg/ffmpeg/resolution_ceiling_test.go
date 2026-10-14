package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDownloadCopyRespectsResolutionCeiling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.mp4")
	require.NoError(t, os.WriteFile(path, []byte("0123456789"), 0o600))
	for _, tc := range []struct {
		name          string
		mode          DownloadMode
		width, height int
		resolution    models.StreamingResolutionEnum
		status        int
	}{
		{"auto keeps smaller portrait", DownloadModeAuto, 720, 1280, models.StreamingResolutionEnumFullHd, http.StatusPartialContent},
		{"copy keeps exact ceiling", DownloadModeCopy, 1080, 1920, models.StreamingResolutionEnumFullHd, http.StatusPartialContent},
		{"4K tier keeps 2160p original", DownloadModeAuto, 3840, 2160, models.StreamingResolutionEnumFourK, http.StatusPartialContent},
		{"copy refuses oversize", DownloadModeCopy, 2160, 3840, models.StreamingResolutionEnumFullHd, http.StatusBadRequest},
		{"copy refuses unknown size under ceiling", DownloadModeCopy, 0, 0, models.StreamingResolutionEnumFullHd, http.StatusBadRequest},
		{"original has no size ceiling", DownloadModeCopy, 0, 0, models.StreamingResolutionEnumOriginal, http.StatusPartialContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sm := &StreamManager{lockManager: fsutil.NewReadLockManager()}
			file := &models.VideoFile{BaseFile: &models.BaseFile{Path: path}, VideoCodec: H264, Format: Mp4Ffmpeg, Width: tc.width, Height: tc.height}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Range", "bytes=2-5")
			w := httptest.NewRecorder()
			sm.ServeDownload(w, req, DownloadOptions{VideoFile: file, Mode: tc.mode, Resolution: tc.resolution})
			require.Equal(t, tc.status, w.Code)
			if tc.status == http.StatusPartialContent {
				require.Equal(t, "2345", w.Body.String(), "a compatible original must retain byte ranges without invoking ffmpeg")
			}
		})
	}
}

func TestHardwareResolutionCeilingsNeverUpscale(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		w, h, ceiling, maxW, maxH int
		filter                    VideoFilter
	}{
		{"small wide original", 800, 80, 1080, 4096, 4096, ""},
		{"small portrait original", 720, 1280, 1080, 4096, 4096, ""},
		{"portrait resize", 2160, 3840, 1080, 4096, 4096, "scale=1080:1920"},
		{"landscape resize", 3840, 2160, 1080, 4096, 4096, "scale=1920:1080"},
		{"hardware width limit", 8000, 1000, 1080, 4096, 4096, "scale=4096:512"},
		{"hardware height limit", 1000, 8000, 1080, 4096, 4096, "scale=512:4096"},
		{"hardware limit at original", 8000, 1000, 0, 4096, 4096, "scale=4096:512"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.filter, VideoFilter("").ScaleMaxLM(tc.w, tc.h, tc.ceiling, tc.maxW, tc.maxH))
		})
	}
}

func TestHardwareDownloadPortraitCeilings(t *testing.T) {
	for _, codec := range []VideoCodec{VideoCodecV265, VideoCodecVAV1, VideoCodecIAV1, VideoCodecNAV1} {
		for _, fullHW := range []bool{false, true} {
			for _, hdr := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/fullHW=%v/HDR=%v", codec.CodeName, fullHW, hdr), func(t *testing.T) {
					color := videoColorMetadata{}
					if hdr {
						color.Transfer = colorTransferPQ
					}
					filter := func(w, h int) string {
						vf := &models.VideoFile{Width: w, Height: h}
						if codec == VideoCodecV265 {
							return string(hevcHWVideoFilter(codec, vf, 1080, fullHW, color))
						}
						return string(av1HWVideoFilter(codec, vf, 1080, fullHW, color))
					}
					within := filter(1080, 1920)
					require.NotContains(t, within, "w=", "portrait originals at the ceiling must not shrink")
					require.NotContains(t, within, "scale=", "CPU scaling must also use the shorter edge")
					oversize := filter(2160, 3840)
					if fullHW {
						require.Contains(t, oversize, "w=1080:h=1920")
					} else {
						require.Contains(t, oversize, "scale=1080:-2")
					}
					if fullHW || codec != VideoCodecIAV1 {
						format := "nv12"
						if hdr {
							format = "p010"
						}
						require.Contains(t, within, "format="+format)
						require.Contains(t, oversize, "format="+format)
					}
				})
			}
		}
	}
}

type ceilingDownloadTestConfig struct{ downloadTestConfig }

func (ceilingDownloadTestConfig) GetLiveTranscodeOutputArgs() []string {
	return []string{"-x265-params", "pools=1:frame-threads=1"}
}

// Exercise the actual CPU encoder, not just its command string: the old height
// comparison shrank portrait originals even when they already fit the ceiling.
func TestHEVCDownloadKeepsPortraitResolutionCeiling(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the download resolution regression")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for the download resolution regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	encoders, err := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	require.NoError(t, err)
	if !bytes.Contains(encoders, []byte("libx265")) {
		t.Skip("libx265 is unavailable")
	}
	for _, tc := range []struct {
		w, h int
		want string
	}{
		{180, 320, "180x320"},
		{360, 640, "240x426"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.mkv")
			out, err := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=12", tc.w, tc.h), "-t", "0.5", "-c:v", "libx264", "-preset", "ultrafast", source).CombinedOutput()
			require.NoError(t, err, "%s", out)
			sm := &StreamManager{encoder: NewEncoder(ffmpegPath), config: ceilingDownloadTestConfig{}, context: ctx}
			vf := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, VideoCodec: H264, Width: tc.w, Height: tc.h, Duration: 0.5}
			args := sm.downloadArgs(vf, encDownloadHEVC, models.StreamingResolutionEnumLow, true, videoColorMetadata{})
			cmd := exec.CommandContext(ctx, ffmpegPath, args...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			data, err := cmd.Output()
			require.NoError(t, err, "%s", stderr.String())
			download := filepath.Join(dir, "download.mp4")
			require.NoError(t, os.WriteFile(download, data, 0o600))
			out, err = exec.CommandContext(ctx, ffprobePath, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", download).CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.Equal(t, tc.want, strings.TrimSpace(string(out)))
		})
	}
}
