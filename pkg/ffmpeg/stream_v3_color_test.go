package ffmpeg

import (
	"context"
	"encoding/binary"
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

func TestHLSColorMetadata(t *testing.T) {
	hlg := colorTransferHLG
	vf := &models.VideoFile{ColorTransfer: &hlg}
	require.Equal(t, "HLG", sourceColorMetadata(vf, nil).hlsVideoRange())
	require.Equal(t, "HLG", sourceColorMetadata(vf, &VideoFile{}).hlsVideoRange())
	require.Equal(t, "PQ", sourceColorMetadata(vf, &VideoFile{ColorTransfer: colorTransferPQ}).hlsVideoRange())
	require.Equal(t, "SDR", sourceColorMetadata(vf, &VideoFile{ColorTransfer: "bt709"}).hlsVideoRange())

	for _, test := range []struct {
		codec, mime, profile, pixelFormat string
		level                             int
		want                              string
	}{
		{"av1", "av01.0.13M.10.0.111.09.18.09.0", "Main", "yuv420p10le", 13, "av01.0.13M.10.0.111.09.18.09.0"},
		{"av1", "", "Main", "yuv420p10le", 13, "av01.0.13M.10"},
		{Hevc, "hev1.2.4.L153.B0", "Main 10", "yuv420p10le", 153, "hvc1.2.4.L153.B0"},
		{Hevc, "", "Main 10", "yuv420p10le", 153, "hvc1.2.4.L153.B0"},
		{H264, "avc1.64002a", "High", "yuv420p", 42, "avc1.64002a"},
		{H264, "avc1.64002a\"\n#EXT-X-ENDLIST", "High", "yuv420p", 42, "avc1.64002a"},
		{H264, "av01.0.13M.10", "High", "yuv420p", 42, "avc1.64002a"},
	} {
		probe := &VideoFile{VideoStream: &FFProbeStream{MimeCodecString: test.mime, Profile: test.profile, PixFmt: test.pixelFormat, Level: test.level}}
		require.Equal(t, test.want, videoCodecString(test.codec, probe))
	}
}

func TestHDRStreamRequiresHDRSourceAndMain10Encoder(t *testing.T) {
	hlg := colorTransferHLG
	vf := &models.VideoFile{ColorTransfer: &hlg}
	sm := &StreamManager{encoder: &FFMpeg{hwCodecSupport: []VideoCodec{VideoCodecV265}}, config: hdrTestConfig{}}
	require.False(t, sm.CanStreamHDR(vf), "8-bit HEVC capability does not establish Main 10 support")
	sm.encoder.hwCodecSupport = []VideoCodec{VideoCodecV265HDR}
	require.True(t, sm.CanStreamHDR(vf))
	require.False(t, sm.CanStreamHDR(&models.VideoFile{}))
	sm.config = rotationTestStreamConfig{}
	require.False(t, sm.CanStreamHDR(vf), "respect disabled hardware encoding")
	require.False(t, (*StreamManager)(nil).CanStreamHDR(vf))
}

type hdrTestConfig struct{ rotationTestStreamConfig }

func (hdrTestConfig) GetTranscodeHardwareAcceleration() bool { return true }

func TestHLSHDRHardwareFailureKeepsToneMapping(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "ffmpeg")
	log := filepath.Join(dir, "calls")
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+log+"'\nexit 1\n"), 0o700))
	hlg := colorTransferHLG
	stream := &v3RunningStream{
		streamType: V3StreamTypeHLS,
		vf: &models.VideoFile{BaseFile: &models.BaseFile{Path: "source.mkv"}, Width: 2160, Height: 3840,
			FrameRate: 59.94, ColorTransfer: &hlg},
		outputDir: dir,
	}
	sm := &StreamManager{encoder: &FFMpeg{ffmpeg: wrapper, hwCodecSupport: []VideoCodec{VideoCodecI264}}, config: hdrTestConfig{}, context: context.Background()}
	for _, segment := range []int{0, 50} {
		args := strings.Join(stream.makeStreamArgs(sm, segment), " ")
		require.Contains(t, args, "tonemap=mobius")
		require.Contains(t, args, "hwupload=extra_hw_frames=64")
		require.Contains(t, args, "-c:v h264_qsv")
		require.NotContains(t, args, "-hwaccel ")
		require.NotContains(t, args, "tonemap_opencl")
		require.Contains(t, args, "-color_trc bt709")
	}
	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(calls), "\n"), "a seek must reuse the capability result")
	require.Contains(t, string(calls), "tonemap_opencl", "probe the actual HDR conversion, not just the encoder")
}

