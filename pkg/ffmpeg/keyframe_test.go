package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/keyframecache"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func keyframeTestTools(t testing.TB) (string, string) {
	t.Helper()
	encoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	return encoder, probe
}

func runKeyframeTool(t testing.TB, program string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, program, args...).CombinedOutput()
	require.NoError(t, err, "%s %v: %s", program, args, out)
	return out
}

// One keyframe per second (30 frames at 30 fps) with B-frames, 640x360.
var keyframeTestGOP = []string{"-c:v", "libx264", "-preset", "ultrafast", "-g", "30", "-keyint_min", "30", "-sc_threshold", "0", "-bf", "2"}

type keyframeFixture struct {
	name string
	path string
	// Scene time of the first keyframe: first video pts minus container start.
	offsetMS int64
}

// keyframeFixtures produces the same 5 s GOP in containers whose start times
// differ: MP4 (zero start), Matroska (audio priming before zero) and MPEG-TS
// with a 10 s timestamp offset, with and without audio.
func keyframeFixtures(t *testing.T, encoder, probe string) []keyframeFixture {
	t.Helper()
	dir := t.TempDir()
	source := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "5"}
	mp4 := filepath.Join(dir, "gop.mp4")
	runKeyframeTool(t, encoder, append(append(append([]string{}, source...), keyframeTestGOP...), "-c:a", "aac", mp4)...)
	mkv := filepath.Join(dir, "gop.mkv")
	runKeyframeTool(t, encoder, append(append(append([]string{}, source...), keyframeTestGOP...), "-c:a", "aac", mkv)...)
	videoTS := filepath.Join(dir, "video.ts")
	runKeyframeTool(t, encoder, "-hide_banner", "-loglevel", "error", "-i", mkv, "-map", "0:v", "-c", "copy", "-f", "mpegts", "-output_ts_offset", "10", videoTS)
	avTS := filepath.Join(dir, "av.ts")
	runKeyframeTool(t, encoder, "-hide_banner", "-loglevel", "error", "-i", mkv, "-c", "copy", "-f", "mpegts", "-output_ts_offset", "10", avTS)

	fixtures := []keyframeFixture{{name: "mp4", path: mp4}, {name: "matroska", path: mkv}, {name: "mpegts-video", path: videoTS}, {name: "mpegts-av", path: avTS}}
	for i := range fixtures {
		f := &fixtures[i]
		// MPEG-TS repeats streams under their program; the values agree.
		firstValue := func(out []byte) float64 {
			return parsePTS(t, strings.Fields(string(out))[0])
		}
		formatStart := firstValue(runKeyframeTool(t, probe, "-v", "error", "-show_entries", "format=start_time", "-of", "csv=p=0", f.path))
		videoStart := firstValue(runKeyframeTool(t, probe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=start_time", "-of", "csv=p=0", f.path))
		f.offsetMS = int64(math.Round((videoStart - formatStart) * 1000))
		if strings.HasPrefix(f.name, "mpegts") {
			require.Greater(t, formatStart, 10.0, "%s must have a non-zero container start", f.name)
		}
	}
	// The fixtures cover both a zero and a non-zero first keyframe.
	require.Zero(t, fixtures[0].offsetMS)
	require.Zero(t, fixtures[2].offsetMS)
	require.Greater(t, fixtures[1].offsetMS, int64(10))
	require.Greater(t, fixtures[3].offsetMS, int64(10))
	return fixtures
}

// firstFrameAfterSeek reports the scene time of the first frame ffmpeg
// decodes after `-ss seek`, the input seek used by the scrub stream's start
// and by "generate screenshot at time".
func firstFrameAfterSeek(t *testing.T, encoder, path string, seek float64) int64 {
	t.Helper()
	out := runKeyframeTool(t, encoder, "-hide_banner", "-nostdin", "-loglevel", "info", "-copyts", "-ss", strconv.FormatFloat(seek, 'f', 6, 64), "-i", path, "-map", "0:v:0", "-vf", "showinfo", "-frames:v", "1", "-f", "null", "-")
	log := &keyframeStillLog{}
	_, _ = log.Write(out)
	_, _ = log.Write([]byte("\n"))
	require.True(t, log.havePTS, "%s", out)
	return int64(math.Round((log.pts - log.start) * 1000))
}

func keyframeTestVideo(path string, width, height int) *models.VideoFile {
	return &models.VideoFile{BaseFile: &models.BaseFile{ID: 7, Path: path, Generation: 1}, Width: width, Height: height, Duration: 5, FrameRate: 30}
}

func newKeyframeTestManager(t *testing.T, encoder, probe string) *StreamManager {
	t.Helper()
	sm := NewStreamManager(t.TempDir(), NewEncoder(encoder), NewFFProbe(probe), encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	return sm
}

func getKeyframeIndex(t *testing.T, sm *StreamManager, vf *models.VideoFile) keyframeIndexResponse {
	t.Helper()
	w := httptest.NewRecorder()
	sm.ServeKeyframeIndex(w, httptest.NewRequest("GET", "/scene/s/keyframes", nil), vf)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, "private, no-cache", w.Header().Get("Cache-Control"))
	var response keyframeIndexResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, `"`+response.Version+`"`, w.Header().Get("ETag"))
	return response
}

