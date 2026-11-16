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
	"slices"
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
	require.NotEmpty(t, log.frames, "%s", out)
	return int64(math.Round(float64(log.frameTimeUS(0))/1000 - log.start*1000))
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

func getKeyframeStills(t *testing.T, sm *StreamManager, vf *models.VideoFile, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	sm.ServeKeyframeStills(w, httptest.NewRequest("GET", "/scene/s/keyframes/stills?"+query, nil), vf)
	return w
}

// splitKeyframeStills checks a batch response's framing and returns each
// slot's bytes.
func splitKeyframeStills(t *testing.T, w *httptest.ResponseRecorder, wantMS []int64) [][]byte {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	require.Equal(t, "X-Keyframe-Times, X-Keyframe-Lengths", w.Header().Get("Access-Control-Expose-Headers"))
	want := make([]string, len(wantMS))
	for i, ms := range wantMS {
		want[i] = formatKeyframeTime(ms)
	}
	require.Equal(t, strings.Join(want, ","), w.Header().Get("X-Keyframe-Times"))
	lengths := strings.Split(w.Header().Get("X-Keyframe-Lengths"), ",")
	require.Len(t, lengths, len(wantMS))
	body := w.Body.Bytes()
	require.Equal(t, strconv.Itoa(len(body)), w.Header().Get("Content-Length"))
	slots := make([][]byte, len(lengths))
	offset := 0
	for i, length := range lengths {
		n, err := strconv.Atoi(length)
		require.NoError(t, err)
		require.LessOrEqual(t, offset+n, len(body))
		slots[i] = body[offset : offset+n]
		offset += n
	}
	require.Equal(t, len(body), offset)
	return slots
}

// requireSameImage compares two JPEGs of the same frame, allowing for
// encoder rounding.
func requireSameImage(t *testing.T, want, got []byte, width, height int) {
	t.Helper()
	wantImage, err := jpeg.Decode(bytes.NewReader(want))
	require.NoError(t, err)
	gotImage, err := jpeg.Decode(bytes.NewReader(got))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, width, height), gotImage.Bounds())
	require.Equal(t, wantImage.Bounds(), gotImage.Bounds())
	require.Less(t, meanLumaDifference(wantImage, gotImage), 1.0)
}

func meanLumaDifference(a, b image.Image) float64 {
	total := 0.0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			total += math.Abs(float64(ar+ag+ab)-float64(br+bg+bb)) / 3 / 257
		}
	}
	return total / float64(bounds.Dx()*bounds.Dy())
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

			singles := map[int64][]byte{}
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
				singles[ms] = w.Body.Bytes()

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

			// Neighbouring keyframes differ, so the image comparisons below
			// identify frames.
			first := int64(math.Round(index.Times[0] * 1000))
			require.Greater(t, meanLumaDifference(decodeJPEG(t, singles[first]), decodeJPEG(t, singles[first+1000])), 2.0)

			// A batch returns, per slot and in request order, the frame the
			// single route returns: keyframe times, mid-GOP times, a repeated
			// time and a time before the first keyframe.
			var times []string
			var wantMS []int64
			for i := len(index.Times) - 1; i >= 0; i-- {
				ms := int64(math.Round(index.Times[i] * 1000))
				times = append(times, formatKeyframeTime(ms), formatKeyframeTime(ms+499))
				wantMS = append(wantMS, ms, ms)
			}
			times = append(times, "0", formatKeyframeTime(first))
			wantMS = append(wantMS, first, first)
			started := time.Now()
			w = getKeyframeStills(t, sm, vf, "t="+strings.Join(times, ",")+"&v="+index.Version)
			elapsed := time.Since(started)
			slots := splitKeyframeStills(t, w, wantMS)
			require.Equal(t, "private, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
			for i, ms := range wantMS {
				requireSameImage(t, singles[ms], slots[i], 426, 240)
			}
			t.Logf("%s batch of %d slots (%d keyframes): %d bytes in %d ms", fixture.name, len(wantMS), len(index.Times), w.Body.Len(), elapsed.Milliseconds())

			w = getKeyframeStills(t, sm, vf, "t=1.5,2.5&v=stale")
			splitKeyframeStills(t, w, []int64{fixture.offsetMS + 1000, fixture.offsetMS + 2000})
			require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
		})
	}
}

