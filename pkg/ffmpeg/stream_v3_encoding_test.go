package ffmpeg

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestLiveHLSEncoding(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		rate          float64
		level, peak   int
	}{
		{"HD", 1920, 1080, 30, 41, 8_000_000},
		{"HD 60", 1920, 1080, 59.94, 42, 12_000_000},
		{"UHD", 3840, 2160, 29.97, 51, 24_000_000},
		{"UHD 60", 3840, 2160, 59.94, 52, 36_000_000},
		{"portrait UHD 60", 2160, 3840, 59.94, 52, 36_000_000},
		{"wide frame dimension", 6000, 192, 30, 51, 8_000_000},
		{"unknown", 0, 0, math.NaN(), 41, 8_000_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoding := liveHLSEncoding(test.width, test.height, test.rate)
			require.Equal(t, test.level, encoding.level)
			require.Equal(t, test.peak, encoding.videoBitrate)
		})
	}
}

type encodingTestConfig struct {
	rotationTestStreamConfig
	maxResolution models.StreamingResolutionEnum
	outputArgs    []string
}

func (c encodingTestConfig) GetMaxStreamingTranscodeSize() models.StreamingResolutionEnum {
	return c.maxResolution
}

func (c encodingTestConfig) GetLiveTranscodeOutputArgs() []string { return c.outputArgs }

func TestV3HLSTranscodeManifestUsesOutputEncoding(t *testing.T) {
	vf := &models.VideoFile{Width: 2160, Height: 3840, FrameRate: 59.94, AudioCodec: "opus", BitRate: 1000}
	sm := &StreamManager{config: encodingTestConfig{maxResolution: models.StreamingResolutionEnumFullHd}}
	for _, test := range []struct {
		resolution, dimensions, codec, bandwidth string
	}{
		{"", "1080x1920", "avc1.64002a", "19028000"},
		{"ORIGINAL", "2160x3840", "avc1.640034", "56828000"},
	} {
		r := httptest.NewRequest("GET", "/stream.master.m3u8", nil)
		w := httptest.NewRecorder()
		serveV3HLSMasterManifest(sm, w, r, vf, test.resolution)
		body := w.Body.String()
		require.Contains(t, body, "RESOLUTION="+test.dimensions)
		require.Contains(t, body, "CODECS=\""+test.codec+",mp4a.40.2\"")
		require.Contains(t, body, "BANDWIDTH="+test.bandwidth+",")
	}
}

// Exercise the encoder and muxer with the real output argument assembly.
// In particular, bitrate/level options after the output URL are silently
// ignored by FFmpeg and would pass a test that merely found them in argv.
func TestV3HLSBoundedEncodingAfterSeek(t *testing.T) {
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
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	output, err := exec.CommandContext(ctx, encoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=59.94", "-frames:v", "360", "-c:v", "libx264", "-preset", "ultrafast", source).CombinedOutput()
	require.NoError(t, err, "%s", output)
	stream := &v3RunningStream{
		streamType: V3StreamTypeHLS,
		vf:         &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: 640, Height: 360, FrameRate: 59.94, Duration: 360 / 59.94},
		outputDir:  dir,
	}
	stream.sourceInfoOnce.Do(func() {})
	sm := &StreamManager{encoder: NewEncoder(encoder), config: encodingTestConfig{outputArgs: []string{"-metadata", "comment=live-output-option"}}, context: ctx}
	args := stream.makeStreamArgs(sm, 1)
	require.True(t, strings.HasSuffix(args[len(args)-1], "playlist_%v.m3u8"))
	output, err = exec.CommandContext(ctx, encoder, args...).CombinedOutput()
	require.NoError(t, err, "%s", output)
	init, err := os.ReadFile(filepath.Join(dir, ".init_video.mp4"))
	require.NoError(t, err)
	media := append([]byte(nil), init...)
	for _, name := range []string{".video_1.m4s", ".video_2.m4s"} {
		segment, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		peak := float64(len(segment)*8) / hlsSegmentDuration(59.94)
		require.Less(t, peak, float64(liveHLSEncoding(640, 360, 59.94).bandwidth(false)))
		media = append(media, segment...)
	}
	joined := filepath.Join(dir, "joined.mp4")
	require.NoError(t, os.WriteFile(joined, media, 0o600))
	output, err = exec.CommandContext(ctx, probe, "-v", "error", "-count_frames", "-show_entries", "stream=level,width,height,nb_read_frames:frame=pts_time", "-of", "json", joined).Output()
	require.NoError(t, err)
	var decoded struct {
		Streams []struct {
			Level, Width, Height int
			Frames               string `json:"nb_read_frames"`
		}
		Frames []struct {
			PTS string `json:"pts_time"`
		}
	}
	require.NoError(t, json.Unmarshal(output, &decoded))
	require.Len(t, decoded.Streams, 1)
	require.Equal(t, 41, decoded.Streams[0].Level)
	require.Equal(t, 640, decoded.Streams[0].Width)
	require.Equal(t, 360, decoded.Streams[0].Height)
	require.Equal(t, "240", decoded.Streams[0].Frames)
	require.Len(t, decoded.Frames, 240)
	origin, err := strconv.ParseFloat(decoded.Frames[0].PTS, 64)
	require.NoError(t, err)
	require.InDelta(t, hlsSegmentDuration(59.94), origin, 4/59.94)
	for i, frame := range decoded.Frames {
		pts, err := strconv.ParseFloat(frame.PTS, 64)
		require.NoError(t, err)
		require.InDelta(t, origin+float64(i)/59.94, pts, 0.0001)
	}
}