func getKeyframe(t *testing.T, sm *StreamManager, vf *models.VideoFile, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	sm.ServeKeyframe(w, httptest.NewRequest("GET", "/scene/s/keyframe?"+query, nil), vf)
	return w
}

func requireKeyframeJPEG(t *testing.T, w *httptest.ResponseRecorder, wantMS int64, width, height int) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
	require.Equal(t, formatKeyframeTime(wantMS), w.Header().Get("X-Keyframe-Time"))
	require.Equal(t, "X-Keyframe-Time", w.Header().Get("Access-Control-Expose-Headers"))
	config, err := jpeg.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	require.Equal(t, width, config.Width)
	require.Equal(t, height, config.Height)
}

func TestKeyframeIndexAndStillsUseSceneTime(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	fixtures := keyframeFixtures(t, encoder, probe)
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			want := make([]float64, 5)
			for i := range want {
				want[i] = float64(fixture.offsetMS+int64(i)*1000) / 1000
			}
			sm := newKeyframeTestManager(t, encoder, probe)
			vf := keyframeTestVideo(fixture.path, 640, 360)
			index := getKeyframeIndex(t, sm, vf)
			require.Equal(t, want, index.Times)
			require.Equal(t, 5.0, index.Duration)
			require.NotEmpty(t, index.Version)

			for _, keyframe := range index.Times {
				ms := int64(math.Round(keyframe * 1000))
				// Index times are the times `ffmpeg -ss` input seeking uses.
				require.Equal(t, ms, firstFrameAfterSeek(t, encoder, fixture.path, keyframe))
				if !strings.HasPrefix(fixture.name, "mpegts") {
					// MPEG-TS has no seek index: a mid-GOP seek resumes at the
					// next keyframe, so only keyframe times are exact there.
					require.Equal(t, ms+500, firstFrameAfterSeek(t, encoder, fixture.path, keyframe+0.5))
				}

				canonical := "t=" + formatKeyframeTime(ms) + "&v=" + index.Version
				started := time.Now()
				w := getKeyframe(t, sm, vf, canonical)
				elapsed := time.Since(started)
				w426, h240 := scaledDimensions(vf, 240)
				requireKeyframeJPEG(t, w, ms, w426, h240)
				require.Equal(t, "private, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
				t.Logf("%s keyframe %.3fs: %d bytes in %d ms", fixture.name, keyframe, w.Body.Len(), elapsed.Milliseconds())

				// The greatest keyframe ≤ t + 10 ms, from anywhere in the GOP.
				for _, offset := range []int64{1, 499, 989} {
					w := getKeyframe(t, sm, vf, "t="+formatKeyframeTime(ms+offset))
					requireKeyframeJPEG(t, w, ms, w426, h240)
					require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
				}
				if ms > 0 {
					requireKeyframeJPEG(t, getKeyframe(t, sm, vf, "t="+formatKeyframeTime(ms-9)), ms, w426, h240)
				}
			}
			// Before the first keyframe, the first keyframe is returned.
			requireKeyframeJPEG(t, getKeyframe(t, sm, vf, "t=0"), fixture.offsetMS, 426, 240)
			// A stale version is served, but not as immutable. Any t resolved
			// through the index with the current version is.
			atTwo := fixture.offsetMS + 1000
			if fixture.offsetMS == 0 {
				atTwo = 2000
			}
			w := getKeyframe(t, sm, vf, "t=2&v=stale")
			requireKeyframeJPEG(t, w, atTwo, 426, 240)
			require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
			w = getKeyframe(t, sm, vf, "t=2&v="+index.Version)
			requireKeyframeJPEG(t, w, atTwo, 426, 240)
			require.Equal(t, "private, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
		})
	}
}

