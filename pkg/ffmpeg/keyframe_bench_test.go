package ffmpeg

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// BenchmarkKeyframeStill measures the in-server still path (process start,
// keyframe-only software decode, scale/tone map, JPEG encode) on 1 s GOP
// fixtures. Run with: go test ./pkg/ffmpeg -run '^$' -bench KeyframeStill
func BenchmarkKeyframeStill(b *testing.B) {
	encoder, _ := keyframeTestTools(b)
	encoders := runKeyframeTool(b, encoder, "-hide_banner", "-encoders")
	dir := b.TempDir()
	pq := videoColorMetadata{Range: "tv", Space: "bt2020nc", Transfer: colorTransferPQ, Primaries: "bt2020"}
	type fixture struct {
		name          string
		encoderName   string
		width, height int
		color         videoColorMetadata
		args          []string
	}
	fixtures := []fixture{
		{name: "1080p-h264", encoderName: "libx264", width: 1920, height: 1080, args: []string{"-c:v", "libx264", "-preset", "veryfast", "-g", "30"}},
		{name: "1080p-av1", encoderName: "libsvtav1", width: 1920, height: 1080, args: []string{"-c:v", "libsvtav1", "-preset", "10", "-g", "30"}},
		{name: "2160p-hevc-pq", encoderName: "libx265", width: 3840, height: 2160, color: pq, args: []string{"-vf", "format=yuv420p10le", "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "keyint=30:min-keyint=30:scenecut=0:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc:log-level=error", "-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc"}},
	}
	f := NewEncoder(encoder)
	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			if !bytes.Contains(encoders, []byte(fixture.encoderName)) {
				b.Skipf("%s is unavailable", fixture.encoderName)
			}
			path := filepath.Join(dir, fixture.name+".mp4")
			args := append([]string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=" + strconv.Itoa(fixture.width) + "x" + strconv.Itoa(fixture.height) + ":rate=30", "-t", "3"}, fixture.args...)
			runKeyframeTool(b, encoder, append(args, path)...)
			vf := &models.VideoFile{BaseFile: &models.BaseFile{Path: path}, Width: fixture.width, Height: fixture.height}
			filter := keyframeStillFilter(vf, fixture.color, 240)
			var ffmpegTime time.Duration
			var size int
			b.ResetTimer()
			for b.Loop() {
				still, err := f.extractKeyframeStill(context.Background(), path, 2-0.001, true, filter, keyframeStillTimeout)
				if err != nil {
					b.Fatal(err)
				}
				if ms := int64(still.time*1000 + 0.5); ms != 2000 {
					b.Fatalf("decoded %.3fs", still.time)
				}
				ffmpegTime += still.elapsed
				size = len(still.jpeg)
			}
			b.ReportMetric(float64(ffmpegTime.Milliseconds())/float64(b.N), "ffmpeg-ms/op")
			b.ReportMetric(float64(size), "jpeg-bytes")
		})
	}
}

// BenchmarkKeyframeIndex measures the demux-only scan of a 60 s 1080p file.
func BenchmarkKeyframeIndex(b *testing.B) {
	encoder, probe := keyframeTestTools(b)
	path := filepath.Join(b.TempDir(), "index.mp4")
	runKeyframeTool(b, encoder, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=30", "-t", "60", "-c:v", "libx264", "-preset", "ultrafast", "-g", "60", path)
	p := NewFFProbe(probe)
	b.ResetTimer()
	for b.Loop() {
		index, err := p.scanKeyframes(context.Background(), path, keyframeIndexTimeout)
		if err != nil {
			b.Fatal(err)
		}
		if len(index.times) != 30 {
			b.Fatalf("%d keyframes", len(index.times))
		}
	}
}
