package ffmpeg

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDownloadProgressAttempts(t *testing.T) {
	sm := &StreamManager{}
	first, ok := sm.downloadProgress.begin(1, "first", 100)
	require.True(t, ok)
	_, ok = sm.downloadProgress.begin(1, "first", 100)
	require.False(t, ok, "duplicate active requests must not share progress")
	require.Nil(t, sm.GetDownloadProgress(2, "first"))
	require.Nil(t, sm.GetDownloadProgress(1, "other-device"))
	var workers sync.WaitGroup
	for i := range 8 {
		workers.Go(func() {
			for j := range 50 {
				sm.downloadProgress.advance(first, float64(i+j))
				_ = sm.GetDownloadProgress(1, "first")
			}
		})
	}
	workers.Wait()
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		sm.downloadProgress.advance(first, invalid)
	}
	require.Equal(t, 56.0, sm.GetDownloadProgress(1, "first").ProcessedSeconds)
	sm.downloadProgress.finish(first, false)
	require.Equal(t, DownloadFailed, sm.GetDownloadProgress(1, "first").State)
	second, ok := sm.downloadProgress.begin(1, "retry", 100)
	require.True(t, ok)
	sm.downloadProgress.advance(first, 99)
	require.Zero(t, sm.GetDownloadProgress(1, "retry").ProcessedSeconds)
	sm.downloadProgress.advance(second, 150)
	require.Equal(t, 100.0, sm.GetDownloadProgress(1, "retry").ProcessedSeconds)
	require.Equal(t, DownloadProcessing, sm.GetDownloadProgress(1, "retry").State)
	sm.downloadProgress.finish(second, true)
	require.Equal(t, DownloadFinished, sm.GetDownloadProgress(1, "retry").State)
	second.finishedAt = time.Now().Add(-downloadProgressRetention)
	require.Nil(t, sm.GetDownloadProgress(1, "retry"))
	for i := range downloadProgressHistory * 2 {
		entry, _ := sm.downloadProgress.begin(1, strconv.Itoa(i), 10)
		sm.downloadProgress.finish(entry, true)
	}
	require.LessOrEqual(t, len(sm.downloadProgress.entries), downloadProgressHistory)
	for _, invalid := range []string{"", "../path", strings.Repeat("a", 129), "request\n"} {
		require.False(t, ValidDownloadRequestID(invalid))
	}
}

func TestDownloadProgressReadsChunkedFFmpegOutput(t *testing.T) {
	sm := &StreamManager{}
	entry, _ := sm.downloadProgress.begin(1, "request", 20)
	writer := &downloadProgressWriter{advance: func(seconds float64) { sm.downloadProgress.advance(entry, seconds) }}
	output := "out_time_us=1500000\nprogress=continue\n" + strings.Repeat("x", 100000) +
		"out_time_us=9000000\nout_time_us=2750000\r\nwarning=invalid\nout_time_us=N/A\nout_time_us=NaN\nout_time_us=-10\nprogress=end\n"
	for start := 0; start < len(output); start += 7 {
		_, err := writer.Write([]byte(output[start:min(start+7, len(output))]))
		require.NoError(t, err)
	}
	require.Equal(t, 2.75, sm.GetDownloadProgress(1, "request").ProcessedSeconds)
	require.Equal(t, DownloadProcessing, sm.GetDownloadProgress(1, "request").State, "progress=end alone does not establish encoder success")
	require.LessOrEqual(t, len(writer.tail), 16*1024)
	require.Contains(t, string(writer.tail), "warning=invalid")
}

type downloadTestConfig struct{ realtime bool }

func (downloadTestConfig) GetMaxStreamingTranscodeSize() models.StreamingResolutionEnum {
	return models.StreamingResolutionEnumOriginal
}
func (c downloadTestConfig) GetLiveTranscodeInputArgs() []string {
	if c.realtime {
		return []string{"-re"}
	}
	return nil
}
func (downloadTestConfig) GetLiveTranscodeOutputArgs() []string   { return nil }
func (downloadTestConfig) GetTranscodeHardwareAcceleration() bool { return false }

