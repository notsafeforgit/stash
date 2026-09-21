package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// Exercise the muxer, not just its arguments: output_ts_offset alone does
// not stop the MP4 muxer from rebasing every new run's fragments to zero.
func TestV3HLSTimestampsAcrossTranscodeRestarts(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the HLS timestamp regression")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for the HLS timestamp regression")
	}

	const frameRate = 29.97
	segmentDuration := hlsSegmentDuration(frameRate)
	transcode := func(t *testing.T, start int) string {
		t.Helper()
		dir := t.TempDir()
		args := Args{
			"-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=29.97",
			"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
			"-t", "7", "-map", "0:v", "-map", "1:a",
		}
		args = append(args, V3StreamTypeHLS.Args(VideoCodecLibX264, start, "", false, dir, frameRate)...)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, ffmpegPath, args...).CombinedOutput(); err != nil {
			t.Fatalf("transcode: %v\n%s", err, output)
		}

		return dir
	}
	firstRun := transcode(t, 0)
	initialization := make(map[Track][]byte)
	for _, track := range []Track{TrackVideo, TrackAudio} {
		data, err := os.ReadFile(filepath.Join(firstRun, ".init_"+string(track)+".mp4"))
		if err != nil {
			t.Fatal(err)
		}
		initialization[track] = data
	}
	for _, start := range []int{0, 4495, 14, 4497} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			dir := firstRun
			if start != 0 {
				dir = transcode(t, start)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, track := range []Track{TrackVideo, TrackAudio} {
				for segment := start; segment < start+3; segment++ {
					data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(".%s_%d.m4s", track, segment)))
					if err != nil {
						t.Fatal(err)
					}
					// Clients retain the first run's init segment across seeks.
					media := append(append([]byte(nil), initialization[track]...), data...)
					path := filepath.Join(dir, "probe.mp4")
					if err := os.WriteFile(path, media, 0o600); err != nil {
						t.Fatal(err)
					}
					output, err := exec.CommandContext(ctx, ffprobePath, "-v", "error", "-show_entries", "packet=pts_time", "-of", "json", path).Output()
					if err != nil {
						t.Fatalf("probe %s segment %d: %v", track, segment, err)
					}
					var probe struct {
						Packets []struct {
							PTS string `json:"pts_time"`
						} `json:"packets"`
					}
					if err := json.Unmarshal(output, &probe); err != nil {
						t.Fatal(err)
					}
					if len(probe.Packets) == 0 {
						t.Fatalf("no packets in %s segment %d", track, segment)
					}
					firstPTS, err := strconv.ParseFloat(probe.Packets[0].PTS, 64)
					if err != nil {
						t.Fatal(err)
					}
					// Allow B-frame reordering, keyframe rounding and AAC
					// priming (four video frames), but never a
					// per-run reset or a whole-segment timing discrepancy.
					want := float64(segment) * segmentDuration
					tolerance := 4 / frameRate
					if math.Abs(firstPTS-want) > tolerance {
						t.Errorf("%s segment %d starts at %.6f, want %.6f ± %.6f", track, segment, firstPTS, want, tolerance)
					}
				}
			}
		})
	}
}