func TestKeyframeStillsReportFailedSlots(t *testing.T) {
	encoder, probe := keyframeTestTools(t)
	fixture := keyframeFixtures(t, encoder, probe)[0]
	sm := newKeyframeTestManager(t, encoder, probe)
	vf := keyframeTestVideo(fixture.path, 640, 360)
	index := getKeyframeIndex(t, sm, vf)
	src, err := newKeyframeSource(vf)
	require.NoError(t, err)
	scanned, ok := sm.keyframes.indexes.Get(src.indexKey)
	require.True(t, ok)
	// Index entries that are not decodable keyframes: one between two real
	// keyframes and one past the end of the file.
	sm.keyframes.indexes.Add(src.indexKey, &keyframeIndex{times: []int64{0, 1000, 2500, 4000, 9000}, startUS: scanned.startUS, color: scanned.color})

	w := getKeyframeStills(t, sm, vf, "t=4,2.5,9.5,1&v="+index.Version)
	slots := splitKeyframeStills(t, w, []int64{4000, 2500, 9000, 1000})
	require.Equal(t, "no-cache", w.Header().Get("Cache-Control"), "a partial response is not immutable")
	require.NotEmpty(t, slots[0])
	require.Empty(t, slots[1])
	require.Empty(t, slots[2])
	requireSameImage(t, getKeyframe(t, sm, vf, "t=1").Body.Bytes(), slots[3], 426, 240)
	requireSameImage(t, getKeyframe(t, sm, vf, "t=4").Body.Bytes(), slots[0], 426, 240)

	// Nothing to return is a failure.
	require.Equal(t, http.StatusInternalServerError, getKeyframeStills(t, sm, vf, "t=9.5,2.5").Code)
}