func TestServeDownloadProgress(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for download progress regression")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24", "-t", "3", "-c:v", "libx264", "-preset", "ultrafast", source).CombinedOutput()
	require.NoError(t, err, "%s", output)

	for _, scenario := range []string{"finished", "cancelled", "failed", "remux"} {
		t.Run(scenario, func(t *testing.T) {
			sm := &StreamManager{
				encoder: NewEncoder(ffmpegPath), config: downloadTestConfig{realtime: true},
				lockManager: fsutil.NewReadLockManager(), context: ctx,
			}
			file := &models.VideoFile{
				BaseFile:   &models.BaseFile{Path: source},
				VideoCodec: H264, Width: 160, Height: 90, Duration: 3,
			}
			if scenario == "failed" {
				file.Path = filepath.Join(dir, "missing.mkv")
			}
			mode := DownloadModeH264
			if scenario == "remux" {
				mode = DownloadModeCopy
			}
			done := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				sm.ServeDownload(w, r, DownloadOptions{VideoFile: file, Mode: mode, SceneID: 42, RequestID: scenario})
			}))
			defer server.Close()
			requestCtx, stop := context.WithCancel(ctx)
			defer stop()
			req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			type downloadResult struct {
				body []byte
				err  error
			}
			result := make(chan downloadResult, 1)
			go func() {
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					result <- downloadResult{err: err}
					return
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				result <- downloadResult{body: body, err: err}
			}()
			if scenario != "failed" {
				require.Eventually(t, func() bool {
					progress := sm.GetDownloadProgress(42, scenario)
					return progress != nil && progress.State == DownloadProcessing && progress.ProcessedSeconds > 0 && progress.ProcessedSeconds < 3
				}, 5*time.Second, 20*time.Millisecond)
			}
			if scenario == "cancelled" {
				stop()
			}
			var downloaded downloadResult
			select {
			case downloaded = <-result:
			case <-ctx.Done():
				t.Fatal("download did not finish")
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("server did not release the encoder")
			}
			progress := sm.GetDownloadProgress(42, scenario)
			require.NotNil(t, progress)
			if scenario == "cancelled" || scenario == "failed" {
				require.Equal(t, DownloadFailed, progress.State)
				return
			}
			require.NoError(t, downloaded.err)
			require.Equal(t, DownloadFinished, progress.State)
			require.True(t, bytes.Contains(downloaded.body[:min(len(downloaded.body), 64)], []byte("ftyp")), "progress must not pollute the MP4 stream")
			outputPath := filepath.Join(dir, scenario+".mp4")
			require.NoError(t, os.WriteFile(outputPath, downloaded.body, 0o600))
			decoded, err := exec.CommandContext(ctx, ffmpegPath, "-v", "error", "-i", outputPath, "-f", "null", "-").CombinedOutput()
			require.NoError(t, err, "download must remain playable: %s", decoded)
		})
	}
}

func TestServeDownloadDirectFileStillHasLengthAndRanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.mp4")
	require.NoError(t, os.WriteFile(path, []byte("0123456789"), 0o600))
	sm := &StreamManager{lockManager: fsutil.NewReadLockManager()}
	file := &models.VideoFile{BaseFile: &models.BaseFile{Path: path}, VideoCodec: H264, Format: Mp4Ffmpeg}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	sm.ServeDownload(w, req, DownloadOptions{VideoFile: file, Mode: DownloadModeCopy, SceneID: 1, RequestID: "direct"})
	require.Equal(t, http.StatusPartialContent, w.Code)
	require.Equal(t, "bytes 2-5/10", w.Header().Get("Content-Range"))
	require.Equal(t, "4", w.Header().Get("Content-Length"))
	require.Equal(t, "2345", w.Body.String())
	require.Nil(t, sm.GetDownloadProgress(1, "direct"))
}
