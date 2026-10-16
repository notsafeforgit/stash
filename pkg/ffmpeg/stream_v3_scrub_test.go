package ffmpeg

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func parsePTS(t *testing.T, value string) float64 {
	t.Helper()
	pts, err := strconv.ParseFloat(value, 64)
	require.NoError(t, err)
	return pts
}

func TestV3ScrubManifestIsLowResolutionAndSilent(t *testing.T) {
	vf := &models.VideoFile{Width: 2160, Height: 3840, FrameRate: 59.94, AudioCodec: "opus"}
	sm := &StreamManager{config: encodingTestConfig{maxResolution: models.StreamingResolutionEnumOriginal}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/scene/test/stream.scrub.master.m3u8?stream_session=preview&resolution=LOW", nil)
	sm.ServeV3MasterManifest(w, r, V3StreamTypeHLSScrub, vf, "ORIGINAL")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "RESOLUTION=240x426")
	require.Contains(t, w.Body.String(), "stream.scrub.m3u8/video.m3u8?")
	require.NotContains(t, w.Body.String(), "AUDIO")
	require.NotContains(t, w.Body.String(), "mp4a")
	require.Equal(t, "opus", vf.AudioCodec)
}

func TestV3ScrubEncodeKeepsFrameCadenceWithoutAudio(t *testing.T) {
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
	output, err := exec.CommandContext(ctx, encoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=59.94:duration=4.01", "-f", "lavfi", "-i", "sine=duration=4.01", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", source).CombinedOutput()
	require.NoError(t, err, "%s", output)
	stream := &v3RunningStream{streamType: V3StreamTypeHLSScrub,
		vf:        &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: 640, Height: 360, FrameRate: 59.94, AudioCodec: "aac"},
		outputDir: dir, maxTranscodeSize: 240,
	}
	stream.sourceInfoOnce.Do(func() {})
	sm := &StreamManager{context: ctx, encoder: NewEncoder(encoder), config: encodingTestConfig{}}
	output, err = exec.CommandContext(ctx, encoder, stream.makeStreamArgs(sm, 1)...).CombinedOutput()
	require.NoError(t, err, "%s", output)
	init, err := os.ReadFile(filepath.Join(dir, ".init_video.mp4"))
	require.NoError(t, err)
	segment, err := os.ReadFile(filepath.Join(dir, ".video_1.m4s"))
	require.NoError(t, err)
	joined := filepath.Join(dir, "preview.mp4")
	require.NoError(t, os.WriteFile(joined, append(init, segment...), 0600))
	output, err = exec.CommandContext(ctx, probe, "-v", "error", "-count_frames", "-show_entries", "stream=codec_type,width,height,nb_read_frames:frame=pts_time", "-of", "json", joined).Output()
	require.NoError(t, err)
	var decoded struct {
		Streams []struct {
			Width, Height int
			Frames        string `json:"nb_read_frames"`
			CodecType     string `json:"codec_type"`
		}
		Frames []struct {
			PTS string `json:"pts_time"`
		}
	}
	require.NoError(t, json.Unmarshal(output, &decoded))
	require.Len(t, decoded.Streams, 1)
	require.Equal(t, "video", decoded.Streams[0].CodecType)
	require.Equal(t, 426, decoded.Streams[0].Width)
	require.Equal(t, 240, decoded.Streams[0].Height)
	require.Equal(t, "120", decoded.Streams[0].Frames)
	require.Len(t, decoded.Frames, 120)
	require.InDelta(t, hlsSegmentDuration(59.94), parsePTS(t, decoded.Frames[0].PTS), 0.0001)
	for i := 1; i < len(decoded.Frames); i++ {
		require.InDelta(t, 1/59.94, parsePTS(t, decoded.Frames[i].PTS)-parsePTS(t, decoded.Frames[i-1].PTS), 0.0001)
	}
	_, err = os.Stat(filepath.Join(dir, ".init_audio.mp4"))
	require.True(t, os.IsNotExist(err))
}