// Exercise actual HDR pixels, SDR bitstream/container tags, seek output and
// HDR-preserving copies. Merely adding BT.709 labels must not pass this test.
func TestV3HLSHDRConversionAndCopy(t *testing.T) {
	encoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := func(t *testing.T, program string, args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, program, args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		return out
	}
	for _, transfer := range []string{colorTransferHLG, colorTransferPQ} {
		t.Run(transfer, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "hdr.mkv")
			run(t, encoder, "-v", "error", "-f", "lavfi", "-i", "nullsrc=s=64x64:r=30,format=yuv420p10le,geq=lum=512:cb=512:cr=512,setparams=color_primaries=bt2020:color_trc="+transfer+":colorspace=bt2020nc:range=tv",
				"-t", "4.1", "-c:v", "libx265", "-preset", "ultrafast", "-g", "60", "-x265-params", "log-level=error:pools=1:frame-threads=1:colorprim=bt2020:transfer="+transfer+":colormatrix=bt2020nc:master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400",
				"-color_primaries", "bt2020", "-color_trc", transfer, "-colorspace", "bt2020nc", "-color_range", "tv", source)
			vf := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: 64, Height: 64, FrameRate: 30, VideoCodec: Hevc, Duration: 4.1}
			sm := &StreamManager{encoder: NewEncoder(encoder), ffprobe: NewFFProbe(probePath), config: rotationTestStreamConfig{}, context: ctx}
			inputProbe, err := sm.ffprobe.NewVideoFile(source)
			require.NoError(t, err)
			require.Equal(t, transfer, inputProbe.ColorTransfer)
			for _, segment := range []int{0, 1} {
				outputDir := t.TempDir()
				stream := &v3RunningStream{streamType: V3StreamTypeHLS, vf: vf, outputDir: outputDir}
				args := stream.makeStreamArgs(sm, segment)
				require.Equal(t, transfer, stream.sourceColor.Transfer, "args=%v", args)
				run(t, encoder, args...)
				playlist := filepath.Join(outputDir, "playlist_video.m3u8")
				data := run(t, probePath, "-v", "error", "-select_streams", "v:0", "-read_intervals", "%+#1", "-show_streams", "-show_frames", "-of", "json", playlist)
				var decoded struct{ Streams []FFProbeStream }
				require.NoError(t, json.Unmarshal(data, &decoded))
				require.NotEmpty(t, decoded.Streams)
				out := decoded.Streams[0]
				require.Equal(t, H264, out.CodecName)
				require.Equal(t, "yuv420p", out.PixFmt)
				require.Equal(t, "bt709", out.ColorPrimaries)
				require.Equal(t, "bt709", out.ColorSpace)
				require.Equal(t, "bt709", out.ColorTransfer)
				require.Equal(t, "tv", out.ColorRange)
				require.NotContains(t, string(data), "Mastering display metadata")
				require.NotContains(t, string(data), "Content light level metadata")
				pixels := run(t, encoder, "-v", "error", "-i", playlist, "-frames:v", "1", "-pix_fmt", "yuv420p", "-f", "rawvideo", "-")
				require.Len(t, pixels, 64*64*3/2)
				// Truncating 10-bit 512 to 8-bit produces 128, regardless of
				// the label. Tone mapping must visibly change these pixels.
				require.Greater(t, math.Abs(float64(pixels[0])-128), 8.0)
				require.Greater(t, int(pixels[0]), 16)
				require.Less(t, int(pixels[0]), 235)
			}
			for _, kind := range []*V3StreamType{V3StreamTypeHLSCopyFMP4, V3StreamTypeHLSCopyFMP4AAC} {
				outputDir := t.TempDir()
				stream := &v3RunningStream{streamType: kind, vf: vf, outputDir: outputDir}
				run(t, encoder, stream.makeStreamArgs(sm, 0)...)
				copied, err := sm.ffprobe.NewVideoFile(filepath.Join(outputDir, "playlist_video.m3u8"))
				require.NoError(t, err)
				require.Equal(t, transfer, copied.ColorTransfer)
				require.Equal(t, 10, copied.BitDepth)
				require.Equal(t, "hvc1", copied.VideoStream.CodecTagString)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/stream.fmp4.master.m3u8", nil)
				sm.ServeV3MasterManifest(w, r, kind, vf, "ORIGINAL")
				require.Contains(t, w.Body.String(), "VIDEO-RANGE="+sourceColorMetadata(vf, copied).hlsVideoRange())
				require.Contains(t, w.Body.String(), "CODECS=\"hvc1.2.")
			}
			for _, segment := range []int{0, 1} {
				outputDir := t.TempDir()
				stream := &v3RunningStream{streamType: V3StreamTypeHLSHEVC, vf: vf, outputDir: outputDir}
				args := stream.makeStreamArgs(sm, segment)
				require.NotContains(t, strings.Join(args, " "), "tonemap")
				run(t, encoder, args...)
				playlist := filepath.Join(outputDir, "playlist_video.m3u8")
				hdr, err := sm.ffprobe.NewVideoFile(playlist)
				require.NoError(t, err)
				require.Equal(t, Hevc, hdr.VideoCodec)
				require.Equal(t, 10, hdr.BitDepth)
				require.Equal(t, transfer, hdr.ColorTransfer)
				require.Equal(t, "bt2020", hdr.ColorPrimaries)
				require.Equal(t, "bt2020nc", hdr.ColorSpace)
				require.Equal(t, "hvc1", hdr.VideoStream.CodecTagString)
				require.Equal(t, liveHEVCHLSEncoding(64, 64, 30).level, hdr.VideoStream.Level)
				pixels := run(t, encoder, "-v", "error", "-i", playlist, "-frames:v", "1", "-pix_fmt", "yuv420p10le", "-f", "rawvideo", "-")
				require.InDelta(t, 512, binary.LittleEndian.Uint16(pixels[:2]), 4, "preserve HDR pixels, not only the tags")
				w := httptest.NewRecorder()
				sm.ServeV3MasterManifest(w, httptest.NewRequest("GET", "/stream.hevc.master.m3u8?stream_session=hdr", nil), V3StreamTypeHLSHEVC, vf, "ORIGINAL")
				require.Contains(t, w.Body.String(), "VIDEO-RANGE="+sourceColorMetadata(vf, hdr).hlsVideoRange())
				require.Contains(t, w.Body.String(), "CODECS=\"hvc1.2.4.L90.B0\"")
				require.Contains(t, w.Body.String(), "stream.hevc.m3u8/video.m3u8?stream_session=hdr")
				require.NotContains(t, w.Body.String(), "TYPE=AUDIO")
			}
			w := httptest.NewRecorder()
			serveV3HLSMasterManifest(sm, w, httptest.NewRequest("GET", "/stream.master.m3u8", nil), vf, "ORIGINAL")
			require.Contains(t, w.Body.String(), "VIDEO-RANGE=SDR")
		})
	}
}