func decodeJPEG(t *testing.T, data []byte) image.Image {
	t.Helper()
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return decoded
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
	sixteen := strings.TrimSuffix(strings.Repeat("1,", keyframeBatchMaxStills), ",")
	for _, query := range []string{"", "t=", "t=1,,2", "t=1,abc", "t=1,-1", "t=,1", "t=1,", "t=" + sixteen + ",1"} {
		require.Equal(t, http.StatusBadRequest, getKeyframeStills(t, sm, vf, query).Code, query)
	}
	// Sixteen times are accepted; this file has no keyframe index.
	require.Equal(t, http.StatusNotFound, getKeyframeStills(t, sm, vf, "t="+sixteen).Code)
	missing := keyframeTestVideo(filepath.Join(t.TempDir(), "missing.mp4"), 640, 360)
	require.Equal(t, http.StatusNotFound, getKeyframe(t, sm, missing, "t=1").Code)
	require.Equal(t, http.StatusNotFound, getKeyframeStills(t, sm, missing, "t=1").Code)
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
	single := getKeyframe(t, sm, vf, "t=1.021&v="+first.Version)
	requireKeyframeJPEG(t, single, fixture.offsetMS+1000, 426, 240)
	// The batch reuses the single's still and stores the others.
	batchMS := []int64{fixture.offsetMS + 3000, fixture.offsetMS + 1000, fixture.offsetMS}
	batch := splitKeyframeStills(t, getKeyframeStills(t, sm, vf, "t=3.1,1.5,0&v="+first.Version), batchMS)
	require.Equal(t, single.Body.Bytes(), batch[1])
	sm.Shutdown()

	// A later process reads all of them from the store without running
	// ffprobe or ffmpeg.
	reopened := newKeyframeTestManager(t, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	reopened.SetKeyframeStore(keyframecache.Open(storePath, KeyframeStoreBudgets()))
	require.Equal(t, first, getKeyframeIndex(t, reopened, vf))
	w := getKeyframe(t, reopened, vf, "t=1.5&v="+first.Version)
	requireKeyframeJPEG(t, w, fixture.offsetMS+1000, 426, 240)
	require.Equal(t, "private, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
	w = getKeyframeStills(t, reopened, vf, "t=0,3.5,1.021&v="+first.Version)
	require.Equal(t, [][]byte{batch[2], batch[0], batch[1]}, splitKeyframeStills(t, w, []int64{batchMS[2], batchMS[0], batchMS[1]}))
	require.Equal(t, "private, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
	require.Zero(t, reopened.keyframes.extractions.inUse())
	// An uncached keyframe needs ffmpeg, which is missing here.
	require.Equal(t, http.StatusInternalServerError, getKeyframeStills(t, reopened, vf, "t=2.5").Code)

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
	// Batches use the same filter.
	batch := splitKeyframeStills(t, getKeyframeStills(t, sm, vf, "t=1,2&v="+index.Version), []int64{1000, 2000})
	requireSameImage(t, w.Body.Bytes(), batch[0], 64, 64)
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
	single := getKeyframe(t, sm, vf, "t=1&v="+index.Version)
	requireKeyframeJPEG(t, single, 1000, width, height)
	// The concat demuxer keeps the display matrix for batches.
	batch := splitKeyframeStills(t, getKeyframeStills(t, sm, vf, "t=1,0&v="+index.Version), []int64{1000, 0})
	requireSameImage(t, single.Body.Bytes(), batch[0], width, height)
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
	require.Zero(t, sm.keyframes.extractions.inUse(), "the slot must be released")
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

// newFakeExtractionManager returns a manager whose ffmpeg only sleeps, with a
// cached two-keyframe index for the returned file.
func newFakeExtractionManager(t *testing.T) (*StreamManager, *models.VideoFile, string) {
	t.Helper()
	script, pids := fakeTool(t, "ffmpeg")
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	sm := NewStreamManager(t.TempDir(), NewEncoder(script), nil, encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	src, err := newKeyframeSource(vf)
	require.NoError(t, err)
	sm.keyframes.indexes.Add(src.indexKey, &keyframeIndex{times: []int64{0, 1000, 2000, 3000}})
	return sm, vf, pids
}

func TestKeyframeStillsCancelFFmpegWithRequest(t *testing.T) {
	sm, vf, pids := newFakeExtractionManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *httptest.ResponseRecorder)
	go func() {
		w := httptest.NewRecorder()
		sm.ServeKeyframeStills(w, httptest.NewRequest("GET", "/scene/s/keyframes/stills?t=0,1,2", nil).WithContext(ctx), vf)
		done <- w
	}()
	require.Eventually(t, func() bool { return len(readPIDs(t, pids)) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, sm.keyframes.extractions.inUse(), "one process for the whole batch")
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish after cancellation")
	}
	require.Len(t, readPIDs(t, pids), 1)
	require.False(t, processRunning(readPIDs(t, pids)[0]), "ffmpeg must be killed with the request")
	require.Zero(t, sm.keyframes.extractions.inUse(), "the slot must be released")
}

func TestKeyframeStillsTimeOut(t *testing.T) {
	sm, vf, pids := newFakeExtractionManager(t)
	sm.keyframes.batchTimeout = 200 * time.Millisecond
	started := time.Now()
	w := getKeyframeStills(t, sm, vf, "t=0,1")
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Less(t, time.Since(started), 3*time.Second)
	require.False(t, processRunning(readPIDs(t, pids)[0]))
	require.Zero(t, sm.keyframes.extractions.inUse())
}

// Batches count as one extraction but leave a slot to single stills.
func TestKeyframeStillsLeaveASlotForSingles(t *testing.T) {
	sm, vf, pids := newFakeExtractionManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	start := func(serve func(http.ResponseWriter, *http.Request, *models.VideoFile), target string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			serve(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil).WithContext(ctx), vf)
		}()
	}
	batches := keyframeStillConcurrency - keyframeBatchReservedSlots
	for i := range batches {
		start(sm.ServeKeyframeStills, fmt.Sprintf("/scene/s/keyframes/stills?t=%d,3", i))
	}
	require.Eventually(t, func() bool { return len(readPIDs(t, pids)) == batches }, 5*time.Second, 10*time.Millisecond)

	w := getKeyframeStills(t, sm, vf, "t=1,2")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	require.Len(t, readPIDs(t, pids), batches, "a refused batch must not start ffmpeg")

	// The reserved slot still serves a single still.
	start(sm.ServeKeyframe, "/scene/s/keyframe?t=2")
	require.Eventually(t, func() bool { return len(readPIDs(t, pids)) == keyframeStillConcurrency }, 5*time.Second, 10*time.Millisecond)
	w = getKeyframe(t, sm, vf, "t=1")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))

	cancel()
	wg.Wait()
	for _, pid := range readPIDs(t, pids) {
		require.False(t, processRunning(pid))
	}
	require.Zero(t, sm.keyframes.extractions.inUse())
}