func TestKeyframeStillWithoutIndexReportsDecodedFrame(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	fixture := keyframeFixtures(t, encoder, probe)[0]
	// Without ffprobe the index cannot be built, so the still seeks directly.
	sm := newKeyframeTestManager(t, encoder, probe)
	sm.keyframes.probe = nil
	vf := keyframeTestVideo(fixture.path, 640, 360)
	w := getKeyframe(t, sm, vf, "t=2.5&v=anything")
	requireKeyframeJPEG(t, w, 2000, 426, 240)
	require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))

	w = httptest.NewRecorder()
	sm.ServeKeyframeIndex(w, httptest.NewRequest("GET", "/scene/s/keyframes", nil), vf)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestKeyframeRequestsValidateInput(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	sm := newKeyframeTestManager(t, encoder, probe)
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("not decoded"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	for _, query := range []string{"", "t=", "t=abc", "t=-1", "t=NaN", "t=Inf", "t=1e9"} {
		require.Equal(t, http.StatusBadRequest, getKeyframe(t, sm, vf, query).Code, query)
	}
	missing := keyframeTestVideo(filepath.Join(t.TempDir(), "missing.mp4"), 640, 360)
	require.Equal(t, http.StatusNotFound, getKeyframe(t, sm, missing, "t=1").Code)
	w := httptest.NewRecorder()
	sm.ServeKeyframeIndex(w, httptest.NewRequest("GET", "/scene/s/keyframes", nil), missing)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestKeyframeVersionFollowsFileIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	first, err := newKeyframeSource(vf)
	require.NoError(t, err)
	again, err := newKeyframeSource(vf)
	require.NoError(t, err)
	require.Equal(t, first.version, again.version)
	require.Len(t, first.version, 20)

	vf.Generation = 2
	moved, err := newKeyframeSource(vf)
	require.NoError(t, err)
	require.NotEqual(t, first.version, moved.version)

	require.NoError(t, os.WriteFile(path, []byte("changed"), 0o600))
	changed, err := newKeyframeSource(vf)
	require.NoError(t, err)
	require.NotEqual(t, moved.version, changed.version)
	require.NotEqual(t, moved.indexKey, changed.indexKey)
}

func TestKeyframeIndexPersistsAcrossManagers(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	fixture := keyframeFixtures(t, encoder, probe)[1]
	storePath := filepath.Join(t.TempDir(), KeyframeStoreFile)
	vf := keyframeTestVideo(fixture.path, 640, 360)

	sm := newKeyframeTestManager(t, encoder, probe)
	sm.SetKeyframeStore(keyframecache.Open(storePath, KeyframeStoreBudgets()))
	first := getKeyframeIndex(t, sm, vf)
	requireKeyframeJPEG(t, getKeyframe(t, sm, vf, "t=1.021&v="+first.Version), fixture.offsetMS+1000, 426, 240)
	sm.Shutdown()

	// A later process reads both from the store without running ffprobe or ffmpeg.
	reopened := newKeyframeTestManager(t, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	reopened.SetKeyframeStore(keyframecache.Open(storePath, KeyframeStoreBudgets()))
	require.Equal(t, first, getKeyframeIndex(t, reopened, vf))
	w := getKeyframe(t, reopened, vf, "t=1.5&v="+first.Version)
	requireKeyframeJPEG(t, w, fixture.offsetMS+1000, 426, 240)
	require.Equal(t, "private, max-age=31536000, immutable", w.Header().Get("Cache-Control"))

	// An unchanged ETag needs no index work.
	r := httptest.NewRequest("GET", "/scene/s/keyframes", nil)
	r.Header.Set("If-None-Match", `"`+first.Version+`"`)
	recorder := httptest.NewRecorder()
	reopened.ServeKeyframeIndex(recorder, r, vf)
	require.Equal(t, http.StatusNotModified, recorder.Code)
}

func TestKeyframeStillToneMapsHDR(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	encoders := runKeyframeTool(t, encoder, "-hide_banner", "-encoders")
	if !bytes.Contains(encoders, []byte("libx265")) {
		t.Skip("libx265 is unavailable")
	}
	source := filepath.Join(t.TempDir(), "hdr.mkv")
	runKeyframeTool(t, encoder, "-v", "error", "-f", "lavfi", "-i", "nullsrc=s=64x64:r=30,format=yuv420p10le,geq=lum=512:cb=512:cr=512,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc:range=tv",
		"-t", "2.1", "-c:v", "libx265", "-preset", "ultrafast", "-g", "30", "-x265-params", "log-level=error:pools=1:frame-threads=1:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc", "-color_range", "tv", source)
	sm := newKeyframeTestManager(t, encoder, probe)
	// The library row has no color tags; the scan supplies them.
	vf := keyframeTestVideo(source, 64, 64)
	index := getKeyframeIndex(t, sm, vf)
	require.Equal(t, []float64{0, 1, 2}, index.Times)
	w := getKeyframe(t, sm, vf, "t=1&v="+index.Version)
	requireKeyframeJPEG(t, w, 1000, 64, 64)
	decoded, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	ycbcr, ok := decoded.(*image.YCbCr)
	require.True(t, ok)
	// Truncating 10-bit 512 gives limited 128 (full-range JPEG ~130); tone
	// mapping must visibly change it.
	require.Greater(t, math.Abs(float64(ycbcr.Y[0])-130), 8.0)
}

func TestKeyframeStillAppliesDisplayRotation(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	runKeyframeTool(t, encoder, append(append([]string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30", "-t", "2"}, keyframeTestGOP...), source)...)
	rotated := filepath.Join(dir, "rotated.mp4")
	if out, err := exec.Command(encoder, "-hide_banner", "-loglevel", "error", "-display_rotation:v:0", "90", "-i", source, "-c", "copy", rotated).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot write a display matrix: %s", out)
	}
	probed, err := NewFFProbe(probe).NewVideoFile(rotated)
	require.NoError(t, err)
	require.Equal(t, int64(90), probed.Rotation)
	// Library rows store display dimensions, as the scrub stream sizes them.
	vf := keyframeTestVideo(rotated, probed.Width, probed.Height)
	require.Equal(t, 360, vf.Width)
	sm := newKeyframeTestManager(t, encoder, probe)
	index := getKeyframeIndex(t, sm, vf)
	width, height := scaledDimensions(vf, 240)
	require.Equal(t, 240, width)
	requireKeyframeJPEG(t, getKeyframe(t, sm, vf, "t=1&v="+index.Version), 1000, width, height)
}

// fakeTool writes a shell script that records its process ID and sleeps.
func fakeTool(t *testing.T, name string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep unavailable")
	}
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	script := filepath.Join(dir, name)
	body := "#!/bin/sh\nif [ \"$1\" = -version ]; then echo \"" + name + " version 7.1\"; exit 0; fi\necho $$ >> " + pids + "\nexec sleep 30\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o700))
	return script, pids
}

func readPIDs(t *testing.T, path string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var pids []int
	for _, line := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(line)
		require.NoError(t, err)
		pids = append(pids, pid)
	}
	return pids
}

func processRunning(pid int) bool {
	// Reaped processes no longer exist; a zombie would mean it was not waited for.
	return syscall.Kill(pid, 0) == nil
}

func TestKeyframeStillCancelsFFmpegWithRequest(t *testing.T) {
	script, pids := fakeTool(t, "ffmpeg")
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	sm := NewStreamManager(t.TempDir(), NewEncoder(script), nil, encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	src, err := newKeyframeSource(vf)
	require.NoError(t, err)
	sm.keyframes.indexes.Add(src.indexKey, &keyframeIndex{times: []int64{0, 1000}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *httptest.ResponseRecorder)
	go func() {
		w := httptest.NewRecorder()
		sm.ServeKeyframe(w, httptest.NewRequest("GET", "/scene/s/keyframe?t=1", nil).WithContext(ctx), vf)
		done <- w
	}()
	require.Eventually(t, func() bool { return len(readPIDs(t, pids)) == 1 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish after cancellation")
	}
	require.False(t, processRunning(readPIDs(t, pids)[0]), "ffmpeg must be killed with the request")
	require.Empty(t, sm.keyframes.stillSlots, "the slot must be released")
}

func TestKeyframeStillRefusesWhenSaturated(t *testing.T) {
	script, pids := fakeTool(t, "ffmpeg")
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	sm := NewStreamManager(t.TempDir(), NewEncoder(script), nil, encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	src, err := newKeyframeSource(vf)
	require.NoError(t, err)
	sm.keyframes.indexes.Add(src.indexKey, &keyframeIndex{times: []int64{0, 1000}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := range keyframeStillConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.ServeKeyframe(httptest.NewRecorder(), httptest.NewRequest("GET", fmt.Sprintf("/scene/s/keyframe?t=%d", i), nil).WithContext(ctx), vf)
		}()
	}
	require.Eventually(t, func() bool { return len(readPIDs(t, pids)) == keyframeStillConcurrency }, 5*time.Second, 10*time.Millisecond)
	w := getKeyframe(t, sm, vf, "t=1")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	require.Len(t, readPIDs(t, pids), keyframeStillConcurrency, "a refused request must not start ffmpeg")
	cancel()
	wg.Wait()
	for _, pid := range readPIDs(t, pids) {
		require.False(t, processRunning(pid))
	}
}

func TestKeyframeIndexSharesScanAndCancelsAbandonedWork(t *testing.T) {
	script, pids := fakeTool(t, "ffprobe")
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	sm := NewStreamManager(t.TempDir(), nil, NewFFProbe(script), encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	sm.keyframes.idleGrace = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	codes := make([]int, 3)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			sm.ServeKeyframeIndex(w, httptest.NewRequest("GET", "/scene/s/keyframes", nil).WithContext(ctx), vf)
			codes[i] = w.Code
		}()
	}
	require.Eventually(t, func() bool { return len(readPIDs(t, pids)) == 1 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	require.Len(t, readPIDs(t, pids), 1, "concurrent requests share one scan")
	pid := readPIDs(t, pids)[0]

	// A still request during the grace period rejoins the scan.
	cancel()
	wg.Wait()
	require.True(t, processRunning(pid), "the scan survives a short gap between requests")
	require.Equal(t, http.StatusInternalServerError, getKeyframe(t, sm, vf, "t=1").Code, "no ffmpeg is configured")
	require.Len(t, readPIDs(t, pids), 1)

	// Nobody waits any more: the scan is cancelled and not recorded as a failure.
	require.Eventually(t, func() bool { return !processRunning(pid) }, 3*time.Second, 10*time.Millisecond)
	src, err := newKeyframeSource(vf)
	require.NoError(t, err)
	_, failed := sm.keyframes.failures.Get(src.indexKey)
	require.False(t, failed)
	require.Eventually(t, func() bool {
		sm.keyframes.mu.Lock()
		defer sm.keyframes.mu.Unlock()
		return len(sm.keyframes.jobs) == 0
	}, 3*time.Second, 10*time.Millisecond)
}

func TestKeyframeIndexTimeoutIsRetriedLater(t *testing.T) {
	script, pids := fakeTool(t, "ffprobe")
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	sm := NewStreamManager(t.TempDir(), nil, NewFFProbe(script), encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	sm.keyframes.indexTimeout = 100 * time.Millisecond
	for range 2 {
		w := httptest.NewRecorder()
		sm.ServeKeyframeIndex(w, httptest.NewRequest("GET", "/scene/s/keyframes", nil), vf)
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
		require.Equal(t, "600", w.Header().Get("Retry-After"))
	}
	require.Len(t, readPIDs(t, pids), 1, "a timed-out file is not rescanned immediately")
	require.False(t, processRunning(readPIDs(t, pids)[0]))
}

func TestKeyframeIndexEncodingAndLookup(t *testing.T) {
	index, err := newKeyframeIndex([]float64{12.4, 11.4, 11.4, 13.4005, 11.0}, 11.378667, videoColorMetadata{Transfer: "smpte2084"})
	require.NoError(t, err)
	// Keyframes before the container start are not seekable scene times.
	require.Equal(t, []int64{21, 1021, 2022}, index.times)
	decoded, err := decodeKeyframeIndex(encodeKeyframeIndex(index))
	require.NoError(t, err)
	require.Equal(t, index, decoded)
	require.Equal(t, int64(21), decoded.atOrBefore(0))
	require.Equal(t, int64(21), decoded.atOrBefore(1020))
	require.Equal(t, int64(1021), decoded.atOrBefore(1021))
	require.Equal(t, int64(2022), decoded.atOrBefore(1_000_000))

	_, err = newKeyframeIndex(nil, 0, videoColorMetadata{})
	require.ErrorIs(t, err, errNoKeyframes)
	for _, invalid := range [][]byte{nil, {2}, {1, 0, 0, 0, 0, 0}, {1, 0, 0, 0, 0, 2, 5, 0}} {
		_, err := decodeKeyframeIndex(invalid)
		require.Error(t, err, "%v", invalid)
	}

	// All-intra sources are thinned to a bounded, evenly spaced index.
	raw := make([]float64, 200_000)
	for i := range raw {
		raw[i] = float64(i) / 30
	}
	dense, err := newKeyframeIndex(raw, 0, videoColorMetadata{})
	require.NoError(t, err)
	require.LessOrEqual(t, len(dense.times), maxIndexedKeyframes)
	require.Greater(t, len(dense.times), maxIndexedKeyframes/2)
	require.Equal(t, int64(0), dense.times[0])
	packed := encodeKeyframeIndex(dense)
	require.Less(t, len(packed), 3*len(dense.times))
}

func TestKeyframeStillLogParsesStartAndFrameTime(t *testing.T) {
	log := &keyframeStillLog{}
	for _, chunk := range []string{
		"Input #0, matroska,webm, from 'x.mkv':\n  Metadata:\n    title           :   Duration: 1, start: 99.0, bitrate\n",
		"  Duration: 00:00:05.00, start: -0.0",
		"21000, bitrate: 300 kb/s\n",
		"[Parsed_showinfo_1 @ 0x1] config in time_base: 1/1000\n",
		"[Parsed_showinfo_1 @ 0x1] n:   0 pts:   1000 pts_time:1       duration: 33 fmt:yuvj420p\n",
		"[Parsed_showinfo_1 @ 0x1] n:   1 pts:   2000 pts_time:2       duration: 33\n",
	} {
		_, err := log.Write([]byte(chunk))
		require.NoError(t, err)
	}
	require.True(t, log.haveStart)
	require.True(t, log.havePTS)
	require.InDelta(t, -0.021, log.start, 1e-9)
	require.InDelta(t, 1.0, log.pts, 1e-9)
}

func TestKeyframeStillFilter(t *testing.T) {
	sdr := keyframeStillFilter(&models.VideoFile{Width: 3840, Height: 2160}, videoColorMetadata{}, 240)
	require.Equal(t, VideoFilter("scale=-2:240"), sdr)
	portrait := keyframeStillFilter(&models.VideoFile{Width: 2160, Height: 3840}, videoColorMetadata{}, 240)
	require.Equal(t, VideoFilter("scale=240:-2"), portrait)
	require.Equal(t, VideoFilter(""), keyframeStillFilter(&models.VideoFile{Width: 320, Height: 180}, videoColorMetadata{}, 240))
	hdr := string(keyframeStillFilter(&models.VideoFile{Width: 3840, Height: 2160}, videoColorMetadata{Transfer: colorTransferHLG}, 240))
	require.True(t, strings.HasPrefix(hdr, "setparams=color_trc=arib-std-b67"), hdr)
	require.Less(t, strings.Index(hdr, "scale=-2:240"), strings.Index(hdr, "tonemap="), "scale before tone mapping")
}