// A seek can leave the cache containing adjacent segments from different
// encoder runs. Their video frames must still make one continuous timeline.
func TestV3HLSFrameContinuityAcrossAV1OpusSeeks(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the HLS frame continuity regression")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is required for the HLS frame continuity regression")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run := func(command string, args ...string) []byte {
		t.Helper()
		output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", filepath.Base(command), err, output)
		}
		return output
	}
	encoders := string(run(ffmpegPath, "-hide_banner", "-encoders"))
	if !strings.Contains(encoders, "libaom-av1") || !strings.Contains(encoders, "libopus") {
		t.Skip("libaom-av1 and libopus are required to generate the seek fixture")
	}

	const frameRate = 29.97
	segmentDuration := hlsSegmentDuration(frameRate)
	source := filepath.Join(t.TempDir(), "source.mkv")
	run(ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30000/1001",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "19", "-c:v", "libaom-av1", "-cpu-used", "8", "-threads", "2",
		"-crf", "40", "-g", "60", "-c:a", "libopus", source,
	)

	type packet struct {
		PTS      string `json:"pts_time"`
		DTS      string `json:"dts_time"`
		Duration string `json:"duration_time"`
		Flags    string `json:"flags"`
	}
	type span struct{ firstPTS, lastPTS, firstDTS, lastDTS, duration float64 }
	initialization := make(map[Track][]byte)
	cached := make(map[int]span)
	// Forward, backward, forward again, then fill the final cache gap.
	for _, start := range []int{0, 5, 2, 4} {
		dir := t.TempDir()
		stream := &v3RunningStream{
			streamType: V3StreamTypeHLS,
			vf: &models.VideoFile{
				BaseFile: &models.BaseFile{Path: source},
				Width:    160, Height: 90, FrameRate: frameRate,
				VideoCodec: "av1", AudioCodec: "opus",
			},
			outputDir: dir,
		}
		sm := &StreamManager{
			encoder: &FFMpeg{}, config: rotationTestStreamConfig{}, context: ctx,
		}
		// Rotation is irrelevant to this fixture; avoid an extra probe.
		stream.displayRotationOnce.Do(func() {})
		args := stream.makeStreamArgs(sm, start)
		args = append(args[:len(args)-1], "-t", "6.2", args[len(args)-1])
		run(ffmpegPath, args...)
		for _, track := range []Track{TrackVideo, TrackAudio} {
			if start == 0 {
				initialization[track], err = os.ReadFile(filepath.Join(dir, ".init_"+string(track)+".mp4"))
				if err != nil {
					t.Fatal(err)
				}
			}
			for segment := start; segment < start+3; segment++ {
				data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(".%s_%d.m4s", track, segment)))
				if err != nil {
					t.Fatal(err)
				}
				media := append(append([]byte(nil), initialization[track]...), data...)
				path := filepath.Join(dir, "probe.mp4")
				if err := os.WriteFile(path, media, 0o600); err != nil {
					t.Fatal(err)
				}
				var probe struct {
					Packets []packet `json:"packets"`
				}
				if err := json.Unmarshal(run(ffprobePath, "-v", "error", "-show_entries", "packet=pts_time,dts_time,duration_time,flags", "-of", "json", path), &probe); err != nil {
					t.Fatal(err)
				}
				if len(probe.Packets) == 0 {
					t.Fatalf("run %d: no packets in %s segment %d", start, track, segment)
				}
				if track == TrackAudio {
					// AAC's packet grid need not coincide with video keyframes,
					// but must stay within one audio packet of scene time.
					first, err := strconv.ParseFloat(probe.Packets[0].PTS, 64)
					if err != nil {
						t.Fatal(err)
					}
					if math.Abs(first-float64(segment)*segmentDuration) > 1024.0/48000+0.0001 {
						t.Errorf("run %d: audio segment %d starts at %f", start, segment, first)
					}
					continue
				}
				if len(probe.Packets) != hlsGopSize(frameRate) {
					t.Errorf("run %d: video segment %d has %d frames, want %d", start, segment, len(probe.Packets), hlsGopSize(frameRate))
				}
				if !strings.Contains(probe.Packets[0].Flags, "K") {
					t.Errorf("run %d: video segment %d does not start with a keyframe", start, segment)
				}
				pts := make([]float64, 0, len(probe.Packets))
				dts := make([]float64, 0, len(probe.Packets))
				for _, p := range probe.Packets {
					pt, err := strconv.ParseFloat(p.PTS, 64)
					if err != nil {
						t.Fatal(err)
					}
					dt, err := strconv.ParseFloat(p.DTS, 64)
					if err != nil {
						t.Fatal(err)
					}
					pts = append(pts, pt)
					dts = append(dts, dt)
				}
				sort.Float64s(pts)
				for i, pt := range pts {
					want := float64(segment)*segmentDuration + float64(i)/frameRate
					if math.Abs(pt-want) > 0.0001 {
						t.Errorf("run %d: segment %d frame %d PTS %.6f, want %.6f", start, segment, i, pt, want)
						break
					}
				}
				for i := 1; i < len(dts); i++ {
					if math.Abs(dts[i]-dts[i-1]-1/frameRate) > 0.0001 {
						t.Errorf("run %d: segment %d has nonuniform decode timing at frame %d", start, segment, i)
						break
					}
				}
				if _, exists := cached[segment]; !exists {
					cached[segment] = span{pts[0], pts[len(pts)-1], dts[0], dts[len(dts)-1], 1 / frameRate}
				}
			}
		}
	}
	for segment := 1; segment < 8; segment++ {
		previous, previousOK := cached[segment-1]
		next, nextOK := cached[segment]
		if !previousOK || !nextOK {
			t.Fatalf("missing cached segments at boundary %d", segment)
		}
		if math.Abs(next.firstPTS-previous.lastPTS-previous.duration) > 0.0001 ||
			math.Abs(next.firstDTS-previous.lastDTS-previous.duration) > 0.0001 {
			t.Errorf("cached video timeline is discontinuous at segment %d: %+v -> %+v", segment, previous, next)
		}
	}
}