func TestKeyframeStillsWaitBrieflyForTheIndex(t *testing.T) {
	script, pids := fakeTool(t, "ffprobe")
	path := filepath.Join(t.TempDir(), "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	vf := keyframeTestVideo(path, 640, 360)
	sm := NewStreamManager(t.TempDir(), nil, NewFFProbe(script), encodingTestConfig{}, fsutil.NewReadLockManager())
	t.Cleanup(sm.Shutdown)
	sm.keyframes.batchIndexWait = 100 * time.Millisecond

	w := getKeyframeStills(t, sm, vf, "t=1,2")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "2", w.Header().Get("Retry-After"))
	// The scan continues for the retry.
	require.Len(t, readPIDs(t, pids), 1)
	require.True(t, processRunning(readPIDs(t, pids)[0]))
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

	require.Equal(t, int64(11_378_667), decoded.startUS)
	negative, err := newKeyframeIndex([]float64{0, 1}, -0.021, videoColorMetadata{})
	require.NoError(t, err)
	decoded, err = decodeKeyframeIndex(encodeKeyframeIndex(negative))
	require.NoError(t, err)
	require.Equal(t, int64(-21_000), decoded.startUS)

	_, err = newKeyframeIndex(nil, 0, videoColorMetadata{})
	require.ErrorIs(t, err, errNoKeyframes)
	const enc = keyframeIndexEncoding
	for _, invalid := range [][]byte{
		nil,
		{enc},
		// The previous encoding, which had no container start.
		{1, 0, 0, 0, 0, 1, 0},
		// No keyframes; a repeated keyframe; trailing data.
		{enc, 0, 0, 0, 0, 0, 0},
		{enc, 0, 0, 0, 0, 0, 2, 5, 0},
		{enc, 0, 0, 0, 0, 0, 1, 0, 0},
	} {
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
	require.InDelta(t, -0.021, log.start, 1e-9)
	require.Len(t, log.frames, 2)
	require.Equal(t, int64(1_000_000), log.frameTimeUS(0))

	// ffmpeg before 7.0 prints pts_time with six significant digits; the
	// integer pts and time base keep microseconds.
	log = &keyframeStillLog{}
	_, _ = log.Write([]byte("[Parsed_showinfo_0 @ 0x1] config in time_base: 1/90000, frame_rate: 30/1\n" +
		"[Parsed_showinfo_0 @ 0x1] n:   0 pts:111111111 pts_time:1234.57 duration: 3000\n"))
	require.Equal(t, int64(1_234_567_900), log.frameTimeUS(0))
}

func TestKeyframeBatchLogMatchesFramesToSegments(t *testing.T) {
	log := &keyframeStillLog{segments: map[int]int64{}}
	_, _ = log.Write([]byte("[Parsed_showinfo_3 @ 0x2] config in time_base: 1/1000, frame_rate: 30/1\n" +
		"[Parsed_metadata_2 @ 0x1] frame:0    pts:131     pts_time:0.131\n" +
		"[Parsed_metadata_2 @ 0x1] lavf.concatdec.start_time=0\n" +
		"[h264 @ 0x3] unrelated decoder message\n" +
		"[Parsed_showinfo_3 @ 0x2] n:   0 pts:    131 pts_time:0.131 duration: 33\n" +
		"[Parsed_metadata_2 @ 0x1] frame:1    pts:264     pts_time:0.264\n" +
		"[Parsed_metadata_2 @ 0x1] lavf.concatdec.start_time=133000\n" +
		"[Parsed_showinfo_3 @ 0x2] n:   1 pts:    264 pts_time:0.264 duration: 33\n"))
	require.False(t, log.invalid)
	require.Len(t, log.frames, 2)
	require.Equal(t, map[int]int64{0: 0, 1: 133_000}, log.segments)
	require.Equal(t, int64(264_000), log.frameTimeUS(1))
	require.Equal(t, "[h264 @ 0x3] unrelated decoder message", log.errorText())

	// A frame logged out of sequence cannot be matched to its image.
	_, _ = log.Write([]byte("[Parsed_showinfo_3 @ 0x2] n:   3 pts:    400 pts_time:0.4\n"))
	require.True(t, log.invalid)
}

func TestKeyframeConcatScriptQuotesPaths(t *testing.T) {
	script, err := keyframeConcatScript("/media/it's here.mp4", []int64{0, 1_021_000})
	require.NoError(t, err)
	require.Equal(t, "ffconcat version 1.0\n"+
		"file 'file:/media/it'\\''s here.mp4'\ninpoint -0.131000\noutpoint 0.002000\n"+
		"file 'file:/media/it'\\''s here.mp4'\ninpoint 0.890000\noutpoint 1.023000\n", string(script))
	_, err = keyframeConcatScript("/media/two\nlines.mp4", []int64{0})
	require.Error(t, err)
}

func TestSplitJPEGStream(t *testing.T) {
	encode := func(shade uint8) []byte {
		img := image.NewGray(image.Rect(0, 0, 16, 16))
		for i := range img.Pix {
			img.Pix[i] = shade
		}
		var out bytes.Buffer
		require.NoError(t, jpeg.Encode(&out, img, nil))
		return out.Bytes()
	}
	first, second := encode(10), encode(200)
	// An application segment whose payload contains an end-of-image marker.
	tricky := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0x00, 0x06, 0xff, 0xd9, 0xff, 0xd9}, first[2:]...)
	images, err := splitJPEGStream(slices.Concat(first, tricky, second))
	require.NoError(t, err)
	require.Equal(t, [][]byte{first, tricky, second}, images)
	for _, data := range [][]byte{first[:len(first)-1], append(slices.Clone(first), 0x00), {0xff, 0xd8}} {
		_, err := splitJPEGStream(data)
		require.Error(t, err)
	}
	empty, err := splitJPEGStream(nil)
	require.NoError(t, err)
	require.Empty(t, empty)
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
