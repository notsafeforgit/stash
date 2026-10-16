package previewimage

import (
	"bytes"
	"context"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stretchr/testify/require"
)

func TestPickerRange(t *testing.T) {
	for _, req := range []PickerRequest{{Start: -1, End: 2}, {Start: 1, End: 1}, {Start: 3, End: 2}, {End: 11}, {End: math.Inf(1)}, {Start: math.NaN(), End: 2}} {
		require.False(t, req.Valid(10), "%+v", req)
	}
	require.True(t, (PickerRequest{End: 10}).Valid(10))
}

func TestPickerOutputLimitCannotBeBypassedByCopy(t *testing.T) {
	b := &pickerBuffer{limit: 4}
	_, err := io.Copy(b, bytes.NewBufferString("too much output"))
	require.Error(t, err)
	require.LessOrEqual(t, b.buffer.Len(), 4)
}

func TestPickerUsesDecodedFrameTimesAndRetainsNoFiles(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "vfr.mkv")
	// Uneven frame spacing and a nonzero source start exercise source-time
	// coordinates rather than the average FPS approximation used by players.
	out, err := exec.Command(ffmpegPath, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=96x64:rate=30:duration=4",
		"-vf", "select='not(mod(n,3))+not(mod(n,7))',setpts=PTS+2/TB", "-vsync", "0", "-c:v", "ffv1", path).CombinedOutput()
	require.NoError(t, err, "%s", out)
	video, err := ffmpeg.NewFFProbe(probePath).NewVideoFile(path)
	require.NoError(t, err)
	e := Encoder{FFmpeg: ffmpeg.NewEncoder(ffmpegPath)}
	frames, err := e.Picker(context.Background(), video, PickerRequest{Start: 0.137, End: video.VideoStreamDuration, Frames: true})
	require.NoError(t, err)
	require.Len(t, frames, PickerFrameCount)
	var differentGaps bool
	for i, frame := range frames {
		if i > 1 {
			differentGaps = differentGaps || math.Abs((frame.At-frames[i-1].At)-(frames[1].At-frames[0].At)) > 0.01
		}
		// A later independent selection must reproduce the displayed frame.
		again, err := e.pickerFrames(context.Background(), video, frame.At, 1)
		require.NoError(t, err)
		require.Equal(t, frame.JPEG, again[0].JPEG, "frame %d at %.9f", i, frame.At)
		im, err := jpeg.Decode(bytes.NewReader(frame.JPEG))
		require.NoError(t, err)
		require.LessOrEqual(t, im.Bounds().Dx(), 480)
		require.LessOrEqual(t, im.Bounds().Dy(), 480)
	}
	require.True(t, differentGaps, "fixture must have variable frame spacing")
	samples, err := e.Picker(context.Background(), video, PickerRequest{End: 3})
	require.NoError(t, err)
	require.Len(t, samples, 9)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.Picker(ctx, video, PickerRequest{End: 3})
	require.Error(t, err)
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1, "only the input video may remain")
}

func TestPickerInterframeHDRAndCoverSelection(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	encoders, _ := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	for _, tc := range []struct {
		codec, transfer, pixel string
		extra                  []string
	}{
		{"libx264", "bt709", "yuv420p", []string{"-preset", "ultrafast"}},
		{"libaom-av1", "arib-std-b67", "yuv420p10le", []string{"-cpu-used", "8"}},
		{"libx265", "smpte2084", "yuv420p10le", []string{"-preset", "ultrafast", "-x265-params", "pools=1"}},
	} {
		t.Run(tc.codec, func(t *testing.T) {
			if !strings.Contains(string(encoders), tc.codec) {
				t.Skip("optional encoder unavailable")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "interframe.mkv")
			primaries, matrix := "bt709", "bt709"
			if IsHDR(tc.transfer) {
				primaries, matrix = "bt2020", "bt2020nc"
			}
			args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=96x128:rate=30000/1001:duration=2",
				"-vf", "format=" + tc.pixel, "-color_primaries", primaries, "-color_trc", tc.transfer, "-colorspace", matrix,
				"-c:v", tc.codec, "-g", "90", "-threads", "2"}
			args = append(args, tc.extra...)
			out, err := exec.Command(ffmpegPath, append(args, path)...).CombinedOutput()
			require.NoError(t, err, "%s", out)
			video, err := ffmpeg.NewFFProbe(probePath).NewVideoFile(path)
			require.NoError(t, err)
			e := Encoder{FFmpeg: ffmpeg.NewEncoder(ffmpegPath)}
			frames, err := e.Picker(context.Background(), video, PickerRequest{Start: 0.739, End: video.VideoStreamDuration, Frames: true})
			require.NoError(t, err)
			require.Len(t, frames, PickerFrameCount)
			for _, i := range []int{0, 1, 12, 24} {
				frame := frames[i]
				again, err := e.pickerFrames(context.Background(), video, frame.At, 1)
				require.NoError(t, err)
				require.Equal(t, frame.JPEG, again[0].JPEG)
				// Final cover generation has a separate high-bit-depth path.
				// Its SDR frame must depict the same instant, not the next frame.
				coverDir := t.TempDir()
				require.NoError(t, e.extractFrame(context.Background(), Request{Video: video, At: frame.At}, coverDir, false))
				coverData, err := os.ReadFile(filepath.Join(coverDir, "sdr.png"))
				require.NoError(t, err)
				cover, err := png.Decode(bytes.NewReader(coverData))
				require.NoError(t, err)
				preview, err := jpeg.Decode(bytes.NewReader(frame.JPEG))
				require.NoError(t, err)
				require.Equal(t, cover.Bounds(), preview.Bounds())
				var difference float64
				for y := 0; y < cover.Bounds().Dy(); y++ {
					for x := 0; x < cover.Bounds().Dx(); x++ {
						cr, cg, cb, _ := cover.At(x, y).RGBA()
						pr, pg, pb, _ := preview.At(x, y).RGBA()
						difference += math.Abs(float64(cr)-float64(pr)) + math.Abs(float64(cg)-float64(pg)) + math.Abs(float64(cb)-float64(pb))
					}
				}
				// JPEG chroma subsampling differs around saturated test-pattern
				// edges. Overall colour/frame content must still agree.
				require.Less(t, difference/float64(3*cover.Bounds().Dx()*cover.Bounds().Dy())/257, 12.0)
			}
			endFrames, err := e.Picker(context.Background(), video, PickerRequest{Start: 1.95, End: video.VideoStreamDuration, Frames: true})
			require.NoError(t, err)
			require.Less(t, len(endFrames), PickerFrameCount)
		})
	}
}