type hdrGPUTestConfig struct{ hdrTestConfig }

func (hdrGPUTestConfig) GetLiveTranscodeOutputArgs() []string { return []string{"-t", "8"} }

// Optional deployment-host check using a real HDR clip and Intel GPU. Run the
// test binary inside the image with STASH_TEST_QSV_HDR_SOURCE set to a read-only
// source path. CI exercises software pixels and GPU failure above without a GPU.
func TestV3HLSHDRQSV(t *testing.T) {
	source := os.Getenv("STASH_TEST_QSV_HDR_SOURCE")
	if source == "" {
		t.Skip("STASH_TEST_QSV_HDR_SOURCE and Intel GPU required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	prober := NewFFProbe("ffprobe")
	probe, err := prober.NewVideoFile(source)
	require.NoError(t, err)
	require.True(t, colorMetadataFromProbe(probe).isHDR())
	require.Greater(t, probe.FileDuration, 100.0)
	vf := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: probe.Width, Height: probe.Height,
		FrameRate: probe.FrameRate, VideoCodec: probe.VideoCodec, AudioCodec: probe.AudioCodec, Duration: probe.FileDuration}
	encoder := NewEncoder("ffmpeg")
	encoder.hwCodecSupport = []VideoCodec{VideoCodecI264}
	sm := &StreamManager{encoder: encoder, ffprobe: prober, config: hdrGPUTestConfig{}, context: ctx}
	for _, ceiling := range []int{0, 1080} {
		dir := t.TempDir()
		stream := &v3RunningStream{streamType: V3StreamTypeHLS, vf: vf, outputDir: dir, maxTranscodeSize: ceiling}
		args := stream.makeStreamArgs(sm, 45)
		require.True(t, stream.fullHW, "GPU tone mapping failed its capability check")
		started := time.Now()
		out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		t.Logf("8 seconds of HDR HLS, ceiling %d: %s", ceiling, time.Since(started))
		result, err := prober.NewVideoFile(filepath.Join(dir, "playlist_video.m3u8"))
		require.NoError(t, err)
		require.Equal(t, "bt709", result.ColorTransfer)
		require.Equal(t, "bt709", result.ColorPrimaries)
		require.Equal(t, "bt709", result.ColorSpace)
		require.Equal(t, 8, result.BitDepth)
		w, h := scaledDimensions(vf, ceiling)
		require.Equal(t, w, result.Width)
		require.Equal(t, h, result.Height)
	}
}

type hdrHEVCGPUTestConfig struct{ hdrTestConfig }

func (hdrHEVCGPUTestConfig) GetLiveTranscodeOutputArgs() []string {
	return []string{"-t", "126"}
}

// Deployment check: encode beyond the reported 100-second failure point with
// the same 4K HDR input. Keep generated segments in the test's temporary dir.
func TestV3HLSHDRHEVCVAAPI(t *testing.T) {
	source := os.Getenv("STASH_TEST_HDR_SOURCE")
	if source == "" {
		t.Skip("STASH_TEST_HDR_SOURCE and VAAPI HEVC hardware required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	prober := NewFFProbe("ffprobe")
	probe, err := prober.NewVideoFile(source)
	require.NoError(t, err)
	require.True(t, colorMetadataFromProbe(probe).isHDR())
	vf := &models.VideoFile{BaseFile: &models.BaseFile{Path: source}, Width: probe.Width, Height: probe.Height,
		FrameRate: probe.FrameRate, VideoCodec: probe.VideoCodec, AudioCodec: probe.AudioCodec, Duration: probe.FileDuration}
	encoder := NewEncoder("ffmpeg")
	encoder.initHWSupport(ctx)
	require.NotNil(t, encoder.hwCodecHDRHLSCompatible(), "Main 10 encoding must pass the startup capability test")
	sm := &StreamManager{encoder: encoder, ffprobe: prober, config: hdrHEVCGPUTestConfig{}, context: ctx}
	dir := t.TempDir()
	stream := &v3RunningStream{streamType: V3StreamTypeHLSHEVC, vf: vf, outputDir: dir}
	args := stream.makeStreamArgs(sm, 0)
	require.True(t, stream.fullHW, "10-bit GPU pipeline failed its capability check")
	started := time.Now()
	out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Logf("126 seconds of 4K HDR HEVC HLS: %s", time.Since(started))
	playlist := filepath.Join(dir, "playlist_video.m3u8")
	result, err := prober.NewVideoFile(playlist)
	require.NoError(t, err)
	require.Equal(t, Hevc, result.VideoCodec)
	require.Equal(t, probe.ColorTransfer, result.ColorTransfer)
	require.Equal(t, probe.ColorPrimaries, result.ColorPrimaries)
	require.Equal(t, probe.ColorSpace, result.ColorSpace)
	require.Equal(t, 10, result.BitDepth)
	require.Equal(t, "hvc1", result.VideoStream.CodecTagString)
	require.Equal(t, vf.Width, result.Width)
	require.Equal(t, vf.Height, result.Height)
	require.Equal(t, liveHEVCHLSEncoding(vf.Width, vf.Height, vf.FrameRate).level, result.VideoStream.Level)
	// Inspect every fragment's declared duration and decode across the area
	// where the phone stutters; this is server output, not an iOS validation.
	contents, err := os.ReadFile(playlist)
	require.NoError(t, err)
	count := 0
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			value, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ","), 64)
			require.NoError(t, err)
			require.Greater(t, value, 0.0)
			require.LessOrEqual(t, value, hlsSegmentDuration(vf.FrameRate)+0.02)
			count++
		}
	}
	require.Greater(t, count, 60)
	out, err = exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-ss", "96", "-i", playlist, "-t", "12", "-an", "-f", "null", "-").CombinedOutput()
	require.NoError(t, err, "%s", out)
}
