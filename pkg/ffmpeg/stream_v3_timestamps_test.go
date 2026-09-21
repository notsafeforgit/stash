package ffmpeg

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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
	timescales := make(map[Track]uint32)
	for _, track := range []Track{TrackVideo, TrackAudio} {
		data, err := os.ReadFile(filepath.Join(firstRun, ".init_"+string(track)+".mp4"))
		if err != nil {
			t.Fatal(err)
		}
		initialization[track] = data
		mdhd := v3MP4Box(t, data, "moov", "trak", "mdia", "mdhd")
		if len(mdhd) < 16 || mdhd[0] > 1 {
			t.Fatal("invalid media header")
		}
		offset := 12
		if mdhd[0] == 1 {
			offset = 20
		}
		if len(mdhd) < offset+4 {
			t.Fatal("truncated media header")
		}
		timescales[track] = binary.BigEndian.Uint32(mdhd[offset:])
		if timescales[track] == 0 {
			t.Fatal("zero media timescale")
		}
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
					// tfdt is unsigned in the browser's MP4 parser. ffprobe's
					// signed packet timestamps can hide an underflow here:
					// AAC priming at -1024 wrapped to 2^64-1024 when timestamp
					// shifting was disabled, breaking iOS playback at startup.
					tfdt := v3MP4Box(t, data, "moof", "traf", "tfdt")
					if len(tfdt) < 8 || tfdt[0] > 1 {
						t.Fatal("invalid fragment decode time")
					}
					decodeTime := uint64(binary.BigEndian.Uint32(tfdt[4:]))
					if tfdt[0] == 1 {
						if len(tfdt) < 12 {
							t.Fatal("truncated fragment decode time")
						}
						decodeTime = binary.BigEndian.Uint64(tfdt[4:])
					}
					want := float64(segment) * segmentDuration
					tolerance := 4 / frameRate
					decodeSeconds := float64(decodeTime) / float64(timescales[track])
					if math.Abs(decodeSeconds-want) > tolerance {
						t.Errorf("%s segment %d raw decode time %d (%.6fs), want %.6f ± %.6f", track, segment, decodeTime, decodeSeconds, want, tolerance)
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
					if math.Abs(firstPTS-want) > tolerance {
						t.Errorf("%s segment %d starts at %.6f, want %.6f ± %.6f", track, segment, firstPTS, want, tolerance)
					}
				}
			}
		})
	}
}

// v3MP4Box reads a nested box without interpreting its timestamps as signed.
func v3MP4Box(t *testing.T, data []byte, path ...string) []byte {
	t.Helper()
	if len(path) == 0 {
		return data
	}
	for len(data) >= 8 {
		size := uint64(binary.BigEndian.Uint32(data[:4]))
		headerSize := uint64(8)
		switch size {
		case 1:
			if len(data) < 16 {
				t.Fatal("truncated extended MP4 box")
			}
			size = binary.BigEndian.Uint64(data[8:16])
			headerSize = 16
		case 0:
			size = uint64(len(data))
		}
		if size < headerSize || size > uint64(len(data)) {
			t.Fatal("invalid MP4 box size")
		}
		if string(data[4:8]) == path[0] {
			return v3MP4Box(t, data[headerSize:size], path[1:]...)
		}
		data = data[size:]
	}
	t.Fatalf("missing MP4 box %s", path[0])
	return nil
}
